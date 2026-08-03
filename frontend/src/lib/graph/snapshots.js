/**
 * Reading a restore plan (P29).
 *
 * The plan is computed in Go. What lives here is how it becomes a sentence, and which parts of it
 * an analyst is being asked to agree to.
 *
 * The one thing this module refuses to do is round off. A restore is the point at which a saved
 * picture meets a case that has changed underneath it, and the interesting answer is almost never
 * "all of it fits". Every item the backend classified is surfaced, and the two that change what
 * the graph CLAIMS — a re-created edge, and a node that resolved to a different id — are called
 * out rather than folded into a total.
 */

/** Outcomes, mirroring backend consts.Restore*. */
export const OUTCOME = Object.freeze({
  APPLIED: 'applied',
  RECREATABLE: 'recreatable',
  UNRESOLVED: 'unresolved',
  MOVED: 'moved',
});

/**
 * summarise turns a plan into the lines shown above the confirm button.
 *
 * `blocking` marks the facts that should stop an analyst clicking through without reading:
 * a re-ingest, and anything that cannot be resolved. They are not errors — a restore onto a
 * re-ingested case is exactly what hash-keying makes possible — but they change what the result
 * means, so they read differently from a count.
 * @param {{nodes_applied?:number,nodes_moved?:number,nodes_unresolved?:number,
 *          relations_applied?:number,relations_recreatable?:number,relations_unresolved?:number,
 *          reingested?:boolean}|null} plan
 */
export function summarise(plan) {
  if (!plan) return { rows: [], notes: [], offered: 0, empty: true };

  const rows = [
    { kind: 'nodes_applied', count: plan.nodes_applied ?? 0 },
    { kind: 'nodes_moved', count: plan.nodes_moved ?? 0 },
    { kind: 'nodes_unresolved', count: plan.nodes_unresolved ?? 0 },
    { kind: 'relations_applied', count: plan.relations_applied ?? 0 },
    { kind: 'relations_recreatable', count: plan.relations_recreatable ?? 0 },
    { kind: 'relations_unresolved', count: plan.relations_unresolved ?? 0 },
  ].filter((r) => r.count > 0);

  const notes = [];
  if (plan.reingested) notes.push({ kind: 'reingested' });
  if ((plan.nodes_unresolved ?? 0) > 0 || (plan.relations_unresolved ?? 0) > 0) {
    notes.push({ kind: 'unresolved' });
  }

  return {
    rows,
    notes,
    offered: plan.relations_recreatable ?? 0,
    empty: rows.length === 0,
  };
}

/**
 * recreatable returns the relations the analyst may choose to re-assert, in the backend's order.
 *
 * These are deliberately NOT pre-selected. Re-creating one makes rohy assert a link today, which
 * is a different claim from a rule having inferred it then — so it is an act the analyst performs,
 * not a default they have to notice and undo.
 * @param {{relations?:{outcome:string}[]}|null} plan
 */
export function recreatable(plan) {
  return (plan?.relations ?? []).filter((r) => r && r.outcome === OUTCOME.RECREATABLE);
}

/**
 * toggle adds or removes one snapshot relation id from the chosen set, returning a new Set so the
 * component re-renders on identity.
 * @param {Set<number>} chosen
 */
export function toggle(chosen, id) {
  const next = new Set(chosen || []);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}

/**
 * unresolvedNodes lists the nodes that could not be found, so the preview can name what is gone
 * rather than only counting it. Their descriptors are what make an orphan meaningful — a bare
 * hash tells the reader nothing.
 * @param {{nodes?:{outcome:string}[]}|null} plan
 */
export function unresolvedNodes(plan) {
  return (plan?.nodes ?? []).filter((n) => n && n.outcome === OUTCOME.UNRESOLVED);
}

/** movedNodes lists nodes whose hash resolved to a different id — the re-ingest evidence. */
export function movedNodes(plan) {
  return (plan?.nodes ?? []).filter((n) => n && n.outcome === OUTCOME.MOVED);
}

/** when renders a snapshot's timestamp, or '' when it cannot be read. */
export function when(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString().replace('T', ' ').slice(0, 19);
}

/**
 * title is what a snapshot is called in the list: the analyst's label if they gave one, otherwise
 * when it was taken. Never a bare id — the id is a filename, not a name.
 * @param {{label?:string, taken_at?:string, id?:string}} snap
 */
export function title(snap) {
  if (!snap) return '';
  const label = (snap.label || '').trim();
  if (label) return label;
  return when(snap.taken_at) || snap.id || '';
}
