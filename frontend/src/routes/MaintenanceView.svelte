<script>
  // Case maintenance: check the case, read what was found, run the fix it names.
  //
  // 🔒 Two rules shape the whole screen.
  //
  // First, nothing runs on its own. Every action is a button, because everything here is
  // proportional to the size of the case — and work that taxes a launch to re-prove something
  // almost always true is not a safety feature (PERFORMANCE.md §12a).
  //
  // Second, an all-clear is a claim, and a claim gets qualified. "Nothing wrong found" after a
  // quick check does not mean what it means after a deep one, and it means nothing at all if a
  // detector could not run — so the verdict always says which of those it is.
  import { onMount, onDestroy } from 'svelte';
  import { theme } from '../stores/theme.js';
  import { maintenance } from '../stores/maintenance.js';
  import { route } from '../stores/router.js';
  import * as api from '../lib/api/index.js';
  import { download } from '../lib/export.js';
  import { ROUTES, THEMES, UI, CHANNELS } from '../lib/consts/index.js';
  import {
    grouped,
    verdict,
    countRows,
    actionsIn,
    runnable,
    duration,
    ranAt,
    backfillFraction,
    sessionSigning,
    writesRefused,
    ACTION,
  } from '../lib/maintenance/report.js';
  import { pendingLine } from '../lib/maintenance/redaction.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import ProgressBar from '../components/material/ProgressBar.svelte';
  import SigningCard from '../components/maintenance/SigningCard.svelte';
  import AnchoringCard from '../components/maintenance/AnchoringCard.svelte';

  const report = $derived($maintenance.report);
  const read = $derived(verdict(report));
  const sections = $derived(grouped(report));
  const counts = $derived(countRows(report));
  const offered = $derived(actionsIn(report));
  const busy = $derived($maintenance.running !== '');
  const progress = $derived(backfillFraction($maintenance.progress));
  const stale = $derived($maintenance.status?.stale ?? 0);
  const forensics = $derived($maintenance.forensics);
  const image = $derived($maintenance.image);
  // A session that cannot write is not offered a button it would refuse (item 43).
  const noWrites = $derived(writesRefused(forensics));
  const signingNow = $derived(sessionSigning(forensics));
  const SIGNING_LINE = {
    read_only: UI.MAINT_FORENSIC_READ_ONLY,
    signed: UI.MAINT_FORENSIC_SIGNED,
    unsigned: UI.MAINT_FORENSIC_UNSIGNED,
  };

  let copied = $state(false);
  let exported = $state(false);

  let unsubscribe = () => {};

  onMount(() => {
    // Two cheap reads, and both are things worth knowing before any button is pressed. The
    // projection status decides whether half the rule library can say anything at all; the
    // forensic posture decides what every other number on this page can be taken to mean.
    maintenance.loadStatus();
    maintenance.loadForensics();
    // Cheap: the journal is in memory. An unfinished redaction may still hold content.
    maintenance.loadPending();
    unsubscribe = api.on(CHANNELS.MAINTENANCE_PROGRESS, (p) => maintenance.onProgress(p));
  });
  onDestroy(() => unsubscribe());

  function runAction(action) {
    if (action === ACTION.BACKFILL) return maintenance.backfill();
    if (action === ACTION.REPAIR) return maintenance.repair();
    if (action === ACTION.REBUILD) return maintenance.rebuild();
    if (action === ACTION.COMPACT) return maintenance.compact();
    if (action === ACTION.VERIFY_IMAGE) return maintenance.verify();
    if (action === ACTION.FINISH_REDACTION) return maintenance.finishRedactions();
    if (action === ACTION.PUBLISH_CHECKPOINT) return maintenance.publishCheckpoint();
    return Promise.resolve(false);
  }

  /** copyRoot puts the fingerprint on the clipboard, because the whole instruction beside it is
   *  "keep this somewhere else" and retyping sixty-four hex characters by hand is how a digit
   *  gets transposed — which would later read as a case that had been altered. */
  async function copyRoot() {
    const value = forensics?.roots?.snapshot ?? '';
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch {
      // Clipboard access can be refused. The value is on screen and selectable, so the copy
      // button is a convenience — failing it silently is better than an error about a fingerprint
      // the analyst can already read.
    }
  }

  /** saveFingerprint writes the artefact out as a file.
   *
   *  A FILE rather than only the clipboard, because the whole instruction is "keep this somewhere
   *  else" and the document carries what the bare hash cannot: which case and which image it
   *  describes, and — inside the file — what it does and does not establish. A hash pasted into a
   *  note six weeks ago is a hash nobody can place. */
  async function saveFingerprint() {
    const doc = await maintenance.exportFingerprint();
    if (!doc) return;
    const stamp = String(doc.sealed_at ?? '').replace(/[:.]/g, '-');
    download(`rohy-fingerprint-${stamp}.json`, 'application/json', JSON.stringify(doc, null, 2));
    exported = true;
    setTimeout(() => (exported = false), 3000);
  }
