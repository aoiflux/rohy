<script>
  // Graph view (P7). Hosts the canvas plus a side panel for adding ingested events as
  // nodes and loading/clearing the persisted mapping. Node/edge state lives in the
  // graph store; all persistence goes through the API wrapper.
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { theme } from '../stores/theme.js';
  import { route } from '../stores/router.js';
  import { events } from '../stores/events.js';
  import { graph } from '../stores/graph.js';
  import { findings } from '../stores/findings.js';
  import { snackbar } from '../stores/snackbar.js';
  import * as api from '../lib/api/index.js';
  import { ROUTES, UI, GRAPH, THEMES, PICKER_SEARCH_DEBOUNCE_MS } from '../lib/consts/index.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import List from '../components/material/List.svelte';
  import ListItem from '../components/material/ListItem.svelte';
  import Select from '../components/material/Select.svelte';
  import Dialog from '../components/material/Dialog.svelte';
  import TextField from '../components/material/TextField.svelte';
  import GraphCanvas from '../components/graph/GraphCanvas.svelte';

  const onCanvas = $derived(new Set(Object.keys($graph.nodes).map(Number)));

  // Multiple graphs (P15): the switcher options + the active graph's counts.
  const graphOptions = $derived(($graph.graphs || []).map((g) => ({ value: g.id, label: g.name })));
  const activeGraph = $derived(($graph.graphs || []).find((g) => g.id === $graph.activeGraphId) || null);

  let graphDialog = $state({ open: false, mode: 'create', id: 0, name: '', description: '' });
  let confirmDeleteGraph = $state(false);

  onMount(async () => {
    if ($events.list.length === 0) events.load();
    await graph.loadGraphs();
    await loadMapping();
  });

  // Findings for whatever is on the canvas, so an analyst's flag travels with the event
  // between views rather than only existing on the events page.
  $effect(() => {
    const keys = Object.values($graph.nodes)
      .map((n) => n.event && n.event.hash_normalized)
      .filter(Boolean);
    if (keys.length) findings.loadFor(keys);
  });

  async function switchGraph(id) {
    try {
      await graph.setActive(Number(id));
      await loadMapping();
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  function openCreateGraph() {
    graphDialog = { open: true, mode: 'create', id: 0, name: '', description: '' };
  }
  function openRenameGraph() {
    if (!activeGraph) return;
    graphDialog = { open: true, mode: 'rename', id: activeGraph.id, name: activeGraph.name, description: activeGraph.description || '' };
  }
  async function submitGraphDialog() {
    const { mode, id, name, description } = graphDialog;
    graphDialog = { ...graphDialog, open: false };
    if (!name.trim()) return;
    try {
      if (mode === 'create') {
        await graph.createGraph(name.trim(), description);
        await loadMapping();
        snackbar.success(UI.GRAPH_CREATED);
      } else {
        await graph.renameGraph(id, name.trim(), description);
        snackbar.success(UI.GRAPH_RENAMED);
      }
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }
  async function doDeleteGraph() {
    confirmDeleteGraph = false;
    const id = $graph.activeGraphId;
    try {
      await graph.deleteGraph(id);
      await loadMapping();
      snackbar.success(UI.GRAPH_DELETED);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  function placeAt(index) {
    const col = index % GRAPH.AUTO_LAYOUT_COLS;
    const row = Math.floor(index / GRAPH.AUTO_LAYOUT_COLS);
    return { x: col * GRAPH.AUTO_LAYOUT_GAP_X + GRAPH.GRID, y: row * GRAPH.AUTO_LAYOUT_GAP_Y + GRAPH.GRID };
  }

  function add(event) {
    if (onCanvas.has(event.id)) return;
    const p = placeAt(Object.keys($graph.nodes).length);
    graph.addNode(event, p.x, p.y);
  }

  // loadMapping restores the ACTIVE graph: its edges, plus every event that is either an
  // edge endpoint or a placed-but-unlinked member (from the per-graph layout), then the
  // saved positions/viewport. Events are shared, so this only adds nodes to the canvas.
  async function loadMapping() {
    try {
      const rels = (await graph.loadRelations()) || [];
      const saved = await graph.loadLayout();
      const layoutIds = saved && saved.nodes ? Object.keys(saved.nodes).map(Number) : [];
      const ids = [...new Set([...rels.flatMap((r) => [r.from, r.to]), ...layoutIds])];
      if (ids.length) {
        const evs = await api.getEvents(ids);
        (evs || []).forEach((e, i) => {
          if (!onCanvas.has(e.id)) {
            const p = placeAt(Object.keys($graph.nodes).length + i);
            graph.addNode(e, p.x, p.y, { animate: false });
          }
        });
      }
      // Restore saved node positions + viewport on top of the loaded nodes.
      graph.applyLayout(saved);
      snackbar.success(`${rels.length} ${UI.MAPPING_LOADED}`);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  async function saveLayout() {
    try {
      await graph.saveLayout();
      snackbar.success(UI.LAYOUT_SAVED);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  // Progressively load more events into the picker as the panel scrolls (P1.4 parity),
  // so every event is reachable here — the canvas only shows placed nodes by design.
  function panelScroll(e) {
    const el = e.currentTarget;
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - 200) events.loadMore();
  }

  // Picker search. Debounced so typing does not fire a query per keystroke, and applied to
  // the shared events filter so it narrows the whole result set rather than only the rows
  // already paged in.
  let pickerQuery = $state($events.filter.search || '');
  let searchTimer;

  $effect(() => {
    const q = pickerQuery;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      if ((get(events).filter.search || '') === q) return;
      events.setFilter({ search: q, offset: 0 });
      events.load();
    }, PICKER_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(searchTimer);
  });

  function clearPickerSearch() {
    pickerQuery = '';
  }
</script>

<div class="gv">
<AppBar title={UI.NAV_GRAPH}>
  <Button variant="text" onclick={() => route.go(ROUTES.DASHBOARD)}>{UI.NAV_DASHBOARD}</Button>
  <Button variant="text" onclick={() => route.go(ROUTES.EVENTS)}>{UI.NAV_EVENTS}</Button>
  <Button variant="text" onclick={() => route.go(ROUTES.RULES)}>{UI.NAV_RULES}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.TIMELINE)}>{UI.NAV_TIMELINE}</Button>
  <Button variant="text" onclick={loadMapping}>{UI.ACTION_LOAD_MAPPING}</Button>
  <Button variant="text" onclick={saveLayout}>{UI.ACTION_SAVE_LAYOUT}</Button>
  <Button variant="text" onclick={() => graph.clearCanvas()}>{UI.ACTION_CLEAR_CANVAS}</Button>
  <Button variant="tonal" onclick={() => theme.toggle()}>
    {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
  </Button>
</AppBar>

<div class="layout">
  <aside class="panel" onscroll={panelScroll}>
    <div class="graphbar">
      <Select
        label={UI.LABEL_GRAPH}
        options={graphOptions}
        value={$graph.activeGraphId}
        onchange={switchGraph}
      />
      <div class="graphbtns">
        <Button variant="text" onclick={openCreateGraph}>{UI.ACTION_NEW_GRAPH}</Button>
        <Button variant="text" onclick={openRenameGraph} disabled={!activeGraph}>{UI.ACTION_RENAME_GRAPH}</Button>
        <Button
          variant="text"
          onclick={() => (confirmDeleteGraph = true)}
          disabled={($graph.graphs || []).length <= 1}
        >
          {UI.ACTION_DELETE_GRAPH}
        </Button>
      </div>
    </div>

    <div class="paneltitle">
      <h3>{UI.PANEL_EVENTS}</h3>
      {#if $events.total > 0}
        <span class="pcount">{$events.list.length} {UI.EVENTS_OF} {$events.total}</span>
      {/if}
    </div>

    <!-- Searching the BACKEND, not just the rows already loaded: filtering only the loaded
         page would quietly claim "no matches" for events that simply had not been paged in
         yet. It is the same filter the Events page uses, so the two views agree. -->
    <div class="picksearch">
      <TextField label={UI.PICKER_SEARCH} bind:value={pickerQuery} />
      {#if pickerQuery}
        <Button variant="text" onclick={clearPickerSearch}>{UI.ACTION_CLEAR_FILTERS}</Button>
      {/if}
    </div>
    {#if $events.list.length === 0}
      <p class="empty">{UI.EMPTY_EVENTS}</p>
    {:else}
      <List>
        {#each $events.list as ev (ev.id)}
          <ListItem onclick={() => add(ev)}>
            <b class="eid">{ev.event_id}</b>
            <span class="prov">{ev.provider}</span>
            {#if onCanvas.has(ev.id)}<span class="on">✓</span>{/if}
          </ListItem>
        {/each}
      </List>
      {#if $events.loadingMore}<p class="empty">{UI.LOADING_MORE}</p>{/if}
    {/if}
  </aside>

  <section class="canvas">
    <GraphCanvas />
  </section>
</div>
</div>

<Dialog bind:open={graphDialog.open} title={graphDialog.mode === 'create' ? UI.NEW_GRAPH_TITLE : UI.RENAME_GRAPH_TITLE}>
  <div class="graphform">
    <TextField label={UI.LABEL_GRAPH_NAME} placeholder={UI.GRAPH_NAME_PLACEHOLDER} bind:value={graphDialog.name} />
    <TextField label={UI.LABEL_GRAPH_DESC} bind:value={graphDialog.description} />
  </div>
  {#snippet actions()}
    <Button variant="text" onclick={() => (graphDialog = { ...graphDialog, open: false })}>{UI.ACTION_CANCEL}</Button>
    <Button onclick={submitGraphDialog}>{graphDialog.mode === 'create' ? UI.ACTION_NEW_GRAPH : UI.ACTION_RENAME_GRAPH}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={confirmDeleteGraph} title={UI.CONFIRM_DELETE_GRAPH_TITLE}>
  <p class="confirm">{UI.CONFIRM_DELETE_GRAPH_BODY}</p>
  {#snippet actions()}
    <Button variant="text" onclick={() => (confirmDeleteGraph = false)}>{UI.ACTION_CANCEL}</Button>
    <Button onclick={doDeleteGraph}>{UI.ACTION_DELETE_GRAPH}</Button>
  {/snippet}
</Dialog>

<style>
  .gv {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .layout {
    display: grid;
    grid-template-columns: 300px 1fr;
    flex: 1;
    min-height: 0;
  }
  .panel {
    border-right: 1px solid var(--color-outline);
    background: var(--color-surface);
    padding: var(--space-4);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    /* Grid items default to min-height:auto, so this refused to shrink below its content:
       the row grew past the viewport, overflow-y never engaged, and the overflow was
       clipped by the ancestor — the panel simply could not be scrolled. */
    min-height: 0;
  }
  .graphbar {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-bottom: var(--space-3);
    border-bottom: 1px solid var(--color-outline);
  }
  .graphbtns {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .graphform {
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
  .picksearch {
    display: flex;
    align-items: flex-end;
    gap: var(--space-2);
    flex: 0 0 auto;
  }
  .picksearch :global(.field) {
    flex: 1;
    min-width: 0;
  }
  .paneltitle {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2);
    flex: 0 0 auto;
  }
  .pcount {
    font-family: var(--font-sans);
    font-size: 0.74rem;
    color: var(--color-on-surface-muted);
  }
  h3 {
    font-family: var(--font-sans);
    font-weight: 800;
    margin: 0;
    color: var(--color-on-surface);
  }
  .empty {
    font-family: var(--font-sans);
    color: var(--color-on-surface-muted);
    font-size: 0.88rem;
  }
  .canvas {
    position: relative;
    min-width: 0;
    min-height: 0; /* same grid-item rule as .panel: let it shrink, not push the row taller */
    overflow: hidden;
  }
  .eid {
    color: var(--color-primary);
    min-width: 52px;
  }
  .prov {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .on {
    color: var(--color-success);
    font-weight: 800;
  }
</style>
