<script>
  // Event detail drawer (P6.2). Shows all EVTX metadata fields, both integrity hashes,
  // the flattened parsed fields, and the raw JSON payload, plus the "Add to Graph"
  // action. Slides in from the right over a scrim.
  import { UI, SOURCE_TYPE_LABEL, RELATION_LABEL } from '../../lib/consts/index.js';
  import * as api from '../../lib/api/index.js';
  import { download } from '../../lib/export.js';
  import { proofOutcome, proofFailure } from '../../lib/maintenance/proof.js';
  import { custodyView, custodyFailure, caseMark } from '../../lib/maintenance/custody.js';
  import { maintenance } from '../../stores/maintenance.js';
  import { ingestion } from '../../stores/ingestion.js';
  import Button from '../material/Button.svelte';
  import Skeleton from '../material/Skeleton.svelte';
  import TextField from '../material/TextField.svelte';
  import FindingEditor from './FindingEditor.svelte';
  import RedactPanel from './RedactPanel.svelte';

  let {
    event = null,
    relation = null,
    relatedEvents = [],
    onclose = undefined,
    onadd = undefined,
    onshowgraph = undefined,
    onselectrelated = undefined,
  } = $props();

  const relationTypes = $derived(
    relation && relation.types ? relation.types.map((t) => RELATION_LABEL[t] || t).join(', ') : '',
  );

  // The raw record and parsed fields are NOT carried by list rows: they live in the payload
  // cold store and are fetched per event, which is what keeps a large case's memory bounded.
  // This drawer is the only view that wants them, so this is the only place that pays.
  let payload = $state(/** @type {any} */ (null));
  let payloadFor = $state(/** @type {number|null} */ (null));
  $effect(() => {
    const id = event ? event.id : null;
    if (id == null) {
      payload = null;
      payloadFor = null;
      return;
    }
    if (payloadFor === id) return; // already fetched for this event
    payloadFor = id;
    payload = null;
    api
      .getEvent(id)
      .then((full) => {
        // Guard against a slow response arriving after the analyst moved on.
        if (payloadFor === id) payload = full;
      })
      .catch(() => {
        if (payloadFor === id) payload = null;
      });
  });

  // A redaction elsewhere, or from this drawer, changes what the event is: fetch it again.
  let redactionsSeen = $maintenance.redactions;
  $effect(() => {
    const n = $maintenance.redactions;
    if (n === redactionsSeen) return;
    redactionsSeen = n;
    payloadFor = null;
  });

  // 🔒 Once the case says the event was redacted, what is shown is what the case holds — not the
  // list row the drawer was opened from, which still carries the content in memory.
  const shown = $derived(payload && payload.redacted ? payload : event);

  const rawXml = $derived(payload ? payload.raw_xml : '');
  // A redaction removed the cold fields: their absence says so rather than reading as "none".
  const payloadRedacted = $derived(!!(payload && payload.payload_redacted));
  const parsedEntries = $derived(payload && payload.parsed_fields ? Object.entries(payload.parsed_fields) : []);
  const payloadLoading = $derived(event != null && payload == null);

  // Human-readable origin; legacy events (pre source-tracking) have no source_type.
  const sourceTypeLabel = $derived(
    shown && shown.source_type ? SOURCE_TYPE_LABEL[shown.source_type] || shown.source_type : UI.DETAIL_SOURCE_UNKNOWN,
  );

  // Timeline / correlation participation (P23), stated explicitly rather than left to be
  // inferred from an empty timestamp field.
  const onTimeline = $derived(!!(shown && shown.timestamp && new Date(shown.timestamp).getTime() > 0));
  const correlated = $derived(!!(relation && relation.count));
  const participation = $derived(
    `${onTimeline ? UI.DETAIL_TIMELINE_YES : UI.DETAIL_TIMELINE_NO} · ` +
      `${correlated ? UI.DETAIL_CORRELATED_YES : UI.DETAIL_CORRELATED_NO}`,
  );

  const meta = $derived(
    shown
      ? [
          ['id', shown.id],
          [UI.LABEL_EVENT_ID, shown.event_id],
          [UI.LABEL_TIMESTAMP, onTimeline ? shown.timestamp : UI.UNDATED_TIMESTAMP],
          [UI.LABEL_PARTICIPATION, participation],
          [UI.LABEL_PROVIDER, shown.provider],
          [UI.LABEL_CHANNEL, shown.channel],
          [UI.LABEL_COMPUTER, shown.computer],
          [UI.LABEL_USER, shown.user || '—'],
          [UI.DETAIL_OCCURRENCES, shown.deduplication_count || 1],
        ]
      : [],
  );

  // Event proof (plan §14.3). The outcome belongs to the event it was asked about: moving to another
  // event clears it, and a response arriving after the analyst moved on is dropped — a "saved"
  // shown beside the wrong event would be a false statement about evidence.
  let proof = $state(/** @type {import('../../lib/maintenance/proof.js').ProofOutcome|null} */ (null));
  let proofBusy = $state('');
  let proofFor = $state(/** @type {number|null} */ (null));
  $effect(() => {
    const id = event ? event.id : null;
    if (proofFor === id) return;
    proofFor = id;
    proof = null;
    proofBusy = '';
  });

  async function saveProof() {
    const id = event.id;
    proofBusy = UI.DETAIL_PROOF_WORKING;
    try {
      const r = await api.exportEventProof(id);
      if (proofFor !== id) return;
      const outcome = proofOutcome(r);
      // The document is written exactly as Go returned it: those are the bytes it verified.
      if (outcome.saved) download(r.file_name, 'application/json', r.document);
      proof = outcome;
    } catch (e) {
      if (proofFor === id) proof = proofFailure(e);
    } finally {
      if (proofFor === id) proofBusy = '';
    }
  }

  // Sealing goes through the maintenance store, so the Maintenance page's posture is re-read and
  // a second pass cannot start beside one already running.
  async function sealAndSaveProof() {
    const id = event.id;
    // Busy is checked here, not inferred afterwards: compact() refuses without clearing the store's
    // error, so reading that error after a refusal would show whatever an earlier task left there.
    if (maintenance.current().running !== '') {
      proof = proofFailure(UI.DETAIL_PROOF_BUSY);
      return;
    }
    proofBusy = UI.DETAIL_PROOF_SEALING;
    const sealed = await maintenance.compact();
    if (proofFor !== id) return;
    if (!sealed) {
      // compact() cleared the error when it started, so this one is its own.
      proof = proofFailure(maintenance.current().error || UI.DETAIL_PROOF_BUSY);
      proofBusy = '';
      return;
    }
    await saveProof();
  }

  // Custody report (plan §14.4). It belongs to the event AND the image it was made against: moving
  // to another event drops it, and so does any seal, because the seal replaced that image. A
  // response that arrives after either is dropped too — the generation says which request is
  // still the current one.
  let custody = $state(/** @type {import('../../lib/maintenance/custody.js').CustodyView|null} */ (null));
  let custodyBusy = $state(false);
  let custodyFor = $state(/** @type {number|null} */ (null));
  let custodyGen = 0;
  // 🔒 Never prefilled from the case. Whatever is here, the analyst typed or pasted: a check
  // against the case's own copy of its fingerprint proves nothing.
  let keptInput = $state('');
  const dropCustody = () => {
    custodyGen++;
    custody = null;
    custodyBusy = false;
  };
  $effect(() => {
    const id = event ? event.id : null;
    if (custodyFor === id) return;
    custodyFor = id;
    dropCustody();
  });
  // Dropped only when the case changes under the report (caseMark), not on every store update.
  let custodyMark = '';
  $effect(() => {
    const mark = caseMark($ingestion, $maintenance);
    if (mark === custodyMark) return;
    custodyMark = mark;
    dropCustody();
  });

  async function runCustody(kept) {
    const id = event.id;
    const gen = ++custodyGen;
    custodyBusy = true;
    custody = null;
    try {
      const r = await api.eventCustody(id, kept);
      if (gen === custodyGen) custody = custodyView(r);
    } catch (e) {
      if (gen === custodyGen) custody = custodyFailure(e);
    } finally {
      if (gen === custodyGen) custodyBusy = false;
    }
  }

  function prettyRaw(raw) {
    if (!raw) return '';
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch (_) {
      return raw;
    }
  }
