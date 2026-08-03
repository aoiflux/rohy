<script>
  // The relationship heatmap: relation counts per (time bucket × group).
  //
  // One component in two shapes. `strip` draws a single row — the totals — sized to sit over the
  // timeline's histogram; the full form draws one row per lane. They are the same component
  // because the strip and the matrix must agree about every number on screen, and two components
  // reading the same summary would eventually stop agreeing.
  //
  // A heatmap is unusually easy to lie with, because colour has no axis. The rules that keep this
  // one honest live in lib/graph/heatmap.js — a linear ramp anchored at the matrix maximum, and
  // an empty cell drawn as empty rather than as the palest shade.
  import { UI, TIMELINE } from '../../lib/consts/index.js';
  import { intensity, describe, lanesFor, cellTitle, totals, sliceToView } from '../../lib/graph/heatmap.js';

  let {
    /** HeatmapSummary from RelationHeatmap, or null before one has been fetched. */
    summary = null,
    /** Single-row mode, for the overlay above the timeline plot. */
    strip = false,
    /** Left inset, so a strip's columns line up with the plot it sits over. */
    inset = 0,
    /**
     * The timeline's zoom window, as fractions of the full extent.
     *
     * 🔒 The heatmap is fetched over the timeline's FULL extent at the same bucket count, so
     * index i means the same instant in both — and zoom is then applied to both arrays
     * identically. Re-fetching over the visible window instead would re-bucket the heatmap, and
     * the strip would drift out of alignment with the histogram beneath it.
     */
    view = null,
  } = $props();

  const read = $derived(describe(summary));
  const buckets = $derived(summary?.buckets ?? []);
  const max = $derived(summary?.max ?? 0);

  const shown = $derived(lanesFor(summary?.lanes ?? [], strip ? 0 : TIMELINE.HEATMAP_LANES));
  const windowBuckets = $derived(sliceToView(buckets, view));
  const stripRow = $derived(sliceToView(totals(summary?.lanes ?? [], buckets.length), view));
  // In strip mode the ramp is anchored at the busiest COLUMN, not the busiest cell: the row is
  // sums, so scaling it by a single lane's peak would saturate almost every column.
  const stripMax = $derived(stripRow.reduce((m, v) => (v > m ? v : m), 0));

  const rows = $derived(
    strip
      ? [{ key: UI.HEATMAP_ALL, total: summary?.placed ?? 0, counts: stripRow, max: stripMax }]
      : shown.lanes.map((l) => ({ ...l, max, counts: sliceToView(l.counts ?? [], view) })),
  );
</script>

<div class="heat" class:strip style="padding-left: {inset}px">
  {#if !summary}
    <p class="hint">{UI.HEATMAP_NOT_RUN}</p>
  {:else if read.empty}
    <!-- Nothing placeable is a real answer, not an error. Say which of the two reasons it is. -->
    <p class="hint">{UI.HEATMAP_EMPTY}</p>
  {:else}
    {#each rows as row (row.key)}
      <div class="row">
        {#if !strip}
          <span class="rowlabel" title={row.key}>{row.key}</span>
          <span class="rowtotal">{row.total}</span>
        {/if}
        <div class="cells" role="img" aria-label="{row.key}: {row.total}">
          {#each row.counts as c, i (i)}
            <!-- Zero draws nothing at all. An empty cell and an almost-empty one must not be a
                 few percent of lightness apart. -->
            <span
              class="cell"
              class:on={c > 0}
              style="opacity: {intensity(c, row.max)}"
              title={cellTitle(row, windowBuckets[i], c)}
            ></span>
          {/each}
        </div>
      </div>
    {/each}

    {#if shown.hidden > 0}
      <p class="hint">{shown.hidden} {UI.HEATMAP_LANES_HIDDEN}</p>
    {/if}
  {/if}

  <!-- Caveats last and always: a relation that could not be placed is not a relation that does
       not exist, and a matrix that quietly omitted it would be smaller than the graph. -->
  {#if !strip}
    {#each read.notes as n (n.kind)}
      <p class="note">
        <b>{n.count}</b>
        {n.kind === 'undated'
          ? UI.HEATMAP_UNDATED
          : n.kind === 'outside'
            ? UI.HEATMAP_OUTSIDE
            : UI.HEATMAP_UNACCOUNTED}
      </p>
    {/each}
  {/if}
</div>

<style>
  .heat {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .rowlabel {
    flex: none;
    width: 132px;
    overflow: hidden;
    font-size: 0.74rem;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--color-on-surface-muted);
  }
  .rowtotal {
    flex: none;
    width: 40px;
    font-size: 0.72rem;
    font-variant-numeric: tabular-nums;
    text-align: right;
    color: var(--color-on-surface-muted);
  }
  .cells {
    display: flex;
    flex: 1;
    gap: 1px;
    height: 14px;
  }
  .strip .cells {
    height: 8px;
  }
  .cell {
    flex: 1;
    min-width: 0;
    border-radius: 1px;
    /* Transparent by default; `on` supplies the colour and `opacity` the intensity, so an empty
       cell is genuinely absent rather than the faintest step of a ramp. */
    background: transparent;
  }
  .cell.on {
    background: var(--color-primary);
  }
  .hint {
    margin: 0;
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
  }
  .note {
    margin: 0;
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
  }
</style>
