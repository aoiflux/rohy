// Graph store (P5.2). Owns canvas state: nodes (event + world position), edges,
// viewport (pan/zoom), selection, and layout mode. The rendering/interaction logic is
// P7; persistence via the graph API is P8. This store is the single source of truth
// the canvas binds to, preventing canvas/DB divergence.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';

export const LAYOUT = Object.freeze({
  MANUAL: 'manual',
  AUTO: 'auto',
});

// How long a newly created node/edge stays flagged as "fresh" so its entrance animation
// can play once. Comfortably longer than the animation itself.
const FRESH_MS = 700;

function initial() {
  return {
    nodes: /** @type {Record<number, {event:any,x:number,y:number}>} */ ({}),
    edges: /** @type {any[]} */ ([]),
    viewport: { x: 0, y: 0, zoom: 1 },
    selection: /** @type {number[]} */ ([]),
    layout: LAYOUT.MANUAL,
    // focus is a one-shot request to centre the canvas on an event node (P14.4). The
    // nonce lets the canvas react to repeated focus requests for the same id.
    focus: /** @type {{id:number|null, nonce:number}} */ ({ id: null, nonce: 0 }),
    // Multiple graphs (P15): the registry list and the active graph id. Nodes/edges/
    // layout above always belong to the active graph.
    graphs: /** @type {any[]} */ ([]),
    activeGraphId: 0,
    // Ids of items created just now, so the canvas animates an entrance for THOSE only —
    // not for the constant mount/unmount churn of virtualization (P10 / R-AN1).
    fresh: { nodes: /** @type {Set<number>} */ (new Set()), edges: /** @type {Set<number>} */ (new Set()) },
  };
}

