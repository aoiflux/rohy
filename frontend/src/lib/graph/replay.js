/**
 * Replay: watching a graph assemble itself in the order the evidence says it happened (P29).
 *
 * 🔒 The whole feature turns on one decision. Replay is ordered by EVENT TIMESTAMPS, never by
 * `created_at`. `created_at` is when the rule build ran — a few milliseconds of one afternoon —
 * so a graph animated in that order would show the analyst the shape of their own tooling rather
 * than the shape of the incident. An edge appears when its LATER endpoint's timestamp is reached,
 * which is the moment the relationship became true. That is the same rule the heatmap follows.
 *
 * The second decision is what to do with evidence that has no time. Catalogue rows carry no
 * timestamp, and an event with no timestamp is not an event from the epoch — so they are NOT
 * animated into place at t=0 as though they were the first thing that happened. They are present
 * from the start, in a tray, labelled with why. Same rule the temporal layout follows.
 *
 * This module is pure: nodes and edges in, a schedule out. No timers, no store, no rendering —
 * so the ordering can be tested without a clock.
 */

/** A time far enough before everything that "present from the start" sorts first. */
const ALWAYS = -Infinity;

const ms = (v) => {
  if (!v) return null;
  const t = new Date(v).getTime();
  return Number.isFinite(t) ? t : null;
};

/**
 * build turns the canvas contents into a replay schedule.
 *
 * @param {Record<string, {event:{id:number,timestamp?:string}}>} nodes canvas nodes by id
 * @param {{id:number,from:number,to:number}[]} edges
 * @returns {{
 *   from:number|null, to:number|null,
 *   nodeAt:Map<string,number>, edgeAt:Map<string,number>,
 *   undatedNodes:number[], approximateEdges:number[],
 *   dated:number, total:number
 * }}
 */
export function build(nodes, edges) {
  const nodeAt = new Map();
  const undatedNodes = [];
  let from = null;
  let to = null;

  for (const [id, n] of Object.entries(nodes || {})) {
    const t = ms(n?.event?.timestamp);
    if (t === null) {
      // Present from the start, and reported. Animating it in at t=0 would assert it was the
      // first thing that happened, which is a claim the evidence does not make.
      nodeAt.set(id, ALWAYS);
      undatedNodes.push(n?.event?.id ?? Number(id));
      continue;
    }
    nodeAt.set(id, t);
    if (from === null || t < from) from = t;
    if (to === null || t > to) to = t;
  }

  const edgeAt = new Map();
  const approximateEdges = [];
  for (const e of edges || []) {
    if (!e) continue;
    const a = nodeAt.get(String(e.from));
    const b = nodeAt.get(String(e.to));
    if (a === undefined || b === undefined) continue; // an endpoint is not on this canvas

    const datedEnds = [a, b].filter((t) => t !== ALWAYS);
    if (datedEnds.length === 2) {
      edgeAt.set(String(e.id), Math.max(a, b));
      continue;
    }
    // One or both endpoints have no timestamp, so the relationship has no moment at which it
    // became true. It appears as soon as both its endpoints are on screen — which is the earliest
    // point at which drawing it is even coherent — and it is marked approximate so the canvas can
    // show it as a weaker claim rather than as a placed one.
    edgeAt.set(String(e.id), datedEnds.length === 1 ? datedEnds[0] : ALWAYS);
    approximateEdges.push(e.id);
  }

  return {
    from,
    to,
    nodeAt,
    edgeAt,
    undatedNodes,
    approximateEdges,
    dated: nodeAt.size - undatedNodes.length,
    total: nodeAt.size,
  };
}

/**
 * visibleAt returns what is on screen at an absolute instant.
 *
 * Membership is by id string, matching how the canvas keys its node map.
 * @param {ReturnType<typeof build>} model
 * @param {number|null} t absolute ms; null means "everything", i.e. replay is off
 */
export function visibleAt(model, t) {
  if (!model || t === null || t === undefined || !Number.isFinite(t)) return null;
  const nodes = new Set();
  const edges = new Set();
  for (const [id, at] of model.nodeAt) if (at <= t) nodes.add(id);
  for (const [id, at] of model.edgeAt) if (at <= t) edges.add(id);
  return { nodes, edges };
}

/**
 * fracToTime maps a 0..1 scrub position onto absolute time; timeToFrac is its inverse.
 * Both return null when the model has no extent — a graph of nothing but catalogue rows has no
 * axis to scrub along, and pretending it does would invent one.
 */
export function fracToTime(model, f) {
  if (!model || model.from === null || model.to === null) return null;
  const span = model.to - model.from;
  const c = clamp01(f);
  return span > 0 ? model.from + span * c : model.from;
}

export function timeToFrac(model, t) {
  if (!model || model.from === null || model.to === null || t === null) return null;
  const span = model.to - model.from;
  if (span <= 0) return 0;
  return clamp01((t - model.from) / span);
}

/**
 * advance steps the playhead forward by real elapsed time, scaled so the whole graph plays in
 * `durationMs` at 1×.
 *
 * Playback is scaled to the SPAN rather than run at wall-clock speed on purpose: a case covering
 * three weeks would otherwise take three weeks, and one covering four seconds would be over
 * before it was seen. The trade is that the perceived pace does not carry information — so the
 * timeline, where it does, stays the place to read pace from.
 *
 * @returns {{t:number, done:boolean}}
 */
export function advance(model, t, elapsedMs, speed, durationMs) {
  if (!model || model.from === null || model.to === null) return { t, done: true };
  const span = model.to - model.from;
  const dur = durationMs > 0 ? durationMs : 1;
  const step = (span * (elapsedMs * (speed > 0 ? speed : 1))) / dur;
  const next = (t === null ? model.from : t) + (span > 0 ? step : span + 1);
  if (next >= model.to) return { t: model.to, done: true };
  return { t: next, done: false };
}

/**
 * describe reports what replay cannot place, for the caveat beside the transport. Silence here
 * would leave nodes that never animate looking like a bug in the playback.
 */
export function describe(model) {
  if (!model) return { playable: false, notes: [] };
  const notes = [];
  if (model.undatedNodes.length > 0) {
    notes.push({ kind: 'undated', count: model.undatedNodes.length });
  }
  if (model.approximateEdges.length > 0) {
    notes.push({ kind: 'approximate', count: model.approximateEdges.length });
  }
  return { playable: model.from !== null && model.dated > 0, notes };
}

function clamp01(v) {
  if (!Number.isFinite(v)) return 0;
  return Math.min(Math.max(v, 0), 1);
}