</script>

<div class="mv">
<AppBar route={ROUTES.MAINTENANCE}>
  <Button variant="tonal" onclick={() => maintenance.check(false)} disabled={busy}>
    {UI.MAINT_CHECK}
  </Button>
  <Button variant="text" onclick={() => maintenance.check(true)} disabled={busy}>
    {UI.MAINT_CHECK_DEEP}
  </Button>
  <!-- Only a backfill can be stopped: CancelMaintenance reaches nothing else, so Stop beside a
       check or a fix would do nothing, or cancel a backfill started from the Rules page. Even a
       backfill stops only in its pass over the events: the seal after that pass runs to the end,
       and Stop stays shown through it because the run kind does not change. The kind is the
       store's run kind (run('backfill', …) in stores/maintenance.js). -->
  {#if $maintenance.running === 'backfill'}
    <Button variant="text" onclick={() => maintenance.cancel()}>{UI.MAINT_CANCEL}</Button>
  {/if}
  <Button variant="tonal" onclick={() => theme.toggle()}>
    {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
  </Button>
</AppBar>

<div class="body">
  <p class="intro">{UI.MAINT_INTRO}</p>
  <p class="hint">{UI.MAINT_CHECK_DEEP_HINT}</p>

  {#if $maintenance.error}
    <div class="banner err">
      <span>{UI.MAINT_FAILED} {$maintenance.error}</span>
      <Button variant="text" onclick={() => maintenance.clearError()}>{UI.MAINT_DISMISS}</Button>
    </div>
  {/if}

  {#if busy}
    <div class="banner">
      <span>{UI.MAINT_RUNNING[$maintenance.running] ?? UI.MAINT_CHECKING}</span>
      <!-- Indeterminate when the total is unknown: a bar pinned at zero reads as work that
           started and stalled. -->
      <ProgressBar value={progress} />
    </div>
  {:else if $maintenance.lastResult && UI.MAINT_DONE[$maintenance.lastResult]}
    <div class="banner ok"><span>{UI.MAINT_DONE[$maintenance.lastResult]}</span></div>
  {/if}

  <!-- The correlation projection, always shown. It is the difference between "no matches" and
       "could not have matched", and it is worth knowing before a check is even run. -->
  <section class="card">
    <h3>{UI.MAINT_PROJECTION_TITLE}</h3>
    {#if $maintenance.status === null}
      <p class="hint">—</p>
    {:else if stale === 0}
      <p class="hint">{UI.MAINT_PROJECTION_OK}</p>
    {:else}
      <p class="warn"><b>{stale}</b> {UI.MAINT_PROJECTION_STALE}</p>
      <Button variant="tonal" onclick={() => maintenance.backfill()} disabled={busy || noWrites}>
        {UI.MAINT_ACTION.backfill}
      </Button>
    {/if}
  </section>

  <!-- Who is working, how the case opened, and its keys (item 41). Above what the case can prove:
       a read-only session is the first thing to know before reading anything below it. -->
  <SigningCard />

  <!--
    What the case can prove (§14). Placed ABOVE the integrity report deliberately: it qualifies
    everything below it. A report that says "no problems found" about a case that cannot establish
    its own contents have not changed is saying much less than it appears to, and a reader who
    meets the qualification after the verdict has already drawn a conclusion.
  -->
  <section class="card">
    <h3>{UI.MAINT_FORENSIC_TITLE}</h3>
    {#if forensics === null}
      <p class="hint">{UI.MAINT_FORENSIC_UNREAD}</p>
    {:else if !forensics.available}
      <!-- 🔒 Reported, never rendered as an all-clear: there is nothing wrong, and there is also
           nothing being checked. -->
      <p class="hint">{UI.MAINT_FORENSIC_UNAVAILABLE}</p>
    {:else}
      {#if forensics.roots_error}
        <p class="warn">{UI.MAINT_FORENSIC_ROOTS_UNREADABLE}</p>
        <p class="hint"><code>{forensics.roots_error}</code></p>
      {:else if forensics.compacted}
        <p class="hint">{UI.MAINT_FORENSIC_SEALED}</p>
      {:else}
        <p class="warn">{UI.MAINT_FORENSIC_UNSEALED}</p>
        <div class="fixes">
          <Button variant="tonal" onclick={() => maintenance.compact()} disabled={busy || noWrites}>
            {UI.MAINT_ACTION.compact}
          </Button>
        </div>
      {/if}

      <p class:hint={signingNow === 'signed'} class:warn={signingNow !== 'signed'}>
        {SIGNING_LINE[signingNow]}
      </p>

      {#if forensics.roots?.snapshot}
        <div class="root">
          <span class="rootlabel">{UI.MAINT_FORENSIC_ROOT_LABEL}</span>
          <!-- Selectable text, not just a copy button: the button can be refused by the
               environment, and the value must still be reachable. -->
          <code class="roothash">{forensics.roots.snapshot}</code>
          <Button variant="text" onclick={copyRoot}>
            {copied ? UI.MAINT_FORENSIC_COPIED : UI.MAINT_FORENSIC_COPY}
          </Button>
          <Button variant="tonal" onclick={saveFingerprint} disabled={busy}>
            {UI.MAINT_FORENSIC_EXPORT}
          </Button>
        </div>
        <p class="hint">{UI.MAINT_FORENSIC_ROOT_HINT}</p>
        {#if exported}
          <!-- 🔒 A note, not a tick. rohy saw a file get written; it cannot see where that file
               went or whether it survives, and the copy is worth having precisely because those
               are outside what this process can observe. -->
          <p class="hint">{UI.MAINT_FORENSIC_EXPORTED}</p>
        {/if}
      {/if}
    {/if}
  </section>

  <!-- Checkpoints outside the case (item 42): the one record whose digest leaves the case. -->
  {#if forensics?.available}
    <AnchoringCard {busy} {noWrites} />
  {/if}

  <!-- Unfinished redactions (plan §14.5). Shown whenever there are any, before a check is run:
       until they finish, content they were removing may still be in the case. -->
  {#if $maintenance.pending && $maintenance.pending.length > 0}
    <section class="card">
      <h3>{UI.REDACT_PENDING_TITLE}</h3>
      <ul class="findings">
        {#each $maintenance.pending as e (e.id)}
          <li class="warning"><p class="msg">{pendingLine(e)}</p></li>
        {/each}
      </ul>
      <div class="fixes">
        <Button variant="tonal" onclick={() => maintenance.finishRedactions()} disabled={busy || noWrites}>
          {UI.MAINT_ACTION.finish_redaction}
        </Button>
      </div>
    </section>
  {/if}

  <!-- The image check. Its own card because it is an action with a result, not a state. -->
  {#if forensics?.available && forensics.compacted}
    <section class="card">
      <h3>{UI.MAINT_IMAGE_TITLE}</h3>
      <p class="hint">{UI.MAINT_IMAGE_HINT}</p>
      <div class="fixes">
        <Button variant="tonal" onclick={() => maintenance.verify()} disabled={busy}>
          {UI.MAINT_ACTION.verify_image}
        </Button>
      </div>
      {#if image}
        <!-- Four outcomes. `absent` and `no_image` both mean nothing was verified, and neither is
             styled as a pass — an unverifiable case must not read as a verified one. -->
        <p class:warn={image.digest !== 'match'} class:bad={image.digest === 'mismatch'}>
          {UI.MAINT_IMAGE[image.digest] ?? image.digest}
        </p>
        {#if image.roots_error}
          <p class="bad">{UI.MAINT_IMAGE_ROOTS_FAILED} {image.roots_error}</p>
        {/if}
        {#if image.computed}
          <div class="root">
            <span class="rootlabel">{UI.MAINT_IMAGE_COMPUTED}</span>
            <code class="roothash">{image.computed}</code>
          </div>
        {/if}
      {/if}
    </section>
  {/if}

  {#if !report}
    <p class="hint">{UI.MAINT_NOT_RUN}</p>
  {:else}
    <section class="card">
      <div class="verdict" class:bad={read.state === 'error'} class:warn={read.state === 'warning'} class:unknown={read.state === 'incomplete'}>
        {#if read.state === 'incomplete'}
          <!-- 🔒 A report whose detector failed has found nothing because it did not look. -->
          <p class="big">{UI.MAINT_INCOMPLETE}</p>
          <ul class="errs">
            {#each report.errors as e (e)}<li>{e}</li>{/each}
          </ul>
        {:else if read.errors > 0}
          <p class="big"><b>{read.errors}</b> {UI.MAINT_HAS_ERRORS}</p>
        {:else if read.warnings > 0}
          <p class="big"><b>{read.warnings}</b> {UI.MAINT_HAS_WARNINGS}</p>
        {:else}
          <p class="big">{UI.MAINT_OK}</p>
          <!-- Qualified, always: what "clean" covers depends on which check was run. -->
          <p class="hint">{read.deep ? UI.MAINT_OK_DEEP : UI.MAINT_OK_QUICK}</p>
        {/if}
      </div>

      <p class="meta">
        {UI.MAINT_RAN} {ranAt(report.ran_at)} · {UI.MAINT_TOOK} {duration(report.duration_ms)}
      </p>

      <h4>{UI.MAINT_LOOKED_AT}</h4>
      <ul class="counts">
        {#each counts as c (c.key)}
          <li class:quiet={c.always && c.value === 0}>
            <b>{c.value}</b> {UI.MAINT_COUNT[c.key]}
          </li>
        {/each}
      </ul>

      {#if offered.length > 0}
        <div class="fixes">
          {#each offered as a (a)}
            <Button variant="tonal" onclick={() => runAction(a)} disabled={busy}>
              {UI.MAINT_ACTION[a]}
            </Button>
          {/each}
        </div>
      {/if}
    </section>

    {#each sections as section (section.severity)}
      <section class="card">
        <h3>{UI.MAINT_SEV[section.severity]}</h3>
        <ul class="findings">
          {#each section.items as f, i (f.code + ':' + (f.subject ?? '') + ':' + i)}
            <li class={section.severity}>
              <p class="msg">{f.message}</p>
              {#if runnable(f.action)}
                <Button variant="text" onclick={() => runAction(f.action)} disabled={busy}>
                  {UI.MAINT_ACTION[f.action]}
                </Button>
              {:else if UI.MAINT_ADVICE[f.action]}
                <!-- Advice, not a button: "ingest the missing log" needs files rohy does not
                     have, and a control that did nothing would be worse than a sentence. -->
                <p class="advice">{UI.MAINT_ADVICE[f.action]}</p>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}

  <div class="foot">
    <Button variant="text" onclick={() => route.go(ROUTES.RULES)}>{UI.NAV_RULES}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.EVENTS)}>{UI.NAV_EVENTS}</Button>
  </div>
</div>
</div>

<style>
  .mv {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: var(--space-5);
    overflow-y: auto;
  }
  .intro {
    margin: 0;
    max-width: 68ch;
    font-size: 0.9rem;
    line-height: 1.5;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 80ch;
    padding: 14px 16px;
    border-radius: 10px;
    background: var(--md-sys-color-surface-container);
  }
  h3 {
    margin: 0;
    font-size: 0.78rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--md-sys-color-on-surface-variant);
  }
  h4 {
    margin: 4px 0 0;
    font-size: 0.8rem;
  }
  .big {
    margin: 0;
    font-size: 1.05rem;
    font-weight: 700;
  }
  .verdict.bad .big {
    color: var(--md-sys-color-error);
  }
  .verdict.warn .big,
  .verdict.unknown .big {
    color: var(--md-sys-color-tertiary);
  }
  .hint,
  .meta,
  .advice {
    margin: 0;
    font-size: 0.8rem;
    line-height: 1.45;
    color: var(--md-sys-color-on-surface-variant);
  }
  .warn {
    margin: 0;
    font-size: 0.85rem;
    line-height: 1.45;
  }
  .bad {
    margin: 0;
    font-size: 0.85rem;
    line-height: 1.45;
    color: var(--md-sys-color-error);
  }
  /* The fingerprint. Monospace and wrapping, because it is sixty-four hex characters an analyst
     compares by eye against one they wrote down — a truncated or ellipsised hash is worse than
     none, since it invites a comparison that only checks the ends. */
  .root {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .rootlabel {
    font-size: 0.78rem;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--md-sys-color-on-surface-variant);
  }
  .roothash {
    flex: 1 1 22ch;
    min-width: 0;
    padding: 4px 8px;
    border-radius: 6px;
    background: var(--md-sys-color-surface-container-high);
    font-family: var(--font-mono, monospace);
    font-size: 0.78rem;
    line-height: 1.5;
    overflow-wrap: anywhere;
    user-select: all;
  }
  .counts,
  .findings,
  .errs {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .counts li {
    font-size: 0.84rem;
  }
  /* A zero that is shown deliberately reads quieter than one that means something. */
  .counts li.quiet {
    color: var(--md-sys-color-on-surface-variant);
  }
  .errs {
    padding-left: 16px;
    font-size: 0.82rem;
    list-style: disc;
  }
  .findings li {
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--md-sys-color-surface-container-high);
    border-left: 3px solid var(--md-sys-color-outline-variant);
  }
  .findings li.error {
    border-left-color: var(--md-sys-color-error);
  }
  .findings li.warning {
    border-left-color: var(--md-sys-color-tertiary);
  }
  .msg {
    margin: 0;
    font-size: 0.86rem;
    line-height: 1.45;
  }
  .fixes,
  .foot {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .banner {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 80ch;
    padding: 10px 14px;
    border-radius: 8px;
    background: var(--md-sys-color-surface-container-high);
    font-size: 0.86rem;
  }
  .banner.err {
    border-left: 3px solid var(--md-sys-color-error);
  }
  .banner.ok {
    border-left: 3px solid var(--md-sys-color-primary);
  }
</style>
