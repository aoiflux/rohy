/**
 * Drawing clusters on the canvas: the outline, and the collapse (P29).
 *
 * The grouping itself is computed in Go — union-find components, rules, correlation slots. What
 * lives here is purely how a group is DRAWN: the convex hull around its cards, and the mapping
 * that lets a collapsed group stand in for its members without the edge layer knowing anything
 * about clusters at all.
 *
 * Two things this module is careful about, both because a wrong answer here is a wrong claim
 * about the evidence rather than a cosmetic glitch:
 *
 *  - A hull encloses the CARDS, not their origins. A hull drawn through the top-left corners
 *    would cut through half the cards it claims to contain, so nodes would appear to be outside
 *    a group they are in.
 *  - A collapsed group's edges are re-pointed at it, never dropped. An edge that vanished when a
 *    group closed would make the collapsed card look less connected than it is — and collapsing
 *    is what an analyst does to a big graph precisely to see the connections between groups.
 */

/** Node ids arrive as numbers from the backend and as object keys (strings) from the canvas. */
const key = (id) => String(id);

/** PROXY_PREFIX marks a synthetic id belonging to a collapsed cluster rather than to an event. */
export const PROXY_PREFIX = 'c:';

/** isProxy distinguishes a collapsed cluster's card from a real event's. */
export function isProxy(id) {
  return typeof id === 'string' && id.startsWith(PROXY_PREFIX);
}

/**
 * cardCorners returns a node card's four corners. Using corners rather than the origin is what
 * makes a hull actually contain the cards it is drawn around.
 * @param {{x:number,y:number}} node
 */
export function cardCorners(node, w, h) {
  return [
    { x: node.x, y: node.y },
    { x: node.x + w, y: node.y },
    { x: node.x + w, y: node.y + h },
    { x: node.x, y: node.y + h },
  ];
}

function cross(o, a, b) {
  return (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);
}

/**
 * convexHull computes the convex hull of a point set (Andrew's monotone chain, O(n log n)).
 * Returns the hull vertices in counter-clockwise order. Fewer than three distinct points have no
 * hull, so the distinct points are returned as-is and the caller falls back to a bounding box.
 * @param {{x:number,y:number}[]} points
 */
export function convexHull(points) {
  const seen = new Set();
  const pts = [];
  for (const p of points || []) {
    if (!p || !Number.isFinite(p.x) || !Number.isFinite(p.y)) continue;
    const k = `${p.x},${p.y}`;
    if (seen.has(k)) continue;
    seen.add(k);
    pts.push({ x: p.x, y: p.y });
  }
  if (pts.length < 3) return pts;
  pts.sort((a, b) => a.x - b.x || a.y - b.y);

  const half = (input) => {
    const out = [];
    for (const p of input) {
      while (out.length >= 2 && cross(out[out.length - 2], out[out.length - 1], p) <= 0) out.pop();
      out.push(p);
    }
    out.pop(); // the last point starts the other half
    return out;
  };
  return [...half(pts), ...half([...pts].reverse())];
}

/** centroid of a point set; {x:0,y:0} for an empty one. */
export function centroid(points) {
  const pts = points || [];
  if (pts.length === 0) return { x: 0, y: 0 };
  let x = 0;
  let y = 0;
  for (const p of pts) {
    x += p.x;
    y += p.y;
  }
  return { x: x / pts.length, y: y / pts.length };
}

/**
 * padOutward pushes each hull vertex away from the centroid, so the outline sits clear of the
 * cards instead of touching them. Offsetting along the true edge normals would be more correct
 * for a very elongated hull; from the centroid is within a pixel or two at the padding used here
 * and does not need the degenerate cases an edge offset does.
 * @param {{x:number,y:number}[]} hull
 */
export function padOutward(hull, pad) {
  if (!hull || hull.length === 0 || !pad) return hull || [];
  const c = centroid(hull);
  return hull.map((p) => {
    const dx = p.x - c.x;
    const dy = p.y - c.y;
    const len = Math.hypot(dx, dy);
    if (len === 0) return { x: p.x, y: p.y };
    return { x: p.x + (dx / len) * pad, y: p.y + (dy / len) * pad };
  });
}

