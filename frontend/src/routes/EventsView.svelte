<script>
  // Events / Forensics view (P6.1 + P6.3). Filter bar → virtualized chronological
  // timeline → export, with a detail drawer (P6.2) and an Add-to-Graph action. All
  // reads go through the events store; export is client-side over the loaded page.
  import { onMount } from 'svelte';
  import { theme } from '../stores/theme.js';
  import { route } from '../stores/router.js';
  import { events } from '../stores/events.js';
  import { findings } from '../stores/findings.js';
  import { graph } from '../stores/graph.js';
  import { snackbar } from '../stores/snackbar.js';
  import { exportJSON, exportCSV } from '../lib/export.js';
  import * as api from '../lib/api/index.js';
  import { get } from 'svelte/store';
  import { UI, ROUTES, THEMES, GRAPH, RELATION_LABEL, UNDATED } from '../lib/consts/index.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import FilterBar from '../components/events/FilterBar.svelte';
  import VirtualList from '../components/events/VirtualList.svelte';
  import ProgressBar from '../components/material/ProgressBar.svelte';
  import EventDetail from '../components/events/EventDetail.svelte';

  let active = $state(/** @type {any} */ (null));
  // Relation-aware highlighting (P14): adjacency summary for the loaded window, and the
  // currently selected event whose related rows are highlighted (persists after the
  // detail drawer closes so the highlight stays visible in the table).
  let adjacency = $state(/** @type {Record<number,{count:number,types:string[],related_ids:number[]}>} */ ({}));
  let selectedId = $state(/** @type {number|null} */ (null));

  onMount(() => {
    if ($events.list.length === 0) events.load();
    findings.refreshMeta();
  });

  // Load the analyst's marks for the loaded window in one call, alongside the relation
  // adjacency. Findings are keyed by content hash, so that is what is sent.
  $effect(() => {
    const keys = $events.list.map((e) => e.hash_normalized).filter(Boolean);
    if (keys.length) findings.loadFor(keys);
  });

  // Refresh the relation adjacency whenever the loaded event set changes. One backend
  // call summarizes the whole window (no per-row round-trips).
  $effect(() => {
    const ids = $events.list.map((e) => e.id);
    if (ids.length === 0) {
      adjacency = {};
      return;
    }
    api
      .relationsAdjacency(ids)
      .then((a) => {
        adjacency = a || {};
      })
      .catch(() => {
        adjacency = {};
      });
  });

  // Neighbor ids of the selected event → the rows to highlight as "related".
  const relatedSet = $derived.by(() => {
    const a = selectedId != null ? adjacency[selectedId] : null;
    return new Set(a && a.related_ids ? a.related_ids : []);
  });

  // The related events (that are in the loaded window) for the detail drawer's list.
  const activeRelated = $derived.by(() => {
    const a = active ? adjacency[active.id] : null;
    if (!a || !a.related_ids) return [];
    const byId = new Map($events.list.map((e) => [e.id, e]));
    return a.related_ids.map((id) => byId.get(id)).filter(Boolean);
  });

  // An event with no timestamp shows an explicit dash. Rendering the zero date (or "1 Jan
  // 1970") would read as a real time and misrepresent the evidence.
  function fmtTime(ts) {
    if (!ts) return UI.UNDATED_TIMESTAMP;
    const t = new Date(ts).getTime();
    if (!Number.isFinite(t) || t <= 0) return UI.UNDATED_TIMESTAMP;
    return new Date(t).toLocaleString();
  }

  // Tooltip: "3 relations · Temporal, Correlation · 2 by rule, 1 by hand".
  // The provenance split matters: "the tool inferred this" and "an analyst asserted this"
  // are different claims about the evidence and must be distinguishable at a glance (P11).
  function relationTitle(adj) {
    if (!adj) return '';
    const word = adj.count === 1 ? UI.RELATION_ONE : UI.RELATION_MANY;
    const types = (adj.types || []).map((t) => RELATION_LABEL[t] || t).join(', ');
    const parts = [];
    if (adj.system_count) parts.push(`${adj.system_count} ${UI.RELATION_BY_RULE}`);
    if (adj.user_count) parts.push(`${adj.user_count} ${UI.RELATION_BY_HAND}`);
    const origin = parts.length ? ` · ${parts.join(', ')}` : '';
    return `${adj.count} ${word}${types ? ` · ${types}` : ''}${origin}`;
  }

  function selectRow(ev) {
    active = ev;
    selectedId = ev.id;
  }

  function selectRelated(id) {
    const ev = $events.list.find((e) => e.id === id);
    if (ev) selectRow(ev);
  }

  // P14.4 cross-view auto-focus: ensure the event is on the canvas, request focus, and
  // navigate to the graph where the canvas centres on it.
  function showInGraph(ev) {
    if (!$graph.nodes[ev.id]) {
      const count = Object.keys($graph.nodes).length;
      const col = count % GRAPH.AUTO_LAYOUT_COLS;
      const row = Math.floor(count / GRAPH.AUTO_LAYOUT_COLS);
      graph.addNode(ev, col * GRAPH.AUTO_LAYOUT_GAP_X + GRAPH.GRID, row * GRAPH.AUTO_LAYOUT_GAP_Y + GRAPH.GRID);
    }
    graph.focusEvent(ev.id);
    active = null;
    route.go(ROUTES.GRAPH);
  }

  // Export the FULL filtered result set (not just the loaded page): re-query the backend
  // with the active filter and no row limit, then download. That query can take a moment
  // on a large case, so the action reports that it is working rather than appearing dead.
  let exporting = $state(/** @type {string|null} */ (null));

  async function doExport(kind) {
    if (!$events.list.length) {
      snackbar.warn(UI.EXPORT_EMPTY);
      return;
    }
    if (exporting) return; // one export at a time; the button is disabled meanwhile
    exporting = kind;
    try {
      const filter = { ...get(events).filter, offset: 0, limit: 0 };
      const all = (await api.queryEvents(filter)) || [];
      const rows = all.length ? all : $events.list;
      // Findings are fetched for the WHOLE exported set, not read from the store's cache:
      // that cache only holds the pages scrolled through, so exporting from it would drop
      // annotations on events the user never happened to scroll past.
      const byKey = await findingsFor(rows);
      kind === 'csv' ? exportCSV(rows, undefined, byKey) : exportJSON(rows, undefined, byKey);
      snackbar.success(`${UI.EXPORT_DONE} ${rows.length} ${UI.RESULT_COUNT}`);
    } catch (err) {
      // Falling back to the loaded page is better than failing outright — but the user
      // must be told the file is partial, not silently handed a short export.
      const byKey = await findingsFor($events.list).catch(() => undefined);
      kind === 'csv'
        ? exportCSV($events.list, undefined, byKey)
        : exportJSON($events.list, undefined, byKey);
      snackbar.warn(`${UI.EXPORT_PARTIAL} ${$events.list.length} ${UI.RESULT_COUNT}`);
    } finally {
      exporting = null;
    }
  }

  // findingsFor resolves the annotations for a set of events. A failure here must not lose
  // the export: the evidence is the part that cannot be reproduced by clicking again, so an
  // export without findings still beats no export at all.
  async function findingsFor(rows) {
    const keys = rows.map((e) => e.hash_normalized).filter(Boolean);
    if (!keys.length) return undefined;
    try {
      return (await api.getFindings(keys)) || undefined;
    } catch (_) {
      return undefined;
    }
  }

  // Timeline participation (P23). The events page is the complete inventory, so undated
  // events are listed like any other; what changes is that each row SAYS whether it can
  // appear on a timeline. Nothing here is hidden or dimmed.
  function onTimeline(ev) {
    return !!ev.timestamp && new Date(ev.timestamp).getTime() > 0;
  }

  function addToGraph(event) {
    const count = Object.keys($graph.nodes).length;
    const col = count % GRAPH.AUTO_LAYOUT_COLS;
    const row = Math.floor(count / GRAPH.AUTO_LAYOUT_COLS);
    graph.addNode(event, col * GRAPH.AUTO_LAYOUT_GAP_X + GRAPH.GRID, row * GRAPH.AUTO_LAYOUT_GAP_Y + GRAPH.GRID);
    active = null;
    snackbar.success(UI.ADDED_TO_GRAPH, { action: { label: UI.NAV_GRAPH, run: () => route.go(ROUTES.GRAPH) } });
  }
