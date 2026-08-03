/**
 * Reading a relationship heatmap (P29).
 *
 * The matrix is computed in Go. What lives here is how a cell becomes a colour, and — more
 * importantly — how the summary's counts become a sentence the analyst can trust.
 *
 * A heatmap is unusually easy to lie with. Colour has no axis, so a cell that is dark because
 * the ramp is wrong looks exactly like a cell that is dark because a lot happened there. Two
 * rules keep it honest:
 *
 *   1. **The ramp is linear and anchored at the busiest cell the backend reported**, not at a
 *      per-lane maximum. A per-lane ramp makes every lane's busiest moment look equally busy,
 *      which turns a quiet rule and a screaming one into the same picture.
 *   2. **Zero is not a shade.** An empty cell is empty — not the palest colour on the ramp —
 *      because "nothing happened here" and "almost nothing happened here" are different
 *      findings and must not be a few percent of lightness apart.
 */

/**
 * intensity maps a cell count onto 0..1 against the matrix maximum.
 *
 * Returns exactly 0 for an empty cell, so the renderer can draw nothing rather than the palest
 * step of the ramp.
 */
export function intensity(count, max) {
  if (!Number.isFinite(count) || count <= 0) return 0;
  if (!Number.isFinite(max) || max <= 0) return 0;
  return Math.min(count / max, 1);
}

/**
 * describe turns a summary into the caveats shown beside the matrix.
 *
 * Every relation the backend considered is accounted for: placed, undated, or outside the
 * window. Anything unaccounted for would be a silent subtraction, which is the failure mode a
 * count-based view has to be built against.
 * @param {{total?:number,placed?:number,undated?:number,outside?:number,lanes?:unknown[]}|null} summary
 */
export function describe(summary) {
  if (!summary) return { empty: true, notes: [] };
  const total = summary.total ?? 0;
  const placed = summary.placed ?? 0;
  const undated = summary.undated ?? 0;
  const outside = summary.outside ?? 0;

  const notes = [];
  if (undated > 0) notes.push({ kind: 'undated', count: undated });
  if (outside > 0) notes.push({ kind: 'outside', count: outside });
  // A discrepancy means the backend and this reader disagree about what happened to a relation.
  // Better surfaced as an explicit unknown than silently absorbed into "placed".
  const unaccounted = total - placed - undated - outside;
  if (unaccounted > 0) notes.push({ kind: 'unaccounted', count: unaccounted });

  return { empty: placed === 0, total, placed, undated, outside, notes };
}

/**
 * lanesFor returns the rows to draw, capped for legibility. The backend already folds the
 * smallest lanes into an "(other)" row, so this is a second, softer bound for a strip that has
 * far less vertical room than the standalone matrix.
 *
 * It truncates from the BOTTOM of an already volume-sorted list, and reports how many it
 * dropped — a matrix silently showing eight of twenty rules reads as a case with eight rules.
 * @param {{key:string,total:number,counts:number[]}[]} lanes
 */
export function lanesFor(lanes, limit) {
  const all = lanes || [];
  if (!limit || all.length <= limit) return { lanes: all, hidden: 0 };
  return { lanes: all.slice(0, limit), hidden: all.length - limit };
}

/**
 * cellTitle is the hover text for one cell: the count, the lane, and the window it covers.
 * Colour cannot express a number, so the number has to be reachable.
 * @param {{key:string}} lane
 * @param {{start:string,end:string}} bucket
 */
export function cellTitle(lane, bucket, count) {
  const when = bucket ? `${short(bucket.start)} – ${short(bucket.end)}` : '';
  return `${lane?.key ?? ''}: ${count} · ${when}`.trim();
}

/** short renders an ISO instant as "MM-DD HH:MM:SS", or '' when it cannot be read. */
export function short(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString().replace('T', ' ').slice(5, 19);
}

/**
 * totals sums each bucket across every lane, for the single-row strip. It is derived from the
 * lanes rather than read from `buckets[i].count` on purpose: the strip and the matrix must show
 * the same numbers, and deriving both from one source is what guarantees it.
 * @param {{counts:number[]}[]} lanes
 */
export function totals(lanes, bucketCount) {
  const out = new Array(Math.max(bucketCount || 0, 0)).fill(0);
  for (const l of lanes || []) {
    const counts = l.counts || [];
    for (let i = 0; i < out.length && i < counts.length; i += 1) out[i] += counts[i] || 0;
  }
  return out;
}

/**
 * sliceToView cuts a per-bucket array down to the timeline's current zoom window.
 *
 * 🔒 This is what keeps the strip aligned with the histogram under it. The heatmap is requested
 * over the timeline's FULL extent with the same bucket count, so index i means the same instant
 * in both; zoom is then applied identically to both arrays. Requesting the heatmap over the
 * visible window instead would re-bucket it, and the two would stop lining up the moment the
 * window's span was not an exact multiple of a bucket.
 *
 * @param {number[]} counts
 * @param {{start:number,end:number}|null} view fractions of the full extent
 */
export function sliceToView(counts, view) {
  const all = counts || [];
  if (!view || all.length === 0) return all;
  const start = clamp01(view.start);
  const end = clamp01(view.end);
  if (!(end > start)) return all;
  const a = Math.floor(start * all.length);
  const b = Math.ceil(end * all.length);
  // At least one column, so a window narrower than a bucket shows the bucket it is inside
  // rather than nothing at all.
  return all.slice(Math.min(a, all.length - 1), Math.max(b, a + 1));
}

function clamp01(v) {
  if (!Number.isFinite(v)) return 0;
  return Math.min(Math.max(v, 0), 1);
}