/** hullPath renders a closed SVG path, or '' when there is nothing to draw. */
export function hullPath(points) {
  if (!points || points.length < 3) return '';
  return `${points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${round(p.x)} ${round(p.y)}`).join(' ')} Z`;
}

const round = (v) => Math.round(v * 100) / 100;

/**
 * clusterHull builds the outline for one cluster's members, plus the point its label hangs from
 * (the topmost vertex, so a label never lands inside the cards).
 *
 * Members not on the canvas are skipped rather than treated as being at the origin — a cluster
 * computed a moment ago can name a node that has since been removed, and anchoring it at 0,0
 * would stretch the outline across the whole world.
 *
 * @param {(number|string)[]} nodeIds
 * @param {Record<string,{x:number,y:number}>} nodes
 * @param {{width:number,height:number,pad:number}} geom
 */
export function clusterHull(nodeIds, nodes, geom) {
  const pts = [];
  let present = 0;
  for (const id of nodeIds || []) {
    const n = nodes?.[key(id)];
    if (!n || !Number.isFinite(n.x) || !Number.isFinite(n.y)) continue;
    present += 1;
    pts.push(...cardCorners(n, geom.width, geom.height));
  }
  if (present === 0) return null;

  let hull = convexHull(pts);
  if (hull.length < 3) hull = boundingBox(pts); // every card at one point, or a single card
  const padded = padOutward(hull, geom.pad);
  const path = hullPath(padded);
  if (!path) return null;

  const anchor = padded.reduce((best, p) => (p.y < best.y ? p : best), padded[0]);
  return { path, anchor: { x: anchor.x, y: anchor.y }, present };
}

function boundingBox(points) {
  const xs = points.map((p) => p.x);
  const ys = points.map((p) => p.y);
  const x0 = Math.min(...xs);
  const x1 = Math.max(...xs);
  const y0 = Math.min(...ys);
  const y1 = Math.max(...ys);
  return [
    { x: x0, y: y0 },
    { x: x1, y: y0 },
    { x: x1, y: y1 },
    { x: x0, y: y1 },
  ];
}

/**
 * memberIndex maps each node of a COLLAPSED cluster to that cluster's proxy id.
 *
 * A node belonging to two collapsed clusters — which rule grouping allows — is mapped to the
 * first one in cluster order. Order is the backend's (largest first, deterministic), so the
 * choice is stable; it is a choice all the same, which is why rule clusters carry `overlapping`.
 * @param {{id:string,node_ids:number[]}[]} clusters
 * @param {Set<string>} collapsed
 */
export function memberIndex(clusters, collapsed) {
  const out = new Map();
  for (const c of clusters || []) {
    if (!c || !collapsed?.has(c.id)) continue;
    for (const id of c.node_ids || []) {
      if (!out.has(key(id))) out.set(key(id), PROXY_PREFIX + c.id);
    }
  }
  return out;
}

/**
 * proxyCards builds one card per collapsed cluster, positioned at the centre of the cards it
 * replaces so the graph does not visibly jump when a group closes.
 *
 * `size` is the cluster's OWN count, not the number of members currently on the canvas — a
 * collapsed card that under-reported what it contains would be hiding exactly the thing it is
 * supposed to disclose.
 */
export function proxyCards(clusters, collapsed, nodes, geom) {
  const out = [];
  for (const c of clusters || []) {
    if (!c || !collapsed?.has(c.id)) continue;
    const centres = [];
    for (const id of c.node_ids || []) {
      const n = nodes?.[key(id)];
      if (n && Number.isFinite(n.x) && Number.isFinite(n.y)) {
        centres.push({ x: n.x + geom.width / 2, y: n.y + geom.height / 2 });
      }
    }
    if (centres.length === 0) continue;
    const c0 = centroid(centres);
    out.push({
      id: PROXY_PREFIX + c.id,
      clusterId: c.id,
      label: c.label,
      size: c.size ?? (c.node_ids || []).length,
      onCanvas: centres.length,
      x: c0.x - geom.width / 2,
      y: c0.y - geom.height / 2,
    });
  }
  return out;
}

/**
 * remapEdges re-points every edge at the collapsed cluster standing in for its endpoint.
 *
 * Edges whose two endpoints land in the SAME collapsed cluster are internal and are not drawn —
 * they are inside the card. Everything else is kept and re-pointed, so a collapsed group shows
 * exactly the connections it has to the rest of the graph. The relation id is preserved, so
 * selecting a re-pointed edge still inspects the real relation.
 *
 * @param {{id:number,from:number,to:number}[]} edges
 * @param {Map<string,string>} index
 */
export function remapEdges(edges, index) {
  if (!index || index.size === 0) return { edges: edges || [], internal: 0 };
  const out = [];
  let internal = 0;
  for (const e of edges || []) {
    const from = index.get(key(e.from)) ?? e.from;
    const to = index.get(key(e.to)) ?? e.to;
    // Internal means BOTH endpoints landed in the same collapsed cluster — hence the proxy
    // check. Testing `from === to` alone would also swallow a self-loop between two real nodes
    // and report it as something the collapse hid, which it is not.
    if (from === to && isProxy(from)) {
      internal += 1;
      continue;
    }
    out.push(from === e.from && to === e.to ? e : { ...e, from, to });
  }
  return { edges: out, internal };
}

/**
 * toggle returns a new collapsed set with one cluster flipped. A new Set rather than a mutation,
 * because the canvas re-renders off identity.
 * @param {Set<string>} collapsed
 */
export function toggle(collapsed, id) {
  const next = new Set(collapsed || []);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}
