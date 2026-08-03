<script>
  // The cluster outline layer: one convex hull per group, drawn beneath the edges and cards.
  //
  // It sits under everything because a hull is context, not content. Drawn on top it would
  // obscure the evidence it is meant to frame, and its fill would tint every card inside it.
  //
  // The label carries the COUNT, always. That is the same rule the collapsed card follows: a
  // group must never be able to hide how much it contains.
  import { GRAPH, UI } from '../../lib/consts/index.js';
  import { clusterHull } from '../../lib/graph/clusters.js';

  let {
    /** @type {{id:string,label:string,node_ids:number[],size:number,overlapping?:boolean}[]} */
    clusters = [],
    /** Canvas nodes keyed by event id. */
    nodes = {},
    /** @type {Set<string>} ids currently folded — a collapsed group draws no hull, its card is the group. */
    collapsed = new Set(),
    /** (clusterId) => void — fold this group. */
    ontoggle = undefined,
  } = $props();

  const geom = { width: GRAPH.NODE_WIDTH, height: GRAPH.NODE_HEIGHT, pad: GRAPH.HULL_PAD };

  const drawn = $derived(
    (clusters || [])
      .filter((c) => c && !collapsed.has(c.id))
      .map((c) => ({ c, hull: clusterHull(c.node_ids, nodes, geom) }))
      .filter((d) => d.hull !== null),
  );

  // Sized from the text rather than measured, because measuring means a layout read per hull on
  // every pan. The estimate errs wide; a tag slightly too big reads as padding.
  function tagWidth(c) {
    return `${c.label} · ${c.size}`.length * 6.6 + 18;
  }

  // Colour is derived from the cluster id rather than assigned from a palette in order, so a
  // group keeps its colour when the list is re-ordered or another group is folded away.
  function hue(id) {
    let h = 0;
    for (let i = 0; i < id.length; i += 1) h = (h * 31 + id.charCodeAt(i)) % 360;
    return h;
  }
</script>

<svg class="hulls" width="1" height="1" aria-hidden="true">
  {#each drawn as d (d.c.id)}
    <g style="--hull-hue: {hue(d.c.id)}">
      <path class="hull" d={d.hull.path} />
      <!-- The label is a fold affordance as well as a name, so a group can be closed from the
           canvas rather than only from the panel. -->
      <g
        class="tag"
        transform="translate({d.hull.anchor.x}, {d.hull.anchor.y - 10})"
        role="button"
        tabindex="-1"
        aria-label="{UI.CLUSTER_COLLAPSE} {d.c.label}"
        onclick={() => ontoggle?.(d.c.id)}
        onkeydown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') ontoggle?.(d.c.id);
        }}
      >
        <rect x="0" y="-14" width={tagWidth(d.c)} height="20" rx="6" />
        <text x="8" y="1">{d.c.label} · {d.c.size}</text>
      </g>
    </g>
  {/each}
</svg>

<style>
  .hulls {
    position: absolute;
    left: 0;
    top: 0;
    overflow: visible;
    /* Context, never a pointer target — except the tag, which opts back in below. */
    pointer-events: none;
  }
  .hull {
    fill: hsl(var(--hull-hue) 70% 55% / 0.08);
    stroke: hsl(var(--hull-hue) 70% 55% / 0.55);
    stroke-width: 1.5;
    stroke-dasharray: 8 5;
    vector-effect: non-scaling-stroke;
  }
  .tag {
    pointer-events: auto;
    cursor: pointer;
  }
  .tag rect {
    fill: var(--color-surface);
    stroke: hsl(var(--hull-hue) 70% 55% / 0.8);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .tag:hover rect {
    filter: brightness(1.1);
  }
  .tag text {
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 700;
    fill: var(--color-on-surface);
    user-select: none;
  }
</style>