</script>

<div class="view">
  <AppBar title={UI.NAV_EVENTS}>
    <span class="count">
      {$events.list.length} {UI.EVENTS_OF} {$events.total} {UI.RESULT_COUNT}
    </span>
    <Button variant="text" onclick={() => route.go(ROUTES.DASHBOARD)}>{UI.NAV_DASHBOARD}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.GRAPH)}>{UI.NAV_GRAPH}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.RULES)}>{UI.NAV_RULES}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.TIMELINE)}>{UI.NAV_TIMELINE}</Button>
    <Button variant="text" onclick={() => doExport('json')} disabled={exporting !== null}>
      {exporting === 'json' ? UI.EXPORTING : UI.ACTION_EXPORT_JSON}
    </Button>
    <Button variant="text" onclick={() => doExport('csv')} disabled={exporting !== null}>
      {exporting === 'csv' ? UI.EXPORTING : UI.ACTION_EXPORT_CSV}
    </Button>
    <Button variant="tonal" onclick={() => theme.toggle()}>
      {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
    </Button>
  </AppBar>

  <FilterBar />

  <!-- One bar for every kind of "working": a fresh query, an appended page, or an export
       gathering the full result set. Indeterminate because none of them report progress —
       claiming a percentage we do not have would be worse than an honest sweep. -->
  {#if $events.loading || $events.loadingMore || exporting}
    <div class="busy" aria-label={exporting ? UI.EXPORTING : UI.SPLASH_LOADING}>
      <ProgressBar />
    </div>
  {/if}

  <div class="head">
    <span>{UI.LABEL_EVENT_ID}</span>
    <span>{UI.LABEL_PROVIDER}</span>
    <span>{UI.LABEL_CHANNEL}</span>
    <span>{UI.LABEL_USER}</span>
    <span>{UI.LABEL_TIMESTAMP}</span>
  </div>

  <div class="list">
    {#if $events.loading}
      <p class="msg">{UI.SPLASH_LOADING}</p>
    {:else if $events.list.length === 0}
      <p class="msg">{UI.EMPTY_EVENTS}</p>
    {:else}
      <VirtualList items={$events.list} onEndReached={() => events.loadMore()}>
        {#snippet row(ev)}
          <button
            class="erow"
            class:selected={ev.id === selectedId}
            class:related={relatedSet.has(ev.id)}
            onclick={() => selectRow(ev)}
            type="button"
          >
            <span class="eid">
              {ev.event_id}
              {#if ev.deduplication_count > 1}
                <span class="dedup" title={UI.BADGE_DEDUP_TITLE}>×{ev.deduplication_count}</span>
              {/if}
              {#if adjacency[ev.id]}
                <!-- Colour-coded by provenance: a rule-correlated event is visually
                     distinct from one an analyst mapped by hand. -->
                <span
                  class="rel"
                  class:byrule={adjacency[ev.id].system_count > 0}
                  class:byhand={adjacency[ev.id].system_count === 0}
                  title={relationTitle(adjacency[ev.id])}
                  aria-label={UI.RELATION_BADGE_ARIA}
                >
                  {adjacency[ev.id].system_count > 0 ? '⚙' : '🔗'}{adjacency[ev.id].count}
                </span>
              {/if}
              <!-- The analyst's own marks, in the accent that identifies authored data
                   everywhere else in the app, so a flag never reads as something the tool
                   concluded on its own. -->
              {#if $findings.byKey[ev.hash_normalized]}
                {@const f = $findings.byKey[ev.hash_normalized]}
                {#if f.flagged}
                  <span class="flag" title={UI.FINDING_FLAG_TITLE} aria-label={UI.FINDING_FLAG_BADGE_ARIA}>★</span>
                {/if}
                {#if f.note}
                  <span class="note" title={f.note} aria-label={UI.FINDING_NOTE_BADGE_ARIA}>✎</span>
                {/if}
                {#each f.tags || [] as t (t)}
                  <span class="tag">{t}</span>
                {/each}
              {/if}
            </span>
            <span class="prov">{ev.provider}</span>
            <span class="chan">{ev.channel}</span>
            <span class="user">{ev.user || '—'}</span>
            <span class="ts">
              {#if onTimeline(ev)}
                {fmtTime(ev.timestamp)}
              {:else}
                <!-- Undated: shown as plainly as any other row, but labelled so its absence
                     from the timeline is a stated fact rather than a mystery. -->
                <span class="notime" title={UI.NO_TIMELINE_HINT}>⊘ {UI.NO_TIMELINE}</span>
              {/if}
            </span>
          </button>
        {/snippet}
      </VirtualList>
    {/if}
  </div>

  {#if $events.loadingMore}
    <div class="loadmore">{UI.LOADING_MORE}</div>
  {/if}
</div>

<EventDetail
  event={active}
  relation={active ? adjacency[active.id] : null}
  relatedEvents={activeRelated}
  onclose={() => (active = null)}
  onadd={addToGraph}
  onshowgraph={showInGraph}
  onselectrelated={selectRelated}
/>

<style>
  /* Undated rows are NOT degraded (P23): same weight and size as any other row. The label
     states the fact; it does not dim or shrink the record. */
  .notime {
    font-family: var(--font-sans);
    color: var(--color-on-surface-variant);
    cursor: help;
    white-space: nowrap;
  }
  /* Sits between the filter bar and the column header so it never shifts the rows. */
  .busy {
    padding: 0 var(--space-5);
    margin-top: -4px;
  }
  .head,
  .erow {
    display: grid;
    grid-template-columns: 80px 1.4fr 1fr 1fr 1.4fr;
    gap: var(--space-3);
    align-items: center;
    padding: 0 var(--space-5);
    font-family: var(--font-sans);
  }
  .head {
    height: 40px;
    background: var(--color-surface-variant);
    border-bottom: 1px solid var(--color-outline);
    color: var(--color-on-surface-muted);
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .view {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .list {
    flex: 1;
    min-height: 0;
  }
  .msg {
    padding: var(--space-5);
    color: var(--color-on-surface-muted);
    font-family: var(--font-sans);
  }
  .erow {
    width: 100%;
    height: 100%;
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--color-outline);
    color: var(--color-on-surface);
    font-size: 0.85rem;
    text-align: left;
    cursor: pointer;
  }
  .erow:hover {
    background: var(--color-surface-variant);
  }
  /* Selected event and its related events (relation-aware highlighting, P14). */
  .erow.related {
    background: color-mix(in srgb, var(--color-accent) 16%, transparent);
    box-shadow: inset 3px 0 0 var(--color-accent);
  }
  .erow.selected {
    background: color-mix(in srgb, var(--color-primary) 16%, transparent);
    box-shadow: inset 3px 0 0 var(--color-primary);
  }
  .rel {
    font-size: 0.68rem;
    font-weight: 700;
    border-radius: var(--radius-sm, 4px);
    padding: 0 4px;
    line-height: 1.5;
    border: 1px solid currentColor;
  }
  /* Rule-correlated (auto) vs hand-mapped read differently on purpose — see
     relationTitle() for why the distinction matters. */
  .rel.byrule {
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 12%, transparent);
  }
  .rel.byhand {
    color: var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
  }
  .eid {
    color: var(--color-primary);
    font-weight: 700;
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .dedup {
    font-size: 0.68rem;
    font-weight: 700;
    color: var(--color-on-accent, var(--color-on-surface));
    background: var(--color-accent);
    border-radius: var(--radius-sm, 4px);
    padding: 1px 5px;
    line-height: 1.4;
  }
  /* Analyst marks (P25). Accent-coloured because that is what authored data looks like
     throughout the app; a derived relation badge stays visually separate. */
  .flag,
  .note {
    color: var(--color-accent);
    font-size: 0.8rem;
    line-height: 1;
    cursor: help;
  }
  .tag {
    font-family: var(--font-sans);
    font-size: 0.66rem;
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, transparent);
    border: 1px solid var(--color-accent);
    border-radius: 999px;
    padding: 0 6px;
    line-height: 1.5;
    max-width: 14ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .prov,
  .user {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chan {
    color: var(--color-on-surface-muted);
  }
  .ts {
    font-family: var(--font-mono);
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
  }
  .loadmore {
    flex: 0 0 auto;
    text-align: center;
    padding: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
    background: var(--color-surface);
    border-top: 1px solid var(--color-outline);
  }
</style>
