<script>
  // Event detail drawer (P6.2). Shows all EVTX metadata fields, both integrity hashes,
  // the flattened parsed fields, and the raw JSON payload, plus the "Add to Graph"
  // action. Slides in from the right over a scrim.
  import { UI, SOURCE_TYPE_LABEL, RELATION_LABEL } from '../../lib/consts/index.js';
  import Button from '../material/Button.svelte';
  import FindingEditor from './FindingEditor.svelte';

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

  const parsedEntries = $derived(event && event.parsed_fields ? Object.entries(event.parsed_fields) : []);

  // Human-readable origin; legacy events (pre source-tracking) have no source_type.
  const sourceTypeLabel = $derived(
    event && event.source_type ? SOURCE_TYPE_LABEL[event.source_type] || event.source_type : UI.DETAIL_SOURCE_UNKNOWN,
  );

  // Timeline / correlation participation (P23), stated explicitly rather than left to be
  // inferred from an empty timestamp field.
  const onTimeline = $derived(!!(event && event.timestamp && new Date(event.timestamp).getTime() > 0));
  const correlated = $derived(!!(relation && relation.count));
  const participation = $derived(
    `${onTimeline ? UI.DETAIL_TIMELINE_YES : UI.DETAIL_TIMELINE_NO} · ` +
      `${correlated ? UI.DETAIL_CORRELATED_YES : UI.DETAIL_CORRELATED_NO}`,
  );

  const meta = $derived(
    event
      ? [
          ['id', event.id],
          [UI.LABEL_EVENT_ID, event.event_id],
          [UI.LABEL_TIMESTAMP, onTimeline ? event.timestamp : UI.UNDATED_TIMESTAMP],
          [UI.LABEL_PARTICIPATION, participation],
          [UI.LABEL_PROVIDER, event.provider],
          [UI.LABEL_CHANNEL, event.channel],
          [UI.LABEL_COMPUTER, event.computer],
          [UI.LABEL_USER, event.user || '—'],
          [UI.DETAIL_OCCURRENCES, event.deduplication_count || 1],
        ]
      : [],
  );

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
          {#if event.source_identifier}
            <dt>{UI.DETAIL_SOURCE_IDENTIFIER}</dt>
            <dd class="mono">{event.source_identifier}</dd>
          {/if}
        </dl>
      </section>

      <section>
        <h3>{UI.DETAIL_HASHES}</h3>
        <dl>
          <dt>{UI.DETAIL_HASH_RAW}</dt>
          <dd class="mono">{event.hash_raw}</dd>
          <dt>{UI.DETAIL_HASH_NORM}</dt>
          <dd class="mono">{event.hash_normalized}</dd>
        </dl>
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
        {#if parsedEntries.length === 0}
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
        <pre class="raw">{prettyRaw(event.raw_xml)}</pre>
      </section>
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
  footer {
    padding: var(--space-4) var(--space-5);
    border-top: 1px solid var(--color-outline);
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
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