</script>

{#if event}
  <div class="scrim" onclick={() => onclose?.()} role="presentation"></div>
  <aside class="drawer" aria-label={UI.DETAIL_TITLE}>
    <header>
      <h2>{UI.DETAIL_TITLE}</h2>
      <button class="x" onclick={() => onclose?.()} aria-label={UI.ACTION_CLOSE}>×</button>
    </header>

    <div class="body">
      <!-- The analyst's own findings sit at the top: on revisiting a marked event, their
           reasoning is what they came back for, not the metadata they already read. -->
      <FindingEditor {event} />

      <section>
        <h3>{UI.DETAIL_METADATA}</h3>
        <dl>
          {#each meta as [k, v] (k)}
            <dt>{k}</dt>
            <dd>{v}</dd>
          {/each}
        </dl>
      </section>

      <section>
        <h3>{UI.DETAIL_SOURCE}</h3>
        <dl>
          <dt>{UI.DETAIL_SOURCE_TYPE}</dt>
          <dd>{sourceTypeLabel}</dd>
          {#if shown.source_identifier}
            <dt>{UI.DETAIL_SOURCE_IDENTIFIER}</dt>
            <dd class="mono">{shown.source_identifier}</dd>
          {/if}
        </dl>
      </section>

      <section>
        <h3>{UI.DETAIL_HASHES}</h3>
        <dl>
          <dt>{UI.DETAIL_HASH_RAW}</dt>
          <dd class="mono">{shown.hash_raw}</dd>
          <dt>{UI.DETAIL_HASH_NORM}</dt>
          <dd class="mono">{shown.hash_normalized}</dd>
        </dl>
      </section>

      <section class="proof">
        <h3>{UI.DETAIL_PROOF_TITLE}</h3>
        <p class="muted">{UI.DETAIL_PROOF_HINT}</p>
        <div class="proofactions">
          <Button variant="outlined" disabled={proofBusy !== ''} onclick={saveProof}>{UI.DETAIL_PROOF_SAVE}</Button>
          {#if proof && proof.sealable}
            <Button variant="tonal" disabled={proofBusy !== ''} onclick={sealAndSaveProof}>{UI.DETAIL_PROOF_SEAL}</Button>
          {/if}
        </div>
        {#if proofBusy}
          <p class="muted" role="status">{proofBusy}</p>
        {:else if proof}
          <p class={proof.damage ? 'proofmsg damage' : 'proofmsg'} role="status">{proof.message}</p>
          {#if proof.saved}
            <p class="mono fingerprint">{proof.snapshot}</p>
          {/if}
          {#each proof.warnings as w (w)}
            <p class="proofmsg warn">{w}</p>
          {/each}
        {/if}
      </section>

      {#snippet lines(/** @type {import('../../lib/maintenance/custody.js').CustodyLine[]} */ ls)}
        <ul class="layers">
          {#each ls as l, i (l.key + i)}
            <li class={'layer ' + l.tone}>
              <span class="lname">{l.label}</span>
              <span class="lstate">{l.stateLabel}</span>
              <p class="lmsg">{l.message}</p>
              {#each l.notes as n (n)}
                <p class="lmsg warn">{n}</p>
              {/each}
              {#if l.remedy}
                <p class="lmsg">{l.remedy}</p>
              {/if}
              {#if l.cause}
                <p class="lmsg mono">{UI.CUSTODY_CAUSE} {l.cause}</p>
              {/if}
            </li>
          {/each}
        </ul>
      {/snippet}

      <section class="custody">
        <h3>{UI.CUSTODY_TITLE}</h3>
        <p class="muted">{UI.CUSTODY_HINT}</p>
        <div class="proofactions">
          <Button variant="outlined" disabled={custodyBusy} onclick={() => runCustody('')}>{UI.CUSTODY_RUN}</Button>
        </div>
        <div class="kept">
          <TextField label={UI.CUSTODY_KEPT_LABEL} placeholder={UI.CUSTODY_KEPT_PLACEHOLDER} bind:value={keptInput} />
          <Button variant="tonal" disabled={custodyBusy || keptInput.trim() === ''} onclick={() => runCustody(keptInput)}
            >{UI.CUSTODY_CHECK}</Button
          >
        </div>
        {#if custodyBusy}
          <p class="muted" role="status">{UI.CUSTODY_WORKING}</p>
        {:else if custody && !custody.ok}
          <p class="proofmsg damage" role="status">{custody.error}</p>
        {:else if custody}
          <p class="proofmsg" role="status">{custody.presence}</p>
          <p class="muted">{custody.form} {custody.counts}</p>
          {@render lines(custody.layers)}
          <details class="records">
            <summary>{UI.CUSTODY_RECORDS_TITLE}</summary>
            {@render lines(custody.records)}
          </details>
          <p class="rlabel">{UI.CUSTODY_ESTABLISHES}</p>
          {#if custody.establishes.length}
            <ul class="claims">
              {#each custody.establishes as c (c)}<li>{c}</li>{/each}
            </ul>
          {:else}
            <p class="lmsg">{UI.CUSTODY_ESTABLISHES_NONE}</p>
          {/if}
          <p class="rlabel">{UI.CUSTODY_NOT_ESTABLISHES}</p>
          <ul class="claims">
            {#each custody.doesNot as c (c)}<li>{c}</li>{/each}
          </ul>
        {/if}
      </section>

      <section>
        <h3>{UI.DETAIL_RELATIONS}</h3>
        {#if !relation || !relation.count}
          <p class="muted">{UI.DETAIL_RELATION_NONE}</p>
        {:else}
          <dl>
            <dt>{relation.count === 1 ? UI.RELATION_ONE : UI.RELATION_MANY}</dt>
            <dd>{relation.count}{relationTypes ? ` · ${relationTypes}` : ''}</dd>
          </dl>
          {#if relatedEvents.length}
            <p class="rlabel">{UI.LABEL_RELATED_EVENTS}</p>
            <div class="related">
              {#each relatedEvents as re (re.id)}
                <button class="rchip" type="button" onclick={() => onselectrelated?.(re.id)}>
                  {re.event_id} · {re.provider}
                </button>
              {/each}
            </div>
          {/if}
        {/if}
      </section>

      <section>
        <h3>{UI.DETAIL_PARSED}</h3>
        {#if payloadRedacted}
          <p class="muted">{UI.DETAIL_RAW_REDACTED}</p>
        {:else if parsedEntries.length === 0}
          <p class="muted">{UI.DETAIL_NONE}</p>
        {:else}
          <dl>
            {#each parsedEntries as [k, v] (k)}
              <dt>{k}</dt>
              <dd>{v}</dd>
            {/each}
          </dl>
        {/if}
      </section>

      <section>
        <h3>{UI.DETAIL_RAW}</h3>
        {#if payloadLoading}
          <!-- The raw record is fetched from the payload cold store as the drawer opens. The
               drawer's own slide-in usually covers it; when the fetch runs longer, a shaped
               skeleton stands in for the record rather than a bare "loading" line. -->
          <div class="rawskel" aria-label={UI.DETAIL_LOADING}>
            <Skeleton width="90%" />
            <Skeleton width="80%" />
            <Skeleton width="95%" />
            <Skeleton width="60%" />
          </div>
        {:else if payloadRedacted}
          <p class="muted">{UI.DETAIL_RAW_REDACTED}</p>
        {:else}
          <pre class="raw">{prettyRaw(rawXml)}</pre>
        {/if}
      </section>

      <RedactPanel {event} detail={payload} />
    </div>

    <footer>
      <Button variant="text" onclick={() => onshowgraph?.(event)}>{UI.ACTION_SHOW_IN_GRAPH}</Button>
      <Button onclick={() => onadd?.(event)}>{UI.ACTION_ADD_TO_GRAPH}</Button>
    </footer>
  </aside>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--color-scrim);
    z-index: 90;
  }
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    height: 100vh;
    width: min(560px, 92vw);
    background: var(--color-surface);
    border-left: 1px solid var(--color-outline);
    box-shadow: var(--elevation-3);
    z-index: 91;
    display: flex;
    flex-direction: column;
    font-family: var(--font-sans);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-5);
    border-bottom: 1px solid var(--color-outline);
  }
  h2 {
    margin: 0;
    font-size: 1.1rem;
    font-weight: 800;
    color: var(--color-on-surface);
  }
  .x {
    background: transparent;
    border: none;
    color: var(--color-on-surface-muted);
    font-size: 1.5rem;
    line-height: 1;
    cursor: pointer;
  }
  .body {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
  h3 {
    margin: 0 0 var(--space-3);
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-on-surface-muted);
  }
  dl {
    display: grid;
    grid-template-columns: minmax(120px, 40%) 1fr;
    gap: var(--space-2) var(--space-4);
    margin: 0;
  }
  dt {
    color: var(--color-on-surface-muted);
    font-size: 0.85rem;
  }
  dd {
    margin: 0;
    color: var(--color-on-surface);
    font-size: 0.85rem;
    word-break: break-word;
  }
  .mono {
    font-family: var(--font-mono);
    font-size: 0.78rem;
  }
  .muted {
    color: var(--color-on-surface-muted);
    font-size: 0.85rem;
    margin: 0;
  }
  .raw {
    font-family: var(--font-mono);
    font-size: 0.75rem;
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    overflow-x: auto;
    max-height: 320px;
    color: var(--color-on-surface);
    white-space: pre;
  }
  /* Placeholder for the raw record while it loads from the payload store: same frame as the
     real block, lines of varying width so it reads as text arriving. */
  .rawskel {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-3);
  }
  footer {
    padding: var(--space-4) var(--space-5);
    border-top: 1px solid var(--color-outline);
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
  }
  .proofactions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .proofmsg {
    margin: var(--space-3) 0 0;
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
  .proofmsg.warn {
    color: var(--color-warning);
  }
  .proofmsg.damage {
    color: var(--color-error);
  }
  .kept {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .layers {
    list-style: none;
    margin: var(--space-3) 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .layer {
    border-left: 3px solid var(--color-outline);
    padding-left: var(--space-3);
  }
  .layer.established {
    border-left-color: var(--color-primary);
  }
  .layer.not_established {
    border-left-color: var(--color-warning);
  }
  .layer.broken {
    border-left-color: var(--color-error);
  }
  .lname {
    font-weight: 700;
    font-size: 0.85rem;
  }
  .lstate {
    margin-left: var(--space-2);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
  }
  .layer.broken .lstate {
    color: var(--color-error);
  }
  .lmsg {
    margin: var(--space-1) 0 0;
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
  .lmsg.warn {
    color: var(--color-warning);
  }
  .records {
    margin-top: var(--space-3);
    font-size: 0.85rem;
  }
  .claims {
    margin: 0;
    padding-left: var(--space-5);
    font-size: 0.85rem;
  }
  .fingerprint {
    margin: var(--space-2) 0 0;
    word-break: break-all;
    user-select: all;
  }
  .rlabel {
    margin: var(--space-3) 0 var(--space-2);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
  }
  .related {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .rchip {
    font-family: var(--font-sans);
    font-size: 0.78rem;
    color: var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 12%, transparent);
    border: 1px solid var(--color-primary);
    border-radius: 999px;
    padding: 2px var(--space-3);
    cursor: pointer;
  }
  .rchip:hover {
    background: color-mix(in srgb, var(--color-primary) 22%, transparent);
  }
</style>
