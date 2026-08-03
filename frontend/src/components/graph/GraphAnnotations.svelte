<script>
  // The annotation overlay: regions, arrows and note pins, drawn in world space.
  //
  // It sits above the hulls and below the node cards. A hull is context and belongs underneath
  // everything; an annotation is a mark ABOUT the cards, so it must be visible against them —
  // but it must not cover the evidence, which is why pins sit beside a card rather than on it.
  //
  // 🔒 Only annotations that can be honestly placed are drawn. An anchor whose event has left the
  // case, or is not on this canvas, has no position — and a pin at the origin or clamped to the
  // nearest card would be a mark pointing at the wrong evidence. The count is reported in the
  // panel instead.
  import { GRAPH } from '../../lib/consts/index.js';
  import { placed, arrowPath, centre, KIND } from '../../lib/graph/annotations.js';

  let {
    /** @type {{layers:any[],items:any[]}} */
    doc = { layers: [], items: [] },
    /** hash -> live node id, resolved by the backend. */
    nodeOf = {},
    /** Canvas nodes by id. */
    nodes = {},
  } = $props();

  const geom = { width: GRAPH.NODE_WIDTH, height: GRAPH.NODE_HEIGHT };
  const drawn = $derived(placed(doc, nodeOf, nodes, geom));

  const colourOf = (p) => p.layer?.colour || 'var(--color-accent)';
</script>

<svg class="annotations" width="1" height="1" aria-hidden="true">
  <defs>
    <marker id="ann-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
      <path d="M 0 0 L 10 5 L 0 10 z" fill="context-stroke" />
    </marker>
  </defs>

  {#each drawn.items as p (p.item.id)}
    {#if p.item.kind === KIND.REGION}
      <g class="ann">
        <rect
          class="region"
          x={p.from.x}
          y={p.from.y}
          width={Math.max(p.from.w, 1)}
          height={Math.max(p.from.h, 1)}
          rx="10"
          style="--ann: {colourOf(p)}"
        />
        {#if p.item.text}
          <text class="rlabel" x={p.from.x + 10} y={p.from.y + 20} style="--ann: {colourOf(p)}">
            {p.item.text}
          </text>
        {/if}
      </g>
    {:else if p.item.kind === KIND.ARROW}
      <g class="ann">
        <path
          class="arrow"
          d={arrowPath(p.from, p.to)}
          fill="none"
          style="--ann: {colourOf(p)}"
          marker-end="url(#ann-arrow)"
        />
        {#if p.item.text}
          <text class="alabel" x={(centre(p.from).x + centre(p.to).x) / 2} y={(centre(p.from).y + centre(p.to).y) / 2 - 6}>
            {p.item.text}
          </text>
        {/if}
      </g>
    {:else}
      <!-- A note pin: a small marker beside the card, with the text in a bubble next to it. -->
      <g class="ann pin" transform="translate({p.from.x + GRAPH.PIN_OFFSET}, {p.from.y})">
        <circle class="dot" cx="0" cy="0" r="6" style="--ann: {colourOf(p)}" />
        {#if p.item.text}
          <foreignObject x="10" y="-14" width={GRAPH.PIN_MAX_W} height="120">
            <div class="bubble" style="--ann: {colourOf(p)}">{p.item.text}</div>
          </foreignObject>
        {/if}
      </g>
    {/if}
  {/each}
</svg>

<style>
  /* The whole layer is pass-through. Annotations are created, edited and deleted from the layer
     panel, not from the canvas — so making the marks clickable here would be an affordance that
     leads nowhere, and would swallow canvas drags on the way. */
  .annotations {
    position: absolute;
    left: 0;
    top: 0;
    overflow: visible;
    pointer-events: none;
  }
  .region {
    fill: color-mix(in srgb, var(--ann) 10%, transparent);
    stroke: var(--ann);
    stroke-width: 2;
    stroke-dasharray: 10 6;
    vector-effect: non-scaling-stroke;
  }
  .arrow {
    stroke: var(--ann);
    stroke-width: 2.5;
    vector-effect: non-scaling-stroke;
  }
  .dot {
    fill: var(--ann);
    stroke: var(--color-surface);
    stroke-width: 2;
    vector-effect: non-scaling-stroke;
  }
  text {
    font-family: var(--font-sans);
    font-size: 12px;
    font-weight: 700;
    user-select: none;
  }
  .rlabel {
    fill: var(--ann);
  }
  .alabel {
    fill: var(--color-on-surface);
    text-anchor: middle;
  }
  .bubble {
    display: inline-block;
    max-width: 100%;
    padding: 4px 8px;
    font-family: var(--font-sans);
    font-size: 12px;
    line-height: 1.35;
    color: var(--color-on-surface);
    background: var(--color-surface);
    border: 1px solid var(--ann);
    border-radius: 8px;
    box-shadow: 0 1px 4px rgb(0 0 0 / 0.18);
  }
</style>
