<script>
  // "Some of this case cannot be correlated on fields yet — here is why, and here is the fix."
  //
  // This exists because of the one failure this project treats as worse than an error: a field,
  // temporal or lineage rule run over events ingested before the correlation projection existed
  // returns a small number of matches, and a small number reads as a small RESULT. Nothing else
  // on the page distinguishes "this pattern is rare" from "most of your events could not be
  // considered".
  //
  // So it is a persistent notice rather than a snackbar. A transient toast is exactly the wrong
  // shape for a caveat that changes how every subsequent run should be read.
  import { UI } from '../../lib/consts/index.js';

  let {
    /** {total, current, stale} from CorrelationKeyStatus, or null before it is known */
    status = null,
    running = false,
    progress = null,
    onbackfill = undefined,
    oncancel = undefined,
  } = $props();

  const fmt = (n) => new Intl.NumberFormat().format(n ?? 0);
  const stale = $derived(status?.stale ?? 0);
  const pct = $derived(
    progress && progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0,
  );
</script>

{#if running}
  <div class="notice working" role="status">
    <div class="text">
      <strong>{UI.BACKFILL_RUNNING}</strong>
      <span>{progress ? `${fmt(progress.done)} / ${fmt(progress.total)}` : ''}</span>
    </div>
    <div class="bar"><div class="fill" style={`width:${pct}%`}></div></div>
    <button class="ghost" onclick={() => oncancel?.()}>{UI.BACKFILL_CANCEL}</button>
  </div>
{:else if stale > 0}
  <div class="notice" role="status">
    <div class="text">
      <strong>{fmt(stale)} {UI.BACKFILL_STALE_HEADLINE}</strong>
      <span>{UI.BACKFILL_STALE_DETAIL}</span>
    </div>
    <button class="run" onclick={() => onbackfill?.()}>{UI.BACKFILL_RUN}</button>
  </div>
{/if}

<style>
  .notice {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    margin: 0 var(--space-5) var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: 1px solid var(--color-warning);
    border-left-width: 3px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--color-warning) 10%, transparent);
  }
  .notice.working {
    border-color: var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 220px;
  }
  strong {
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
  span {
    font-family: var(--font-sans);
    font-size: 0.76rem;
    line-height: 1.5;
    color: var(--color-on-surface-muted);
    font-variant-numeric: tabular-nums;
  }
  .bar {
    flex-basis: 100%;
    height: 3px;
    border-radius: 2px;
    background: var(--color-outline);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: var(--color-primary);
    transition: width var(--motion-medium) var(--motion-ease);
  }
  button {
    border-radius: var(--radius-md);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    font-weight: 600;
    padding: var(--space-2) var(--space-4);
    cursor: pointer;
  }
  .run {
    border: none;
    background: var(--color-primary);
    color: var(--color-on-primary);
  }
  .ghost {
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    color: var(--color-on-surface);
  }
</style>
