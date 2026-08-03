// Stepping through a selected node's relations from the keyboard.
//
// This exists to close a gap the clickable-edge work opened: the mouse gained a route to
// selecting a relation and the keyboard had none. Giving every edge a tab stop would have been
// worse than the problem — a graph holds hundreds — so the canvas keeps its single focusable
// `role="application"` region and offers stepping instead, which is the pattern the rest of its
// bare-letter bindings already follow.
//
// The logic is here rather than in the component because a cycle with an off-by-one, or one
// whose order changes between presses, is exactly the kind of thing that is invisible in review
// and obvious in use.

/**
 * relationsOfSelection returns the edges incident to any selected node, in a STABLE order.
 *
 * Stability is the whole contract. The same selection must produce the same cycle every time,
 * or pressing the key twice would not move predictably — and a keyboard user has no other way
 * to tell which edge they are on. Edge id is the ordering because it is assignment order, which
 * is the closest thing to "the order these were created".
 *
 * @param {Array<{id:number, from:number, to:number}>} edges
 * @param {number[]} selectedIds
 * @returns {number[]} edge ids
 */
export function relationsOfSelection(edges, selectedIds) {
  const selected = new Set(selectedIds || []);
  if (selected.size === 0) return [];
  return (edges || [])
    .filter((e) => e && (selected.has(e.from) || selected.has(e.to)))
    .map((e) => e.id)
    .filter((id) => Number.isInteger(id) && id > 0)
    .sort((a, b) => a - b);
}

/**
 * stepRelation returns the next edge id in a cycle, wrapping at both ends.
 *
 * Wrapping rather than stopping: a cycle a key walks off the end of leaves the user pressing a
 * key that does nothing, with no indication whether they have reached the end or the binding
 * has stopped working.
 *
 * A current id that is not in the cycle — the selection changed since the edge was picked —
 * starts from the beginning rather than returning nothing, so the key always does something.
 *
 * @param {number[]} cycle
 * @param {number|null} currentId
 * @param {number} step +1 forward, -1 back
 * @returns {number|null} null only when there is nothing to step to
 */
export function stepRelation(cycle, currentId, step = 1) {
  if (!cycle || cycle.length === 0) return null;
  const at = cycle.indexOf(currentId);
  if (at === -1) return step >= 0 ? cycle[0] : cycle[cycle.length - 1];
  const next = (at + step + cycle.length) % cycle.length;
  return cycle[next];
}
