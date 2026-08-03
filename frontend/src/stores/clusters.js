// Cluster view state (P29).
//
// Grouping is computed in Go; what this store holds is how the canvas is currently LOOKING at
// that grouping — which mode, which clusters are collapsed, whether outlines are drawn at all.
//
// It is view state and nothing more. Nothing here is persisted: which groups an analyst has
// folded up while reading is not a property of the case, and writing it beside the evidence
// would make a reading posture look like a finding.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { toggle as toggleIn } from '../lib/graph/clusters.js';

const empty = () => ({
  /** consts.ClusterModes value; '' until the backend has been asked. */
  mode: '',
  /** Correlation field for slot mode. */
  slot: '',
  /** @type {{id:string,label:string,node_ids:number[],size:number,overlapping?:boolean}[]} */
  list: [],
  /** @type {Set<string>} ids of collapsed clusters. */
  collapsed: new Set(),
  /** Outlines are off until asked for: a permanent set of hulls is visual noise on a graph. */
  visible: false,
  loading: false,
  error: '',
});

function create() {
  const { subscribe, update, set } = writable(empty());

  /**
   * load fetches the grouping for one graph.
   *
   * Collapsed ids are RESET on every load, deliberately. A cluster id is derived from its
   * membership, so re-grouping in another mode produces different ids — carrying the old set
   * across would leave clusters folded that no longer exist, and the analyst with no way to
   * unfold them.
   */
  async function load(graphId, mode, slot) {
    update((s) => ({ ...s, loading: true, error: '' }));
    try {
      const list = await api.clusters({ graph_id: graphId, mode, slot: slot || '' });
      update((s) => ({
        ...s,
        mode,
        slot: slot || '',
        list: list || [],
        collapsed: new Set(),
        visible: true,
        loading: false,
      }));
      return list || [];
    } catch (e) {
      update((s) => ({
        ...s,
        list: [],
        collapsed: new Set(),
        loading: false,
        error: String(e && e.message ? e.message : e),
      }));
      return [];
    }
  }

  return {
    subscribe,
    load,
    /** toggle folds or unfolds one cluster. */
    toggle: (id) => update((s) => ({ ...s, collapsed: toggleIn(s.collapsed, id) })),
    /** expandAll unfolds everything without discarding the grouping. */
    expandAll: () => update((s) => ({ ...s, collapsed: new Set() })),
    /** collapseAll folds every cluster in the current grouping. */
    collapseAll: () =>
      update((s) => ({ ...s, collapsed: new Set(s.list.map((c) => c.id)) })),
    /**
     * hide stops drawing outlines AND unfolds everything. Leaving groups collapsed with their
     * outlines hidden would leave cards on the canvas standing for events, with nothing on
     * screen explaining what they stand for.
     */
    hide: () => update((s) => ({ ...s, visible: false, collapsed: new Set() })),
    show: () => update((s) => ({ ...s, visible: true })),
    /** current reads the state synchronously, for callers outside a component. */
    current: () => get({ subscribe }),
    reset: () => set(empty()),
  };
}

export const clusters = create();
