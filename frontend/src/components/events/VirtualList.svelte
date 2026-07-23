<script>
  // Fixed-height windowed list (P6.1). Renders only the rows intersecting the scroll
  // viewport (± overscan), with spacer divs preserving total scroll height, so tens of
  // thousands of events stay smooth. The parent supplies a `row` snippet per item.
  import { EVENTS_LIST } from '../../lib/consts/index.js';

  let {
    items = [],
    rowHeight = EVENTS_LIST.ROW_HEIGHT,
    overscan = EVENTS_LIST.OVERSCAN,
    row,
    onEndReached = undefined,
  } = $props();

  let scrollTop = $state(0);
  let viewportH = $state(0);

  const total = $derived(items.length);
  const start = $derived(Math.max(0, Math.floor(scrollTop / rowHeight) - overscan));
  const visibleCount = $derived(Math.ceil((viewportH || 0) / rowHeight) + overscan * 2);
  const end = $derived(Math.min(total, start + visibleCount));
  const slice = $derived(items.slice(start, end));
  const padTop = $derived(start * rowHeight);
  const padBottom = $derived(Math.max(0, (total - end) * rowHeight));

  function onscroll(e) {
    const el = e.currentTarget;
    scrollTop = el.scrollTop;
    // Ask the parent to load more when the user nears the end (P1 progressive loading).
    // The parent's loader is guarded, so firing repeatedly is safe.
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - rowHeight * EVENTS_LIST.LOAD_MORE_ROWS) {
      onEndReached?.();
    }
  }
</script>

<div class="scroller" bind:clientHeight={viewportH} {onscroll}>
  <div style="height: {padTop}px"></div>
  {#each slice as item, i (item.id ?? start + i)}
    <div class="vrow" style="height: {rowHeight}px">
      {@render row?.(item, start + i)}
    </div>
  {/each}
  <div style="height: {padBottom}px"></div>
</div>

<style>
  .scroller {
    height: 100%;
    overflow-y: auto;
    overflow-x: hidden;
  }
  .vrow {
    box-sizing: border-box;
    overflow: hidden;
  }
</style>