function create() {
  const store = writable(initial());
  const { subscribe, update, set } = store;

  // Nodes and edges that have just been created, so the canvas can animate ONLY those.
  // This matters because the canvas is virtualized: nodes mount and unmount constantly as
  // you pan, and animating every mount would make panning shimmer (R-AN1). Membership is
  // cleared shortly after, so the animation plays once for a genuinely new item.
  function markFresh(key, id) {
    update((s) => ({ ...s, fresh: { ...s.fresh, [key]: new Set([...s.fresh[key], id]) } }));
    if (typeof setTimeout === 'undefined') return;
    setTimeout(() => {
      update((s) => {
        const next = new Set(s.fresh[key]);
        next.delete(id);
        return { ...s, fresh: { ...s.fresh, [key]: next } };
      });
    }, FRESH_MS);
  }

  // addNode places an event on the canvas. `animate` is false for bulk restores: loading a
  // saved mapping is not "creating" nodes, and animating hundreds of them at once is the
  // per-node animation cost the canvas is supposed to avoid (R-AN1).
  function addNode(event, x = 0, y = 0, { animate = true } = {}) {
    update((s) => ({ ...s, nodes: { ...s.nodes, [event.id]: { event, x, y } } }));
    if (animate) markFresh('nodes', event.id);
  }
  function removeNode(id) {
    update((s) => {
      const nodes = { ...s.nodes };
      delete nodes[id];
      return { ...s, nodes, edges: s.edges.filter((e) => e.from !== id && e.to !== id) };
    });
  }
  function moveNode(id, x, y) {
    update((s) => (s.nodes[id] ? { ...s, nodes: { ...s.nodes, [id]: { ...s.nodes[id], x, y } } } : s));
  }
  function setViewport(v) {
    update((s) => ({ ...s, viewport: { ...s.viewport, ...v } }));
  }
  function setSelection(ids) {
    update((s) => ({ ...s, selection: ids }));
  }
  function setLayout(mode) {
    update((s) => ({ ...s, layout: mode === LAYOUT.AUTO ? LAYOUT.AUTO : LAYOUT.MANUAL }));
  }
  // focusEvent requests the canvas centre on an event's node and selects it. The canvas
  // performs the actual viewport move once it knows its own dimensions (P14.4).
  function focusEvent(id) {
    update((s) => ({ ...s, selection: [id], focus: { id, nonce: s.focus.nonce + 1 } }));
  }

  // clearCanvas empties the canvas (nodes/edges/selection/viewport) but keeps the graph
  // registry + active graph — used on graph switch and by the "Clear canvas" action.
  function clearCanvas() {
    update((s) => ({
      ...s,
      nodes: {},
      edges: [],
      selection: [],
      viewport: { x: 0, y: 0, zoom: 1 },
      focus: { id: null, nonce: 0 },
      // Drop pending entrance flags too, so a node re-added right after a graph switch
      // does not inherit a stale animation.
      fresh: { nodes: new Set(), edges: new Set() },
    }));
  }

  // --- Multiple graphs (P15) ---

  // loadGraphs refreshes the registry list + active id from the backend.
  async function loadGraphs() {
    try {
      const [graphs, activeGraphId] = await Promise.all([api.listGraphs(), api.activeGraph()]);
      update((s) => ({ ...s, graphs: graphs || [], activeGraphId: activeGraphId || 0 }));
      return graphs;
    } catch (_) {
      return null;
    }
  }

  // setActive switches the active graph on the backend and clears the canvas so the
  // caller can load the newly active graph's mapping. No-op if already active.
  async function setActive(id) {
    if (get(store).activeGraphId === id) return;
    await api.setActiveGraph(id);
    clearCanvas();
    update((s) => ({ ...s, activeGraphId: id }));
  }

  // createGraph adds a graph (which becomes active on the backend), refreshes the list,
  // and clears the canvas for the fresh graph. Returns the created graph.
  async function createGraph(name, description) {
    const g = await api.createGraph({ name, description: description || '' });
    clearCanvas();
    update((s) => ({ ...s, graphs: [...s.graphs, g], activeGraphId: g.id }));
    return g;
  }

  async function renameGraph(id, name, description) {
    const g = await api.renameGraph({ id, name, description: description || '' });
    update((s) => ({ ...s, graphs: s.graphs.map((x) => (x.id === id ? g : x)) }));
    return g;
  }

  // deleteGraph removes a graph (backend cascades its edges + layout, never events),
  // then refreshes the list + active id. Clears the canvas if the active graph changed.
  async function deleteGraph(id) {
    await api.deleteGraph(id);
    const wasActive = get(store).activeGraphId === id;
    await loadGraphs();
    if (wasActive) clearCanvas();
  }

  // loadRelations pulls the active graph's persisted edges.
  async function loadRelations() {
    try {
      const edges = await api.getGraphRelations(get(store).activeGraphId);
      update((s) => ({ ...s, edges: edges || [] }));
      return edges;
    } catch (_) {
      return null;
    }
  }

  // connect persists a manual directed edge (with an optional free-text label) in the
  // active graph and reflects it locally (P8 flow entry point).
  async function connect(from, to, relationType, label) {
    const rel = await api.createRelation({
      from,
      to,
      graph_id: get(store).activeGraphId,
      relation_type: relationType,
      relation_label: label || '',
    });
    update((s) => ({ ...s, edges: [...s.edges, rel] }));
    markFresh('edges', rel.id); // draws itself in, confirming the link landed
    return rel;
  }

  // updateRelation edits a persisted edge's type/label and reflects it locally.
  async function updateRelation(id, relationType, label) {
    const rel = await api.updateRelation({ id, relation_type: relationType, relation_label: label || '' });
    update((s) => ({ ...s, edges: s.edges.map((e) => (e.id === id ? rel : e)) }));
    return rel;
  }

  // deleteRelation removes a persisted edge from the DB and the canvas.
  async function deleteRelation(id) {
    await api.deleteRelation(id);
    update((s) => ({ ...s, edges: s.edges.filter((e) => e.id !== id) }));
  }

  // deleteEvent permanently removes an event node (and its edges) from the DB, then
  // drops it from the canvas.
  async function deleteEvent(id) {
    await api.deleteEvent(id);
    update((s) => {
      const nodes = { ...s.nodes };
      delete nodes[id];
      return { ...s, nodes, edges: s.edges.filter((e) => e.from !== id && e.to !== id) };
    });
  }

  // saveLayout persists the active graph's node positions + viewport. The node id set is
  // the graph's canvas membership, so placed-but-unlinked nodes survive reload (P15).
  async function saveLayout() {
    const s = get(store);
    const nodes = {};
    for (const [id, n] of Object.entries(s.nodes)) {
      nodes[id] = { x: n.x, y: n.y };
    }
    await api.saveLayout(s.activeGraphId, { nodes, viewport: s.viewport });
  }

  // loadLayout returns the active graph's persisted layout (or null on failure).
  async function loadLayout() {
    try {
      return await api.loadLayout(get(store).activeGraphId);
    } catch (_) {
      return null;
    }
  }

  // applyLayout moves already-present nodes to their saved positions and restores the
  // viewport. Nodes not on the canvas are ignored (their events must be added first).
  function applyLayout(saved) {
    if (!saved) return;
    update((s) => {
      const nodes = { ...s.nodes };
      for (const [id, pos] of Object.entries(saved.nodes || {})) {
        if (nodes[id]) nodes[id] = { ...nodes[id], x: pos.x, y: pos.y };
      }
      const viewport = saved.viewport && saved.viewport.zoom ? saved.viewport : s.viewport;
      return { ...s, nodes, viewport };
    });
  }

  return {
    subscribe,
    addNode,
    removeNode,
    moveNode,
    setViewport,
    setSelection,
    setLayout,
    focusEvent,
    clearCanvas,
    loadGraphs,
    setActive,
    createGraph,
    renameGraph,
    deleteGraph,
    loadRelations,
    connect,
    updateRelation,
    deleteRelation,
    deleteEvent,
    saveLayout,
    loadLayout,
    applyLayout,
    reset: () => set(initial()),
  };
}

export const graph = create();
