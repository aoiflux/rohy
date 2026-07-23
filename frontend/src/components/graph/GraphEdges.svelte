<script>
  // Edge layer. Renders inside the parent's transformed world layer, so it draws in
  // world coordinates; strokes use vector-effect:non-scaling-stroke so line weight
  // stays constant across zoom. Edges are DIRECTED (arrowhead at the target) and show
  // their free-text label (falling back to the relation type). Colour is a
  // const-driven token per relation type. Only edges with a visible endpoint draw.
  import { GRAPH, RELATION_COLOR_TOKEN, RELATION_LABEL, RELATIONS } from '../../lib/consts/index.js';

  // freshEdges: ids of edges created moments ago, which draw themselves in once as
  // confirmation that the link landed. Everything else renders statically.
  let { edges = [], nodes = {}, visibleIds = new Set(), tempEdge = null, freshEdges = new Set() } = $props();

  const HALF_W = GRAPH.NODE_WIDTH / 2;
  const HALF_H = GRAPH.NODE_HEIGHT / 2;

  function center(id) {
    const n = nodes[id];
    if (!n) return null;
    return { x: n.x + HALF_W, y: n.y + HALF_H };
  }

  // Curve from a to b, stopping short of b so the arrowhead sits at the node border.
  function curve(a, b, inset) {
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const len = Math.hypot(dx, dy) || 1;
    const end = { x: b.x - (dx / len) * inset, y: b.y - (dy / len) * inset };
    const mx = a.x + (end.x - a.x) * 0.5;
    return { d: `M ${a.x} ${a.y} C ${mx} ${a.y}, ${mx} ${end.y}, ${end.x} ${end.y}`, mid: { x: mx, y: (a.y + end.y) / 2 } };
  }

  function colorToken(relationType) {
    return RELATION_COLOR_TOKEN[relationType] || RELATION_COLOR_TOKEN[RELATIONS.DEFAULT];
  }
  function edgeText(e) {
    return (e.relation_label && e.relation_label.trim()) || RELATION_LABEL[e.relation_type] || '';
  }

  const inset = HALF_W * 0.7;
  const drawn = $derived(
    edges
      .filter((e) => (visibleIds.has(e.from) || visibleIds.has(e.to)) && nodes[e.from] && nodes[e.to])
      .map((e) => {
        const c = curve(center(e.from), center(e.to), inset);
        return { e, d: c.d, mid: c.mid, token: colorToken(e.relation_type), text: edgeText(e) };
      }),
  );
</script>

<svg class="edges" width="1" height="1" aria-hidden="true">
  <defs>
    <marker id="em-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
      <path d="M 0 0 L 10 5 L 0 10 z" fill="context-stroke" />
    </marker>
  </defs>

  {#each drawn as d (d.e.id)}
    <path
      class:drawin={freshEdges.has(d.e.id)}
      d={d.d}
      fill="none"
      stroke="var(--{d.token})"
      stroke-width="2"
      vector-effect="non-scaling-stroke"
      marker-end="url(#em-arrow)"
    />
    {#if d.text}
      <g class="label" data-edge-id={d.e.id} transform="translate({d.mid.x}, {d.mid.y})">
        <rect
          x={-(d.text.length * 3.4 + 8)}
          y="-10"
          width={d.text.length * 6.8 + 16}
          height="20"
          rx="6"
          fill="var(--color-surface)"
          stroke="var(--{d.token})"
        />
        <text x="0" y="4" text-anchor="middle" fill="var(--color-on-surface)" font-size="11">{d.text}</text>
      </g>
    {/if}
  {/each}

  {#if tempEdge}
    <!-- The in-progress link reads its own state: dashed and thin while it has no target,
         solid and heavier once it has snapped to one — so "will this land?" is answered
         before the pointer is released (P18.3). -->
    <path
      class="temp"
      class:valid={tempEdge.valid}
      d={curve(tempEdge.from, tempEdge.to, tempEdge.valid ? inset : 0).d}
      fill="none"
      stroke="var(--color-accent)"
      stroke-width={tempEdge.valid ? 3 : 2}
      stroke-dasharray={tempEdge.valid ? 'none' : '6 5'}
      vector-effect="non-scaling-stroke"
      marker-end="url(#em-arrow)"
    />
  {/if}
</svg>

<style>
  .edges {
    position: absolute;
    left: 0;
    top: 0;
    overflow: visible;
    pointer-events: none;
  }
  /* Only the label pills are interactive (edit/delete); the paths stay pass-through. */
  .label {
    pointer-events: auto;
    cursor: pointer;
  }
  .label:hover rect {
    filter: brightness(1.08);
  }
  text {
    font-family: var(--font-sans);
    font-weight: 700;
    user-select: none;
  }
  .temp {
    opacity: 0.75;
  }
  .temp.valid {
    opacity: 1;
  }

  /* A freshly created edge draws itself from source to target, so the link visibly lands
     rather than blinking into existence. The dash length is generous so any edge length is
     fully covered; the animation ends on the resting (fully drawn) state without a fill,
     so an interrupted animation can never leave an edge partially drawn. */
  .drawin {
    stroke-dasharray: 2000;
    animation: edge-draw var(--motion-slow) var(--motion-ease);
  }
  @keyframes edge-draw {
    from {
      stroke-dashoffset: 2000;
    }
    to {
      stroke-dashoffset: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .drawin {
      stroke-dasharray: none;
      animation: none;
    }
  }
</style>
