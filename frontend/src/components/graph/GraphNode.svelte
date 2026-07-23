<script>
  // A graph node rendered as a Material card in WORLD coordinates (the parent applies
  // the pan/zoom transform). Purely presentational: it exposes `data-node-id` and
  // `data-handle` so the canvas can own the whole interaction state machine via
  // hit-testing (which also lets the canvas use a single pointer capture per gesture).
  // Border colour is a const-driven token by node state.
  import { GRAPH, NODE_STATE, UI, OWNS_CONTEXT_MENU_ATTR } from '../../lib/consts/index.js';

  // connectMode: the whole card is a link source, so no handle is needed and the card
  // advertises that with a crosshair + ring. connectSource: this node is the one currently
  // being dragged from.
  let {
    node,
    selected = false,
    highlighted = false,
    connectMode = false,
    connectSource = false,
    // entering: this node was JUST added to the graph (not merely re-rendered by
    // virtualization), so it earns a one-off entrance.
    entering = false,
    // finding: the analyst's own annotation for this event, if any (P25). Rendered as a
    // distinct accent mark so an authored judgement never looks like a derived property of
    // the node.
    finding = null,
  } = $props();

  const borderToken = $derived(
    selected ? NODE_STATE.SELECTED : highlighted ? NODE_STATE.HIGHLIGHTED : NODE_STATE.DEFAULT,
  );

  function fmtTime(ts) {
    const t = new Date(ts).getTime();
    return Number.isFinite(t) ? new Date(t).toLocaleString() : '';
  }
</script>

<div
  class="node"
  class:selected
  class:highlighted
  class:connectmode={connectMode}
  class:connectsource={connectSource}
  class:flagged={finding && finding.flagged}
  class:entering
  data-node-id={node.event.id}
  {...{ [OWNS_CONTEXT_MENU_ATTR]: '' }}
  style="left: {node.x}px; top: {node.y}px; width: {GRAPH.NODE_WIDTH}px; height: {GRAPH.NODE_HEIGHT}px; border-color: var(--{borderToken})"
