// Annotation state (P29).
//
// Unlike the cluster and replay stores, this one holds DATA rather than a reading posture: the
// analyst's own marks on a graph, mirrored from the sidecar. Every mutation goes to the backend
// first and the store adopts what comes back, so the canvas can never show a mark that was
// refused — an annotation that appeared and then vanished on reload would be worse than one that
// never appeared.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';

const empty = () => ({
  /** @type {{id:string,name:string,colour?:string,visible:boolean,z:number}[]} */
  layers: [],
  /** @type {any[]} */
  items: [],
  /** hash -> live node id, resolved by the backend on every read. */
  nodeOf: /** @type {Record<string, number>} */ ({}),
  /** Anchors whose event has left the case. Reported, never silently dropped. */
  orphaned: /** @type {string[]} */ ([]),
  /** Whether the canvas draws the overlay at all. */
  visible: true,
  loading: false,
  error: '',
});

function create() {
  const { subscribe, update, set } = writable(empty());

  /**
   * load re-reads a graph's annotations. It is called after every mutation as well as on entry:
   * the anchor resolution is done by the backend against the case as it is NOW, so a local patch
   * of the returned document would slowly drift from what the anchors actually point at.
   */
  async function load(graphId) {
    update((s) => ({ ...s, loading: true, error: '' }));
    try {
      const view = await api.annotations(graphId);
      update((s) => ({
        ...s,
        layers: view?.document?.layers ?? [],
        items: view?.document?.items ?? [],
        nodeOf: view?.node_of ?? {},
        orphaned: view?.orphaned ?? [],
        loading: false,
      }));
      return true;
    } catch (e) {
      update((s) => ({
        ...s,
        layers: [],
        items: [],
        nodeOf: {},
        orphaned: [],
        loading: false,
        error: String(e && e.message ? e.message : e),
      }));
      return false;
    }
  }

  /** run performs a mutation and reloads, surfacing the error rather than swallowing it. */
  async function run(graphId, fn) {
    try {
      const out = await fn();
      await load(graphId);
      return out;
    } catch (e) {
      update((s) => ({ ...s, error: String(e && e.message ? e.message : e) }));
      throw e;
    }
  }

  return {
    subscribe,
    load,
    current: () => get({ subscribe }),

    saveLayer: (graphId, req) => run(graphId, () => api.saveLayer({ ...req, graph_id: graphId })),
    deleteLayer: (graphId, layerId) => run(graphId, () => api.deleteLayer(graphId, layerId)),
    save: (graphId, item) => run(graphId, () => api.saveAnnotation({ graph_id: graphId, item })),
    remove: (graphId, id) => run(graphId, () => api.deleteAnnotation(graphId, id)),

    /** show/hide toggles whether the overlay draws. A view choice, not a change to the data. */
    show: () => update((s) => ({ ...s, visible: true })),
    hide: () => update((s) => ({ ...s, visible: false })),
    toggleVisible: () => update((s) => ({ ...s, visible: !s.visible })),

    reset: () => set(empty()),
  };
}

export const annotations = create();
