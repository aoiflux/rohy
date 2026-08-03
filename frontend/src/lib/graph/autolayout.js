/**
 * Auto-layout preview state (P29).
 *
 * The arrangement itself is computed in Go — deterministic, unit-tested, and reusable without a
 * browser. What lives here is the part that is genuinely a UI concern: previewing an arrangement
 * without committing to it.
 *
 * That distinction is the whole reason this module exists. Node positions are the one thing on
 * the canvas an analyst places by hand, and there is no undo for them. A layout that applied
 * itself the moment a profile was picked would destroy that work before it had been looked at.
 * So: preview moves the nodes, a snapshot remembers where they were, and nothing is written
 * until the arrangement is kept.
 */

/**
 * snapshot captures the current positions of every node on the canvas, so a preview can be
 * undone exactly. It copies rather than aliasing — a snapshot that shared objects with the live
 * nodes would be rewritten by the very move it exists to reverse.
 * @param {Record<string|number, {x:number,y:number}>} nodes
 * @returns {Record<string, {x:number,y:number}>}
 */
export function snapshot(nodes) {
  const out = {};
  for (const [id, n] of Object.entries(nodes || {})) {
    if (n && Number.isFinite(n.x) && Number.isFinite(n.y)) out[id] = { x: n.x, y: n.y };
  }
  return out;
}

/**
 * toSaved converts a computed layout into the shape `graph.applyLayout` already understands, so
 * a computed arrangement and a restored one travel the same code path. Reusing it rather than
 * writing a second mover is what keeps "the canvas moved" one behaviour instead of two.
 *
 * Positions for nodes that are not on the canvas are dropped here rather than in the store: a
 * layout computed against a canvas that has since changed should move what it still can, not
 * refuse the lot.
 * @param {{positions?:Record<string|number,{x:number,y:number}>}|null} result
 * @param {Record<string|number, unknown>} [nodes] current canvas nodes; omit to keep everything
 */
export function toSaved(result, nodes) {
  const positions = (result && result.positions) || {};
  const out = {};
  for (const [id, p] of Object.entries(positions)) {
    if (!p || !Number.isFinite(p.x) || !Number.isFinite(p.y)) continue;
    if (nodes && !(id in nodes)) continue;
    out[id] = { x: p.x, y: p.y };
  }
  // No viewport: an auto-layout arranges nodes and says nothing about where the analyst is
  // looking. Resetting the pan/zoom as a side effect of a layout would lose their place.
  return { nodes: out };
}

/**
 * placed counts how many of a computed layout's positions actually landed on canvas nodes. A
 * count short of the total means the canvas changed under the layout, which is worth saying
 * rather than leaving the analyst to notice a node that did not move.
 * @param {{positions?:Record<string|number,{x:number,y:number}>}|null} result
 * @param {Record<string|number, unknown>} nodes
 */
export function placed(result, nodes) {
  const positions = (result && result.positions) || {};
  const ids = Object.keys(positions);
  let hit = 0;
  for (const id of ids) if (nodes && id in nodes) hit += 1;
  return { total: ids.length, applied: hit };
}

/**
 * describe turns a layout result into the one or two lines shown under the picker.
 *
 * It never invents reassurance. The backend's `note` is a caveat the profile chose to raise —
 * a broken cycle, an empty correlation projection, events with no timestamp — and it is passed
 * through verbatim, because a summary that smoothed it into "12 groups" would drop precisely
 * the part the analyst needs before reading the picture as evidence.
 * @param {{groups?:{label:string,node_ids:number[],undated?:boolean}[], note?:string}|null} result
 */
export function describe(result) {
  if (!result) return { summary: '', note: '', warn: false };
  const groups = result.groups || [];
  const nodes = groups.reduce((n, g) => n + ((g.node_ids || []).length), 0);
  const summary = groups.length
    ? `${nodes} node${nodes === 1 ? '' : 's'} in ${groups.length} group${groups.length === 1 ? '' : 's'}`
    : '';
  return { summary, note: result.note || '', warn: Boolean(result.note) };
}

/**
 * needsField reports whether a profile groups by a correlation field, so the picker offers the
 * field list for that profile only. Derived from the backend's own descriptor rather than from a
 * name comparison here, so adding a profile does not mean editing this file too.
 * @param {{name:string,needs_slot?:boolean}[]} profiles
 * @param {string} name
 */
export function needsField(profiles, name) {
  const p = (profiles || []).find((x) => x && x.name === name);
  return Boolean(p && p.needs_slot);
}