>
  <div class="head">
    <span class="eid">
      {node.event.event_id}
      {#if finding && finding.flagged}
        <span class="flag" title={UI.FINDING_FLAG_TITLE} aria-label={UI.FINDING_FLAG_BADGE_ARIA}>★</span>
      {/if}
      {#if finding && finding.note}
        <span class="flag" title={finding.note} aria-label={UI.FINDING_NOTE_BADGE_ARIA}>✎</span>
      {/if}
    </span>
    <span class="chan">{node.event.channel}</span>
  </div>
  <div class="prov">{node.event.provider}</div>
  {#if finding && finding.tags && finding.tags.length}
    <div class="tags">
      {#each finding.tags.slice(0, GRAPH.NODE_TAG_LIMIT) as t (t)}
        <span class="tag">{t}</span>
      {/each}
    </div>
  {/if}
  <div class="ts">{fmtTime(node.event.timestamp)}</div>

  <!-- Connect handle: drag from here to another node to create an edge. Its visible size
       is generous and its hit area is larger still (the ::before pad), so linking never
       requires pixel-precision. In connect mode the whole card is the source, so the
       handle steps out of the way. -->
  {#if !connectMode}
    <span
      class="handle"
      data-handle="1"
      data-node-id={node.event.id}
      title={UI.CONNECT_HANDLE_TITLE}
      aria-label={UI.CONNECT_HANDLE_TITLE}
      style="width: {GRAPH.CONNECT_HANDLE_SIZE}px; height: {GRAPH.CONNECT_HANDLE_SIZE}px; --hit-pad: {GRAPH.CONNECT_HANDLE_HIT_PAD}px"
    >
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path
          d="M9 12h6M8.5 8.5H7a3.5 3.5 0 0 0 0 7h1.5M15.5 8.5H17a3.5 3.5 0 0 1 0 7h-1.5"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
        />
      </svg>
    </span>
  {/if}
</div>

<style>
  .node {
    position: absolute;
    background: var(--color-surface);
    color: var(--color-on-surface);
    border: 2px solid var(--color-outline);
    border-radius: var(--radius-md);
    box-shadow: var(--elevation-1);
    padding: var(--space-3);
    box-sizing: border-box;
    cursor: grab;
    user-select: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    overflow: hidden;
    touch-action: none;
  }
  .node.selected {
    box-shadow: var(--elevation-2);
  }
  /* A flagged node keeps its state border (selected/highlighted are interaction state) and
     gains an accent bar instead, so the analyst's mark and the canvas's own state never
     compete for the same channel. */
  .node.flagged {
    box-shadow: inset 4px 0 0 var(--color-accent), var(--elevation-1);
  }
  .flag {
    color: var(--color-accent);
    font-size: 0.8rem;
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 3px;
    overflow: hidden;
    max-height: 18px;
  }
  .tag {
    font-family: var(--font-sans);
    font-size: 0.6rem;
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, transparent);
    border: 1px solid var(--color-accent);
    border-radius: 999px;
    padding: 0 5px;
    line-height: 1.4;
    white-space: nowrap;
    max-width: 10ch;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .eid {
    font-family: var(--font-sans);
    font-weight: 800;
    font-size: 1rem;
    color: var(--color-primary);
  }
  .chan {
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .prov {
    font-family: var(--font-sans);
    font-size: 0.82rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .ts {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* The link handle sits on the node's right edge, vertically centred — a predictable
     place to reach for, unlike a corner. It is always visible (not hover-only) so the
     ability to link is discoverable without hunting. */
  .handle {
    position: absolute;
    right: -12px;
    top: 50%;
    transform: translateY(-50%);
    border-radius: 50%;
    background: var(--color-accent);
    color: var(--color-on-accent);
    border: 2px solid var(--color-surface);
    cursor: crosshair;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0.85;
    box-shadow: var(--elevation-1);
    transition: transform var(--motion-fast) var(--motion-ease), opacity var(--motion-fast) var(--motion-ease),
      box-shadow var(--motion-fast) var(--motion-ease);
  }
  .handle svg {
    width: 70%;
    height: 70%;
    pointer-events: none;
  }
  /* The real target: an invisible pad extending well past the visible dot, so the pointer
     does not have to land on the circle itself. This is what removes the pixel-precision
     the old corner dot demanded.
     The pad grows outward, above and below — but NOT inward (left: 0), so it cannot eat
     into the card body and steal drags from it (R-CX1). */
  .handle::before {
    content: '';
    position: absolute;
    inset: calc(-1 * var(--hit-pad, 12px)) calc(-1 * var(--hit-pad, 12px)) calc(-1 * var(--hit-pad, 12px)) 0;
    border-radius: 50%;
  }
  .node:hover .handle,
  .handle:hover {
    opacity: 1;
    transform: translateY(-50%) scale(1.15);
    box-shadow: 0 0 0 6px color-mix(in srgb, var(--color-accent) 28%, transparent), var(--elevation-2);
  }
  @media (prefers-reduced-motion: reduce) {
    .handle {
      transition: none;
    }
    .node:hover .handle,
    .handle:hover {
      transform: translateY(-50%);
    }
  }

  /* Connect mode: the whole card is the link source, and it says so. */
  .node.connectmode {
    cursor: crosshair;
  }
  .node.connectmode:hover {
    border-color: var(--color-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 30%, transparent), var(--elevation-2);
  }
  .node.connectsource {
    border-color: var(--color-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 55%, transparent), var(--elevation-2);
  }

  /* One-off entrance for a genuinely new node. Keyframes end at the resting state with no
     fill-mode, so a node is never left scaled or transparent if the animation is cut off.
     Position is untouched — animating that would fight dragging. */
  .entering {
    animation: node-in var(--motion-medium) var(--motion-ease);
  }
  @keyframes node-in {
    from {
      opacity: 0;
      transform: scale(0.94);
    }
    to {
      opacity: 1;
      transform: scale(1);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .entering {
      animation: none;
    }
  }

  /* Snap target: unmistakable, because the whole point is that a near-miss still lands. */
  .node.highlighted {
    border-color: var(--color-accent);
    box-shadow: 0 0 0 5px color-mix(in srgb, var(--color-accent) 45%, transparent), var(--elevation-2);
  }
</style>
