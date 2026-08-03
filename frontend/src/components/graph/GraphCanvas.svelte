<script>
  // The graph canvas (P7). Owns the interaction state machine — idle → pan / drag /
  // connect / context-menu — and renders a virtualized window of Material node cards
  // plus an SVG edge layer inside a single pan/zoom world transform. Connecting two
  // nodes opens a dialog to name the link (free-text label) and pick its type; the
  // edge is persisted via the backend (P7.4/P8), never local-only.
  import { graph, LAYOUT } from '../../stores/graph.js';
  import { selection as sel, highlightedEdges } from '../../stores/selection.js';
  import { relationsOfSelection, stepRelation } from '../../lib/graph/relations.js';
  import { clusters } from '../../stores/clusters.js';
  import { memberIndex, proxyCards, remapEdges } from '../../lib/graph/clusters.js';
  import { replay } from '../../stores/replay.js';
  import { build as buildReplay, visibleAt } from '../../lib/graph/replay.js';
  import * as api from '../../lib/api/index.js';
  import { findings } from '../../stores/findings.js';
  import { snackbar } from '../../stores/snackbar.js';
  import {
    screenToWorld,
    zoomAround,
    clampZoom,
    isNodeVisible,
    centerOn,
    fitToNodes,
    normalizeRect,
    nodesInRect,
  } from './coords.js';
  import { isOverlayTarget, OVERLAY_ATTR } from './interaction.js';
  import { GRAPH, RELATIONS, RELATION_LABEL, NODE_ACTION, UI } from '../../lib/consts/index.js';
  import GraphNode from './GraphNode.svelte';
  import GraphEdges from './GraphEdges.svelte';
  import GraphHulls from './GraphHulls.svelte';
  import ClusterCard from './ClusterCard.svelte';
  import Menu from '../material/Menu.svelte';
  import Dialog from '../material/Dialog.svelte';
  import Button from '../material/Button.svelte';
  import TextField from '../material/TextField.svelte';
  import Select from '../material/Select.svelte';

  const MODE = { IDLE: 'idle', PAN: 'pan', DRAG: 'drag', CONNECT: 'connect', MARQUEE: 'marquee' };

  let viewportEl;
  let w = $state(0);
  let h = $state(0);

  let mode = $state(MODE.IDLE);
  let pointerStart = { x: 0, y: 0 };
  let panStartVp = { x: 0, y: 0 };
  let dragStart = /** @type {Record<number,{x:number,y:number}>} */ ({});
  let connectFrom = $state(/** @type {number|null} */ (null));
  let connectTarget = $state(/** @type {number|null} */ (null));
  let cursorWorld = $state({ x: 0, y: 0 });
  // Connect mode (P18): while on, dragging from ANYWHERE on a node starts a link, so no
  // handle has to be hit at all. Node dragging is suspended for the duration.
  let connectMode = $state(false);
  // Marquee box-select, in SCREEN space (it is an overlay, not world content, so it must
  // not scale with zoom).
  let marquee = $state(/** @type {{a:{x:number,y:number}, b:{x:number,y:number}}|null} */ (null));
  // The selection the marquee started from, so dragging a box ADDS to what was already
  // selected rather than replacing it (shift conventionally means "add").
  let marqueeBase = /** @type {number[]} */ ([]);

  let ctxMenu = $state({ open: false, x: 0, y: 0, nodeId: /** @type {number|null} */ (null) });
  let link = $state({ open: false, from: /** @type {number|null} */ (null), to: /** @type {number|null} */ (null), label: '', type: RELATIONS.CORRELATION });
  let edit = $state({ open: false, id: /** @type {number|null} */ (null), label: '', type: RELATIONS.DEFAULT });
  let confirmDel = $state({ open: false, nodeId: /** @type {number|null} */ (null) });

  const vp = $derived($graph.viewport);
  const nodeList = $derived(Object.values($graph.nodes));
  const selection = $derived(new Set($graph.selection));

  // --- Clustering (P29) ---
  //
  // Folding a group is done entirely by REMAPPING what the existing layers are handed: the
  // members drop out of the node list, one card takes their place, and every edge that crossed
  // the group's boundary is re-pointed at that card. GraphEdges and GraphNode learn nothing
  // about clusters, which is what keeps a feature that changes the whole picture from
  // reaching into the parts that draw it.
  const clusterGeom = { width: GRAPH.NODE_WIDTH, height: GRAPH.NODE_HEIGHT, pad: GRAPH.HULL_PAD };
  const folded = $derived($clusters.visible ? $clusters.collapsed : new Set());
  const memberOf = $derived(memberIndex($clusters.list, folded));
  const clusterCards = $derived(proxyCards($clusters.list, folded, $graph.nodes, clusterGeom));

  // --- Replay (P29) ---
  //
  // Like folding, replay is a FILTER over what is drawn — nothing is removed from the graph.
  // `null` means replay is off and everything draws; a Set means only these are on screen yet.
  // The distinction matters: an empty Set and "no replay" must not render alike.
  const replayModel = $derived(buildReplay($graph.nodes, $graph.edges));
  const revealed = $derived($replay.active ? visibleAt(replayModel, $replay.t) : null);
  const replayEdges = $derived(
    revealed ? $graph.edges.filter((e) => revealed.edges.has(String(e.id))) : $graph.edges,
  );
  const remapped = $derived(remapEdges(replayEdges, memberOf));

  // A folded member is not drawn, but it is still ON the canvas — it stays in $graph.nodes, so
  // unfolding restores it exactly and nothing about the graph itself has changed. Replay
  // hides on the same principle and in the same place.
  const shownNodes = $derived(
    nodeList.filter(
      (n) => !memberOf.has(String(n.event.id)) && (!revealed || revealed.nodes.has(String(n.event.id))),
    ),
  );
  const visibleNodes = $derived(shownNodes.filter((n) => isNodeVisible(n, vp, w, h)));
  const visibleIds = $derived.by(() => {
    const ids = new Set(visibleNodes.map((n) => n.event.id));
    // Cluster cards are always "visible" for edge purposes: they are few, and an edge into a
    // folded group disappearing because its card is off-screen would read as the group having
    // no outside links at all.
    for (const c of clusterCards) ids.add(c.id);
    return ids;
  });
  // What fit-to-content measures: the cards actually on screen.
  const fitTargets = $derived.by(() => {
    if (clusterCards.length === 0) return $graph.nodes;
    const out = {};
    for (const n of shownNodes) out[n.event.id] = n;
    for (const c of clusterCards) out[c.id] = { x: c.x, y: c.y };
    return out;
  });

  // The node map the edge layer resolves endpoints through, with a cluster card standing in for
  // each folded group.
  const edgeNodes = $derived.by(() => {
    if (clusterCards.length === 0) return $graph.nodes;
    const out = { ...$graph.nodes };
    for (const c of clusterCards) out[c.id] = { x: c.x, y: c.y };
    return out;
  });

  // The live preview follows the cursor, but LOCKS ON to a snapped target's centre so the
  // user can see the link has committed before releasing — the feedback half of forgiving
  // targeting (P18.3).
  const tempEdge = $derived.by(() => {
    if (mode !== MODE.CONNECT || connectFrom === null) return null;
    const n = $graph.nodes[connectFrom];
    if (!n) return null;
    const from = { x: n.x + GRAPH.NODE_WIDTH / 2, y: n.y + GRAPH.NODE_HEIGHT / 2 };
    const t = connectTarget !== null ? $graph.nodes[connectTarget] : null;
    const to = t ? { x: t.x + GRAPH.NODE_WIDTH / 2, y: t.y + GRAPH.NODE_HEIGHT / 2 } : cursorWorld;
    return { from, to, valid: connectTarget !== null };
  });

  // Cross-view auto-focus (P14.4): when another view requests focus on an event, centre
  // the canvas on its node. Guarded by the nonce so it fires once per request, and
  // deferred until the node exists and the canvas has measured its size.
  let lastFocusNonce = 0;
  $effect(() => {
    const f = $graph.focus;
    if (!f || f.id === null || f.nonce === lastFocusNonce) return;
    const n = $graph.nodes[f.id];
    if (!n || !w || !h) return; // wait until the node is present and the canvas is sized
    lastFocusNonce = f.nonce;
    graph.setViewport(centerOn(n, vp, w, h));
  });

  // The marquee drawn on screen, and the same box projected into world space for hit
  // testing. Both derive from the raw drag corners so a box dragged in any direction works.
  const marqueeScreen = $derived(marquee ? normalizeRect(marquee.a, marquee.b) : null);
  const marqueeWorld = $derived.by(() => {
    if (!marquee) return null;
    return normalizeRect(
      screenToWorld(marquee.a.x, marquee.a.y, vp),
      screenToWorld(marquee.b.x, marquee.b.y, vp),
    );
  });
  const selectionCount = $derived($graph.selection.length);

  const relationOptions = [RELATIONS.CORRELATION, RELATIONS.TEMPORAL, RELATIONS.DEFAULT].map((r) => ({
    value: r,
    label: RELATION_LABEL[r],
  }));
  const ctxItems = [
    { id: NODE_ACTION.REMOVE, label: UI.ACTION_REMOVE_NODE },
    { id: NODE_ACTION.DELETE_DB, label: UI.ACTION_DELETE_EVENT },
  ];

  function localPoint(e) {
    const r = viewportEl.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  // Hit-testing runs over the SHOWN nodes, not every node on the canvas. A folded member is
  // still in the graph but is not on screen, and a click that selected and dragged something
  // invisible — underneath the cluster card standing in for it — would be unexplainable.
  function hitNode(world) {
    for (let i = shownNodes.length - 1; i >= 0; i--) {
      const n = shownNodes[i];
      if (world.x >= n.x && world.x <= n.x + GRAPH.NODE_WIDTH && world.y >= n.y && world.y <= n.y + GRAPH.NODE_HEIGHT) {
        return n.event.id;
      }
    }
    return null;
  }

  // Distance from a world point to a node's rectangle (0 when inside).
  function distToNode(world, n) {
    const dx = Math.max(n.x - world.x, 0, world.x - (n.x + GRAPH.NODE_WIDTH));
    const dy = Math.max(n.y - world.y, 0, world.y - (n.y + GRAPH.NODE_HEIGHT));
    return Math.hypot(dx, dy);
  }

  // Snap-to-target: an exact hit wins, otherwise the NEAREST node within the snap radius.
  // This is what makes a release that is merely close still link to the intended node
  // instead of being silently discarded (P18.3). Only the virtualized visible set is
  // considered, so the cost does not grow with the size of the graph (R-CX2).
  function hitNodeSnapped(world, excludeId) {
    const exact = hitNode(world);
    if (exact !== null && exact !== excludeId) return exact;

    let best = null;
    let bestDist = GRAPH.CONNECT_SNAP_PX;
    for (const n of visibleNodes) {
      const id = n.event.id;
      if (id === excludeId) continue;
      const d = distToNode(world, n);
      if (d <= bestDist) {
        bestDist = d;
        best = id;
      }
    }
    return best;
  }

  // cancelConnect abandons an in-flight link without creating anything.
  function cancelConnect() {
    connectFrom = null;
    connectTarget = null;
    if (mode === MODE.CONNECT) mode = MODE.IDLE;
  }

  function toggleConnectMode() {
    connectMode = !connectMode;
    if (!connectMode) cancelConnect();
  }

  // Keyboard: C toggles connect mode, Esc cancels the in-flight link and then leaves the
  // mode. Typing in a field must never trigger either.
  function onwindowkeydown(e) {
    const t = e.target;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) {
      return;
    }
    // A dialog owns the keyboard while it is open — Esc must close it, not silently change
    // the canvas mode behind it.
    if (link.open || edit.open || confirmDel.open) return;
    // Escape unwinds one layer at a time, most transient first, so a single key never
    // undoes more than the user meant.
    if (e.key === 'Escape') {
      if ($sel.relation) {
        // Most transient first: the inspector is the newest thing on screen, so one Escape
        // should close it rather than also dropping the node selection underneath it.
        sel.clearRelation();
      } else if (connectFrom !== null) {
        cancelConnect();
      } else if (connectMode) {
        connectMode = false;
      } else if ($graph.selection.length > 0) {
        graph.setSelection([]);
      }
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a') {
      e.preventDefault();
      selectAll();
      return;
    }
    if (e.ctrlKey || e.metaKey || e.altKey) return; // leave modified keys to the OS/app

    // R steps through the selected node's relations; Shift+R steps back. This is the keyboard
    // half of edge selection — the mouse got one when edges became clickable, and a feature
    // reachable only by pointer is not reachable.
    if (e.key.toLowerCase() === 'r') {
      e.preventDefault();
      stepThroughRelations(e.shiftKey ? -1 : 1);
      return;
    }

    // Keyboard connect path (P18.4): with exactly two nodes selected, Enter links them —
    // source first, target second, matching the drag direction. Dragging is not the only
    // way to assert a relationship, and it should not be the only way to record one.
    if (e.key === 'Enter' && $graph.selection.length === 2) {
      e.preventDefault();
      const [from, to] = $graph.selection;
      link = { open: true, from, to, label: '', type: RELATIONS.CORRELATION };
      return;
    }

    if (e.key.toLowerCase() === 'c') {
      e.preventDefault();
      toggleConnectMode();
    } else if (e.key.toLowerCase() === 'f') {
      e.preventDefault();
      fitView();
    }
  }

  function selectAll() {
    // Same rule as hit-testing: what is on screen is what gets selected. A count including
    // folded members would not match anything the analyst can see or act on.
    graph.setSelection(shownNodes.map((n) => n.event.id));
  }

  function onpointerdown(e) {
    // Overlay UI (toolbar, context menu) is positioned INSIDE the viewport but is not the
    // canvas; a gesture must never start on it. See interaction.js for why this silently
    // killed the toolbar buttons and the context-menu actions.
    if (isOverlayTarget(e.target)) return;

    const p = localPoint(e);
    viewportEl.setPointerCapture(e.pointerId);
    ctxMenu = { ...ctxMenu, open: false };

    const handleEl = e.target.closest('[data-handle]');
    if (handleEl) {
      mode = MODE.CONNECT;
      connectFrom = Number(handleEl.dataset.nodeId);
      cursorWorld = screenToWorld(p.x, p.y, vp);
      return;
    }

    // Clicking a link's label opens its edit dialog (no pan).
    const edgeEl = e.target.closest('[data-edge-id]');
    if (edgeEl) {
      openEdit(Number(edgeEl.dataset.edgeId));
      return;
    }

    const nodeEl = e.target.closest('[data-node-id]');
    if (nodeEl) {
      const id = Number(nodeEl.dataset.nodeId);

      // Connect mode: the whole card is the link source, so a press anywhere on it starts
      // the link and node dragging is suspended.
      if (connectMode) {
        mode = MODE.CONNECT;
        connectFrom = id;
        connectTarget = null;
        cursorWorld = screenToWorld(p.x, p.y, vp);
        return;
      }

      if (e.shiftKey) {
        const next = new Set($graph.selection);
        next.has(id) ? next.delete(id) : next.add(id);
        graph.setSelection([...next]);
      } else if (!selection.has(id)) {
        graph.setSelection([id]);
      }
      mode = MODE.DRAG;
      pointerStart = p;
      dragStart = {};
      for (const sid of new Set([...$graph.selection, id])) {
        const n = $graph.nodes[sid];
        if (n) dragStart[sid] = { x: n.x, y: n.y };
      }
      return;
    }

    // Empty canvas. Shift starts a box-select; a plain drag pans (the established gesture,
    // deliberately left alone) and clears the selection.
    if (e.shiftKey) {
      mode = MODE.MARQUEE;
      pointerStart = p;
      marquee = { a: p, b: p };
      marqueeBase = [...$graph.selection];
      return;
    }

    mode = MODE.PAN;
    pointerStart = p;
    panStartVp = { x: vp.x, y: vp.y };
    graph.setSelection([]);
  }

  function onpointermove(e) {
    if (mode === MODE.IDLE) return;
    const p = localPoint(e);
    if (mode === MODE.PAN) {
      graph.setViewport({ x: panStartVp.x + (p.x - pointerStart.x), y: panStartVp.y + (p.y - pointerStart.y) });
    } else if (mode === MODE.DRAG) {
      const dx = (p.x - pointerStart.x) / vp.zoom;
      const dy = (p.y - pointerStart.y) / vp.zoom;
      for (const [sid, start] of Object.entries(dragStart)) {
        graph.moveNode(Number(sid), start.x + dx, start.y + dy);
      }
    } else if (mode === MODE.CONNECT) {
      cursorWorld = screenToWorld(p.x, p.y, vp);
      connectTarget = hitNodeSnapped(cursorWorld, connectFrom);
    } else if (mode === MODE.MARQUEE) {
      marquee = { a: pointerStart, b: p };
      // Live preview: the selection updates as the box grows, so the user sees what they
      // are about to get instead of finding out on release.
      graph.setSelection([...new Set([...marqueeBase, ...nodesInRect($graph.nodes, marqueeWorld)])]);
    }
  }

  function onpointerup(e) {
    if (mode === MODE.CONNECT && connectFrom !== null) {
      const p = localPoint(e);
      // Resolve through the same snapping used for the preview, so what the user saw
      // highlighted is exactly what gets linked. Releasing over empty space links nothing.
      const target = hitNodeSnapped(screenToWorld(p.x, p.y, vp), connectFrom);
      if (target !== null) {
        link = { open: true, from: connectFrom, to: target, label: '', type: RELATIONS.CORRELATION };
      }
    }
    if (mode === MODE.MARQUEE) {
      // A box too small to be intentional is a shaky click, not a selection gesture: keep
      // whatever was selected before rather than acting on a 2px box.
      const r = marqueeScreen;
      if (r && r.width < GRAPH.MARQUEE_MIN_PX && r.height < GRAPH.MARQUEE_MIN_PX) {
        graph.setSelection(marqueeBase);
      }
      marquee = null;
      marqueeBase = [];
    }
    connectFrom = null;
    connectTarget = null;
    mode = MODE.IDLE;
    try {
      viewportEl.releasePointerCapture(e.pointerId);
    } catch (_) {
      /* capture may already be gone */
    }
  }

  function onwheel(e) {
    e.preventDefault();
    const p = localPoint(e);
    const factor = e.deltaY < 0 ? 1 + GRAPH.ZOOM_STEP : 1 - GRAPH.ZOOM_STEP;
    graph.setViewport(zoomAround(vp, vp.zoom * factor, p.x, p.y));
  }

  function oncontextmenu(e) {
    const nodeEl = e.target.closest('[data-node-id]');
    if (!nodeEl) return;
    e.preventDefault();
    const p = localPoint(e);
    ctxMenu = { open: true, x: p.x, y: p.y, nodeId: Number(nodeEl.dataset.nodeId) };
  }

  function onCtxSelect(action) {
    const nodeId = ctxMenu.nodeId;
    ctxMenu = { ...ctxMenu, open: false };
    if (nodeId === null) return;
    if (action === NODE_ACTION.REMOVE) {
      graph.removeNode(nodeId);
    } else if (action === NODE_ACTION.DELETE_DB) {
      confirmDel = { open: true, nodeId };
    }
  }

  async function createLink() {
    const { from, to, label, type } = link;
    link = { ...link, open: false };
    if (from === null || to === null) return;
    try {
      await graph.connect(from, to, type, label);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  function openEdit(id) {
    const e = $graph.edges.find((x) => x.id === id);
    if (!e) return;
    edit = { open: true, id, label: e.relation_label || '', type: e.relation_type || RELATIONS.DEFAULT };
  }
  async function saveEdit() {
    const { id, label, type } = edit;
    edit = { ...edit, open: false };
    if (id === null) return;
    try {
      await graph.updateRelation(id, type, label);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }
  async function deleteEdit() {
    const { id } = edit;
    edit = { ...edit, open: false };
    if (id === null) return;
    try {
      await graph.deleteRelation(id);
      snackbar.success(UI.LINK_DELETED);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }
  async function confirmDeleteEvent() {
    const id = confirmDel.nodeId;
    confirmDel = { open: false, nodeId: null };
    if (id === null) return;
    try {
      await graph.deleteEvent(id);
      snackbar.success(UI.EVENT_DELETED);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  function zoomBy(factor) {
    graph.setViewport(zoomAround(vp, clampZoom(vp.zoom * factor), w / 2, h / 2));
  }
  // fitView frames every node. It replaces the old "jump to world origin" reset, which
  // could land on empty canvas exactly when the user was lost and reached for it.
  function fitView() {
    // Fit to what is drawn: the shown nodes plus any cluster cards. Fitting to folded members
    // would zoom out to cover cards that are not there.
    graph.setViewport(fitToNodes(fitTargets, w, h));
  }
  /**
   * inspectEdge selects a relation and pulls in everything the inspector shows.
   *
   * A failure is silent on purpose: the edge may have been deleted by another view between the
   * click and the call, and an error dialog for "the thing you clicked is gone" is worse than
   * simply not opening a panel about it.
   */
  async function inspectEdge(id) {
    try {
      sel.selectRelation(await api.inspectRelation(id));
    } catch {
      sel.clearRelation();
    }
  }

  /**
   * stepThroughRelations is the keyboard route to selecting a relation.
   *
   * It is bound to a bare letter like the canvas's other actions, and it steps through the
   * edges of the CURRENT node selection rather than through every edge in the graph: a graph
   * holds hundreds, and a cycle that long is not something anyone walks.
   *
   * Nothing selected, or a node with no relations, is a no-op that says so — a key that
   * silently does nothing is indistinguishable from a broken one.
   */
  function stepThroughRelations(direction) {
    if ($graph.selection.length === 0) {
      snackbar.info(UI.RELATION_STEP_NEEDS_SELECTION);
      return;
    }
    const cycle = relationsOfSelection($graph.edges, $graph.selection);
    if (cycle.length === 0) {
      snackbar.info(UI.RELATION_STEP_NONE);
      return;
    }
    const next = stepRelation(cycle, $sel.relation?.relation?.id ?? null, direction);
    if (next !== null) inspectEdge(next);
  }

  function autoLayout() {
    const ids = Object.keys($graph.nodes);
    ids.forEach((id, i) => {
      const col = i % GRAPH.AUTO_LAYOUT_COLS;
      const row = Math.floor(i / GRAPH.AUTO_LAYOUT_COLS);
      graph.moveNode(Number(id), col * GRAPH.AUTO_LAYOUT_GAP_X + GRAPH.GRID, row * GRAPH.AUTO_LAYOUT_GAP_Y + GRAPH.GRID);
    });
    graph.setLayout(LAYOUT.AUTO);
  }
</script>

<svelte:window onkeydown={onwindowkeydown} />

<div
  class="viewport"
  bind:this={viewportEl}
  bind:clientWidth={w}
  bind:clientHeight={h}
  role="application"
  aria-label={UI.NAV_GRAPH}
  class:connecting={mode === MODE.CONNECT || connectMode}
  style="background-size: {GRAPH.GRID * vp.zoom}px {GRAPH.GRID * vp.zoom}px; background-position: {vp.x}px {vp.y}px"
  {onpointerdown}
  {onpointermove}
  {onpointerup}
  {onwheel}
  {oncontextmenu}
>
  <div class="world" style="transform: translate({vp.x}px, {vp.y}px) scale({vp.zoom})">
    {#if $clusters.visible}
      <!-- Beneath the edges and cards: a hull is context for the evidence, not a thing drawn
           over it. -->
      <GraphHulls
        clusters={$clusters.list}
        nodes={$graph.nodes}
        collapsed={folded}
        ontoggle={(id) => clusters.toggle(id)}
      />
    {/if}
    <GraphEdges
      edges={remapped.edges}
      nodes={edgeNodes}
      {visibleIds}
      {tempEdge}
      freshEdges={$graph.fresh.edges}
      highlighted={highlightedEdges($sel.relation)}
      onselect={inspectEdge}
    />
    {#each visibleNodes as n (n.event.id)}
      <GraphNode
        node={n}
        selected={selection.has(n.event.id)}
        highlighted={connectTarget === n.event.id}
        {connectMode}
        connectSource={connectFrom === n.event.id}
        entering={$graph.fresh.nodes.has(n.event.id)}
        finding={$findings.byKey[n.event.hash_normalized]}
      />
    {/each}
    {#each clusterCards as c (c.id)}
      <ClusterCard card={c} onexpand={(id) => clusters.toggle(id)} />
    {/each}
  </div>

  <!-- Box-select overlay: drawn in SCREEN space so its border stays 1px at any zoom. -->
  {#if marqueeScreen}
    <div
      class="marquee"
      style="left: {marqueeScreen.x}px; top: {marqueeScreen.y}px; width: {marqueeScreen.width}px; height: {marqueeScreen.height}px"
    ></div>
  {/if}

  {#if selectionCount > 0}
    <div class="selcount">{selectionCount} {UI.SELECTION_COUNT_SUFFIX}</div>
  {/if}

  {#if nodeList.length === 0}
    <div class="empty">{UI.GRAPH_EMPTY}</div>
  {:else}
    <!-- The hint states the CURRENT interaction, so the active mode is never ambiguous. -->
    <div class="tip" class:mode={connectMode}>
      {#if mode === MODE.CONNECT}
        {UI.CONNECT_RELEASE_HINT}
      {:else if connectMode}
        {UI.CONNECT_MODE_HINT}
      {:else if mode === MODE.MARQUEE}
        {UI.MARQUEE_HINT}
      {:else}
        {$graph.edges.length === 0 ? UI.GRAPH_CONNECT_HINT : UI.MARQUEE_HINT}
      {/if}
    </div>
  {/if}

  <div class="toolbar" {...{ [OVERLAY_ATTR]: '' }}>
    <button
      class="connect"
      class:active={connectMode}
      onclick={toggleConnectMode}
      title="{UI.ACTION_CONNECT_MODE} ({UI.CONNECT_SHORTCUT_HINT})"
      aria-label={UI.ACTION_CONNECT_MODE}
      aria-pressed={connectMode}
    >
      ⇢
    </button>
    <button onclick={() => zoomBy(1 + GRAPH.ZOOM_STEP)} title={UI.ACTION_ZOOM_IN} aria-label={UI.ACTION_ZOOM_IN}>+</button>
    <button onclick={() => zoomBy(1 - GRAPH.ZOOM_STEP)} title={UI.ACTION_ZOOM_OUT} aria-label={UI.ACTION_ZOOM_OUT}>−</button>
    <button onclick={fitView} title={UI.ACTION_FIT_VIEW} aria-label={UI.ACTION_FIT_VIEW}>⛶</button>
    <button onclick={autoLayout} title={UI.ACTION_AUTO_LAYOUT} aria-label={UI.ACTION_AUTO_LAYOUT}>▦</button>
  </div>

  {#if ctxMenu.open}
    <div class="anchor" {...{ [OVERLAY_ATTR]: '' }} style="left: {ctxMenu.x}px; top: {ctxMenu.y}px">
      <Menu bind:open={ctxMenu.open} items={ctxItems} onselect={onCtxSelect} />
    </div>
  {/if}
</div>

<Dialog bind:open={link.open} title={UI.LINK_TITLE}>
  <div class="linkform">
    <TextField label={UI.LINK_LABEL} placeholder={UI.LINK_PLACEHOLDER} bind:value={link.label} />
    <Select label={UI.CONNECT_PICK_RELATION} options={relationOptions} bind:value={link.type} />
  </div>
  {#snippet actions()}
    <Button variant="text" onclick={() => (link = { ...link, open: false })}>{UI.ACTION_CANCEL}</Button>
    <Button onclick={createLink}>{UI.LINK_CREATE}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={edit.open} title={UI.EDIT_LINK_TITLE}>
  <div class="linkform">
    <TextField label={UI.LINK_LABEL} placeholder={UI.LINK_PLACEHOLDER} bind:value={edit.label} />
    <Select label={UI.CONNECT_PICK_RELATION} options={relationOptions} bind:value={edit.type} />
  </div>
  {#snippet actions()}
    <Button variant="outlined" onclick={deleteEdit}>{UI.ACTION_DELETE}</Button>
    <Button variant="text" onclick={() => (edit = { ...edit, open: false })}>{UI.ACTION_CANCEL}</Button>
    <Button onclick={saveEdit}>{UI.LINK_UPDATE}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={confirmDel.open} title={UI.CONFIRM_DELETE_EVENT_TITLE}>
  <p class="confirm">{UI.CONFIRM_DELETE_EVENT_BODY}</p>
  {#snippet actions()}
    <Button variant="text" onclick={() => (confirmDel = { open: false, nodeId: null })}>{UI.ACTION_CANCEL}</Button>
    <Button onclick={confirmDeleteEvent}>{UI.ACTION_DELETE}</Button>
  {/snippet}
</Dialog>

<style>
  .viewport {
    position: relative;
    width: 100%;
    height: 100%;
    overflow: hidden;
    background-color: var(--color-background);
    background-image: radial-gradient(var(--color-outline) 1px, transparent 1px);
    touch-action: none;
    cursor: grab;
  }
  .viewport:active {
    cursor: grabbing;
  }
  .viewport.connecting {
    cursor: crosshair;
  }
  .world {
    position: absolute;
    left: 0;
    top: 0;
    transform-origin: 0 0;
    will-change: transform;
  }
  .empty,
  .tip {
    position: absolute;
    font-family: var(--font-sans);
    color: var(--color-on-surface-muted);
    pointer-events: none;
  }
  .empty {
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .tip {
    left: 50%;
    bottom: var(--space-4);
    transform: translateX(-50%);
    font-size: 0.8rem;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: 999px;
    padding: var(--space-2) var(--space-4);
    box-shadow: var(--elevation-1);
  }
  .toolbar {
    position: absolute;
    top: var(--space-4);
    left: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .toolbar button {
    width: 40px;
    height: 40px;
    border-radius: var(--radius-md);
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    color: var(--color-on-surface);
    font-size: 1.1rem;
    cursor: pointer;
    box-shadow: var(--elevation-1);
  }
  .toolbar button:hover {
    background: var(--color-surface-variant);
  }
  .toolbar button:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  /* An engaged mode must be unmistakable from across the canvas. */
  .toolbar button.connect.active {
    background: var(--color-accent);
    color: var(--color-on-accent);
    border-color: var(--color-accent);
    box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-accent) 30%, transparent);
  }
  .tip.mode {
    color: var(--color-on-accent);
    background: var(--color-accent);
    border-color: var(--color-accent);
  }
  .anchor {
    position: absolute;
  }
  /* Box-select rectangle. Purely visual — it must never intercept the drag creating it. */
  .marquee {
    position: absolute;
    border: 1px solid var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 14%, transparent);
    border-radius: 2px;
    pointer-events: none;
    z-index: 5;
  }
  .selcount {
    position: absolute;
    top: var(--space-4);
    right: var(--space-4);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    font-weight: 700;
    color: var(--color-on-primary);
    background: var(--color-primary);
    border-radius: 999px;
    padding: var(--space-1) var(--space-3);
    pointer-events: none;
    box-shadow: var(--elevation-1);
  }
  .linkform {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    min-width: 320px;
  }
  .confirm {
    font-family: var(--font-sans);
    font-size: 0.92rem;
    line-height: 1.5;
    color: var(--color-on-surface);
    margin: 0;
    max-width: 420px;
  }
</style>
