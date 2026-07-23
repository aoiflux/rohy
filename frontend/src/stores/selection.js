// Selection/detail store (P5.2). Tracks the event opened in the detail view and the
// queue of events the user has marked "Add to Graph" (consumed by the canvas in P7).
import { writable } from 'svelte/store';

function create() {
  const { subscribe, update } = writable({
    active: /** @type {any} */ (null),
    graphQueue: /** @type {number[]} */ ([]),
  });

  return {
    subscribe,
    open: (event) => update((s) => ({ ...s, active: event })),
    close: () => update((s) => ({ ...s, active: null })),
    queueForGraph: (id) =>
      update((s) => (s.graphQueue.includes(id) ? s : { ...s, graphQueue: [...s.graphQueue, id] })),
    dequeue: (id) => update((s) => ({ ...s, graphQueue: s.graphQueue.filter((x) => x !== id) })),
    clearQueue: () => update((s) => ({ ...s, graphQueue: [] })),
  };
}

export const selection = create();
