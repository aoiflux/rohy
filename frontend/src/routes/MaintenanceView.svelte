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
    ACTION,
  } from '../lib/maintenance/report.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import ProgressBar from '../components/material/ProgressBar.svelte';

  const report = $derived($maintenance.report);
  const read = $derived(verdict(report));
  const sections = $derived(grouped(report));
  const counts = $derived(countRows(report));
  const offered = $derived(actionsIn(report));
  const busy = $derived($maintenance.running !== '');
  const progress = $derived(backfillFraction($maintenance.progress));
  const stale = $derived($maintenance.status?.stale ?? 0);

  let unsubscribe = () => {};

  onMount(() => {
    // The projection status is cheap and is the one thing worth knowing before any button is
    // pressed: it decides whether half the rule library can say anything at all.
    maintenance.loadStatus();
    unsubscribe = api.on(CHANNELS.MAINTENANCE_PROGRESS, (p) => maintenance.onProgress(p));
  });
  onDestroy(() => unsubscribe());

  function runAction(action) {
    if (action === ACTION.BACKFILL) return maintenance.backfill();
    if (action === ACTION.REPAIR) return maintenance.repair();
    if (action === ACTION.REBUILD) return maintenance.rebuild();
    return Promise.resolve(false);
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
  {#if busy}
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
      <Button variant="tonal" onclick={() => maintenance.backfill()} disabled={busy}>
        {UI.MAINT_ACTION.backfill}
      </Button>
    {/if}
  </section>

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
