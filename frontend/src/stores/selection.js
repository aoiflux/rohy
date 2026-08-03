// Selection/detail store (P5.2). Tracks the event opened in the detail view and the
// queue of events the user has marked "Add to Graph" (consumed by the canvas in P7).
import { writable } from 'svelte/store';

function create() {
  const { subscribe, update } = writable({
    active: /** @type {any} */ (null),
    graphQueue: /** @type {number[]} */ ([]),
    /**
     * The inspected relation, as the backend resolved it — endpoints, graph, provenance and the
     * rest of its occurrence.
     *
     * The WHOLE detail is held rather than a summary, because the panel needs all of it and a
     * store keeping a reduced copy would mean either a second round trip to draw the panel or
     * two representations of one thing drifting apart.
     */
    relation: /** @type {any} */ (null),
  });

  return {
    subscribe,
    open: (event) => update((s) => ({ ...s, active: event })),
    close: () => update((s) => ({ ...s, active: null })),
    queueForGraph: (id) =>
      update((s) => (s.graphQueue.includes(id) ? s : { ...s, graphQueue: [...s.graphQueue, id] })),
    dequeue: (id) => update((s) => ({ ...s, graphQueue: s.graphQueue.filter((x) => x !== id) })),
    clearQueue: () => update((s) => ({ ...s, graphQueue: [] })),

    /** selectRelation records an inspected edge. Pass null to clear. */
    selectRelation: (detail) => update((s) => ({ ...s, relation: detail ?? null })),
    clearRelation: () => update((s) => ({ ...s, relation: null })),
  };
}

/**
 * highlightedEdges returns the edge ids a selection should light up: the selected one and the
 * rest of its occurrence.
 *
 * Lighting up the whole match is the point. "What else is part of this chain?" is the question
 * an analyst has when looking at one link of it, and before relations carried a match id there
 * was no way to answer it.
 *
 * A plain function rather than a derived store, so the canvas can call it while rendering and
 * the tests can assert it without mounting anything.
 *
 * @param {any} detail a RelationDetail, or null
 * @returns {Set<number>}
 */
export function highlightedEdges(detail) {
  const ids = [detail?.relation?.id, ...(detail?.sibling_ids ?? [])];
  return new Set(ids.filter((id) => Number.isInteger(id) && id > 0));
}

export const selection = create();
