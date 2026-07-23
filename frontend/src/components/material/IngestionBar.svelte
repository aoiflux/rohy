<script>
  // Global ingestion indicator. Mounted once at the app root so an ingest in flight is
  // visible from EVERY view, not just the Dashboard it was started from.
  //
  // Without this, starting an ingest and navigating to Events or Graph left no sign that
  // anything was happening — the app looked idle while it was busy, which reads as "my
  // click did nothing".
  import { slide } from 'svelte/transition';
  import { ingestion, progressFraction } from '../../stores/ingestion.js';
  import { route } from '../../stores/router.js';
  import { motion } from '../../lib/motion.js';
  import { UI, MOTION, ROUTES, INGEST_STATE, INGEST_LIFECYCLE } from '../../lib/consts/index.js';
  import ProgressBar from './ProgressBar.svelte';

  const running = $derived($ingestion.state === INGEST_STATE.RUNNING);
  const paused = $derived($ingestion.lifecycle === INGEST_LIFECYCLE.PAUSED);
  const stopping = $derived($ingestion.lifecycle === INGEST_LIFECYCLE.STOPPING);
  const fraction = $derived(progressFraction($ingestion.progress));

  // A run with no chunk total (live capture, or a source that cannot be sized up front)
  // gets an indeterminate sweep rather than a fake percentage. A paused run holds its bar
  // still, so "paused" never looks like "still working".
  const value = $derived(paused ? fraction : $ingestion.progress.chunks_total ? fraction : null);

  function fileName(path) {
    if (!path) return '';
    return String(path).split(/[\\/]/).pop();
  }

  function fmt(n) {
    return new Intl.NumberFormat().format(n || 0);
  }

  const label = $derived.by(() => {
    if (stopping) return UI.STATUS_STOPPING;
    if (paused) return UI.STATUS_PAUSED;
    return UI.STATUS_RUNNING;
  });
</script>

{#if running}
  <button
    class="bar"
    class:paused
    type="button"
    transition:slide={motion(MOTION.FAST)}
    onclick={() => route.go(ROUTES.DASHBOARD)}
    title={UI.INGEST_BAR_HINT}
  >
    <span class="state">{label}</span>
    {#if $ingestion.path}
      <span class="file" title={$ingestion.path}>{fileName($ingestion.path)}</span>
    {/if}
    <span class="track"><ProgressBar {value} /></span>
    <span class="counts">
      {fmt($ingestion.progress.records_persisted)}
      {UI.INGEST_BAR_STORED}
    </span>
  </button>
{/if}

<style>
  /* A slim strip under the title bar: informative without stealing the view. It is a
     button because its whole job is to take you to the controls. */
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-2) var(--space-4);
    background: var(--color-surface);
    border: none;
    border-bottom: 1px solid var(--color-outline);
    color: var(--color-on-surface-variant);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    text-align: left;
    cursor: pointer;
  }
  .bar:hover {
    background: var(--color-surface-variant);
  }
  .bar:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  .state {
    font-weight: 800;
    color: var(--color-primary);
    white-space: nowrap;
  }
  .bar.paused .state {
    color: var(--color-accent);
  }
  .file {
    font-family: var(--font-mono);
    max-width: 28ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .track {
    flex: 1;
    min-width: 80px;
  }
  .counts {
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
</style>
