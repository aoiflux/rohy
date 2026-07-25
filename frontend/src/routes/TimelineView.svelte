<script>
  // Timeline page (P24). Chronological shape of the filtered event set, with zoom, pan and
  // range selection, plus the list of events inside the visible window.
  //
  // Division of labour: the BACKEND returns a density histogram (counts, not events), so
  // the picture is cheap at any dataset size; individual events are fetched only for the
  // window actually in view. Undated events cannot appear here at all — that is stated on
  // the page with a route to them, rather than being silently absent (P23).
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { theme } from '../stores/theme.js';
  import { route } from '../stores/router.js';
  import { events } from '../stores/events.js';
  import { graph } from '../stores/graph.js';
  import { findings } from '../stores/findings.js';
  import { prefs } from '../stores/prefs.js';
  import { snackbar } from '../stores/snackbar.js';
  import * as api from '../lib/api/index.js';
  import { formToFilter } from '../lib/filter.js';
  import { isTypingTarget } from '../lib/shortcuts.js';
  import {
    UI,
    ROUTES,
    THEMES,
    TIMELINE,
    TIMELINE_GROUP,
    TIMELINE_MODE,
    FINDING_FILTERS,
    UNDATED,
    EVENTS_LIST,
  } from '../lib/consts/index.js';
  import { zoomAround } from '../lib/timeline.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import Select from '../components/material/Select.svelte';
  import ProgressBar from '../components/material/ProgressBar.svelte';
  import TimelineCanvas from '../components/timeline/TimelineCanvas.svelte';
  import TimelineOverview from '../components/timeline/TimelineOverview.svelte';

  let summary = $state(/** @type {any} */ (null));
  let loading = $state(false);
  let windowEvents = $state(/** @type {any[]} */ ([]));
  let hover = $state(/** @type {any} */ (null));
  let playhead = $state(/** @type {number|null} */ (null));
  let selectedId = $state(/** @type {number|null} */ (null));
  let adjacency = $state(/** @type {Record<number, any>} */ ({}));

  let groupBy = $state(prefs.current().timelineGroupBy || TIMELINE_GROUP.NONE);
  const groupOptions = [
    { value: TIMELINE_GROUP.NONE, label: UI.TIMELINE_GROUP_NONE },
    { value: TIMELINE_GROUP.PROVIDER, label: UI.LABEL_PROVIDER },
    { value: TIMELINE_GROUP.CHANNEL, label: UI.LABEL_CHANNEL },
    { value: TIMELINE_GROUP.USER, label: UI.LABEL_USER },
    { value: TIMELINE_GROUP.COMPUTER, label: UI.LABEL_COMPUTER },
    { value: TIMELINE_GROUP.GRAPH, label: UI.TIMELINE_GROUP_GRAPH },
  ];

  // Graph lanes come back as graph IDs (the persistence layer has no notion of graph names),
  // so they are resolved here against the registry the graph store already holds.
  const displayLanes = $derived.by(() => {
    const lanes = (summary && summary.lanes) || [];
    if (groupBy !== TIMELINE_GROUP.GRAPH) return lanes;
    const names = new Map(($graph.graphs || []).map((g) => [String(g.id), g.name]));
    return lanes.map((l) => ({ ...l, key: names.get(l.key) || l.key }));
  });

  // View state persists across navigation and restart, like the search panel does.
  let view = $state(prefs.current().timelineView || { start: 0, end: 1 });

  // What a plain drag on the chart does. Persisted, so an analyst who works in ranges is not
  // reset to pan on every visit.
  let dragMode = $state(prefs.current().timelineDragMode || TIMELINE_MODE.PAN);
  function setDragMode(m) {
    dragMode = m;
    prefs.set({ timelineDragMode: m });
  }

  // Zoom from the toolbar buttons, anchored at the centre of the current window so the
  // midpoint stays put — the same model the wheel uses, just without a cursor position.
  function zoom(factor) {
    const mid = (view.start + view.end) / 2;
    setView(zoomAround(view, factor, mid, TIMELINE.MIN_VIEW_SPAN));
  }
  const atFullExtent = $derived(view.start <= 0 && view.end >= 1);

  const extent = $derived.by(() => {
    if (!summary || !summary.from || !summary.to) return null;
    const from = new Date(summary.from).getTime();
    const to = new Date(summary.to).getTime();
    return Number.isFinite(from) && Number.isFinite(to) && to >= from ? { from, to } : null;
  });

  /** Fraction of the full extent → absolute time. */
  function fracToTime(f) {
    if (!extent) return null;
    return new Date(extent.from + (extent.to - extent.from) * f);
  }

  const windowRange = $derived.by(() => {
    if (!extent) return null;
    return { from: fracToTime(view.start), to: fracToTime(view.end) };
  });

  function fmt(ts) {
    if (!ts) return '—';
    return new Date(ts).toLocaleString();
  }
  function fmtNum(n) {
    return new Intl.NumberFormat().format(n || 0);
  }

  // The timeline shows only DATED events, so its query always excludes undated ones — this
  // is the surface where that exclusion is a statement of fact rather than hidden data.
  function timelineFilter(extra = {}) {
    return { ...formToFilter(prefs.current().filterForm), undated: UNDATED.EXCLUDE, ...extra };
  }

  async function loadSummary() {
    loading = true;
    try {
      summary = await api.timeline(timelineFilter(), TIMELINE.BUCKETS, groupBy);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
      summary = null;
    } finally {
      loading = false;
    }
  }

  function setGroupBy(value) {
    groupBy = value;
    prefs.set({ timelineGroupBy: value });
    loadSummary();
  }

  /** Absolute time → fraction of the full extent, or null when out of range. */
  function timeToFrac(ts) {
    if (!extent || !ts) return null;
    const t = new Date(ts).getTime();
    if (!Number.isFinite(t)) return null;
    const denom = extent.to - extent.from;
    return denom > 0 ? (t - extent.from) / denom : 0;
  }

  // Correlation highlighting: the selected event plus its related events, marked across
  // every lane. This is the one place individual events ARE drawn — the set is a single
  // event's neighbours, so it stays small and does not reintroduce the per-event cost the
  // histogram exists to avoid.
  //
  // The related events are FETCHED by id rather than looked up in the loaded window: a
  // correlated event is very often outside the current view (that is usually the
  // interesting part), and marking only the ones that happened to be paged in would make
  // the feature look broken exactly when it matters.
  let markEvents = $state(/** @type {any[]} */ ([]));

  $effect(() => {
    const id = selectedId;
    if (id === null) {
      markEvents = [];
      return;
    }
    const adj = adjacency[id];
    const ids = [id, ...((adj && adj.related_ids) || [])];
    api
      .getEvents(ids)
      .then((evs) => (markEvents = evs || []))
      .catch(() => (markEvents = []));
  });

  // Flagged events, marked across the whole extent (P25). This is the analyst's own map of
  // the case: where the things they decided mattered actually fall in time. It is fetched
  // once for the extent rather than per view window, so panning does not re-query — the set
  // is bounded by how much a person has flagged, not by case size.
  let flaggedEvents = $state(/** @type {any[]} */ ([]));

  $effect(() => {
    // Depend on the case's flag count so the marks refresh when a flag is added or cleared
    // anywhere in the app.
    void $findings.stats.flagged;
    api
      .queryEvents({
        ...timelineFilter(),
        finding_state: FINDING_FILTERS.FLAGGED,
        offset: 0,
        limit: TIMELINE.FLAG_MARK_LIMIT,
      })
      .then((evs) => (flaggedEvents = evs || []))
      .catch(() => (flaggedEvents = []));
  });

  const marks = $derived.by(() => {
    const out = [];
    // Flags are drawn whether or not anything is selected: they are a property of the case,
    // not of the current selection.
    for (const ev of flaggedEvents) {
      const f = timeToFrac(ev.timestamp);
      if (f === null) continue; // a flagged undated event has no position on a timeline
      out.push({ frac: f, kind: 'flagged' });
    }
    if (selectedId === null) return out;
    for (const ev of markEvents) {
      const f = timeToFrac(ev.timestamp);
      if (f === null) continue; // an undated neighbour has no position on a timeline
      out.push({ frac: f, kind: ev.id === selectedId ? 'selected' : 'related' });
    }
    return out;
  });

  // Flagged events that carry no timestamp cannot be marked. Counted so the page can SAY so
  // rather than leaving the analyst to wonder why a flag they set is not on the timeline.
  const flaggedUndated = $derived(flaggedEvents.filter((e) => timeToFrac(e.timestamp) === null).length);

  // Adjacency for the events in view, so correlation marks are available without a
  // per-click round trip.
  $effect(() => {
    const ids = windowEvents.map((e) => e.id);
    if (ids.length === 0) {
      adjacency = {};
      return;
    }
    api
      .relationsAdjacency(ids)
      .then((a) => (adjacency = a || {}))
      .catch(() => (adjacency = {}));
  });

  function selectEvent(ev) {
    selectedId = ev.id;
    const f = timeToFrac(ev.timestamp);
    if (f !== null) playhead = f;
  }

  // Graph → timeline: when another view focuses an event, select and reveal it here, so
  // selection flows BOTH ways rather than only outward (P24.4).
  let lastFocusNonce = 0;
  $effect(() => {
    const f = $graph.focus;
    if (!f || f.id === null || f.nonce === lastFocusNonce) return;
    lastFocusNonce = f.nonce;
    revealEvent(f.id);
  });

  async function revealEvent(id) {
    selectedId = id;
    try {
      const ev = await api.getEvent(id);
      const frac = timeToFrac(ev && ev.timestamp);
      if (frac === null) return; // undated events have no position here
      playhead = frac;
      // Centre the window on it, keeping the current zoom.
      const half = Math.max(view.end - view.start, TIMELINE.MIN_VIEW_SPAN) / 2;
      const start = Math.min(Math.max(frac - half, 0), 1 - half * 2);
      setView({ start, end: start + half * 2 });
    } catch (_) {
      /* event may have been deleted */
    }
  }

  // Rows to highlight as correlated with the selection.
  const relatedSet = $derived.by(() => {
    const adj = selectedId !== null ? adjacency[selectedId] : null;
    return new Set((adj && adj.related_ids) || []);
  });

  const hoverBucket = $derived.by(() => {
    if (!hover || !summary || !summary.buckets?.length) return null;
    const i = Math.round(hover.frac * (summary.buckets.length - 1));
    return summary.buckets[Math.min(Math.max(i, 0), summary.buckets.length - 1)];
  });

  // Events for the visible window only. Debounced so dragging does not fire a query per
  // frame; bounded so a wide window cannot pull the whole case into memory.
  let windowTimer;
  $effect(() => {
    const r = windowRange;
    clearTimeout(windowTimer);
    if (!r) {
      windowEvents = [];
      return;
    }
    windowTimer = setTimeout(async () => {
      try {
        windowEvents =
          (await api.queryEvents(
            timelineFilter({
              time_from: r.from.toISOString(),
              time_to: r.to.toISOString(),
              offset: 0,
              limit: EVENTS_LIST.PAGE_LIMIT,
            }),
          )) || [];
      } catch (_) {
        windowEvents = [];
      }
    }, 180);
    return () => clearTimeout(windowTimer);
  });

  function setView(next) {
    view = next;
    prefs.set({ timelineView: next });
  }

  function resetView() {
    setView({ start: 0, end: 1 });
  }

  // A swept range becomes a real filter on the shared events model, so the timeline and the
  // events page always mean the same thing by "filtered" (R-TL4).
  function applyRange(r) {
    const from = fracToTime(r.start);
    const to = fracToTime(r.end);
    if (!from || !to) return;
    events.setFilter({ time_from: from.toISOString(), time_to: to.toISOString(), offset: 0 });
    events.load();
    snackbar.success(UI.TIMELINE_RANGE_APPLIED, {
      action: { label: UI.NAV_EVENTS, run: () => route.go(ROUTES.EVENTS) },
    });
    setView({ start: r.start, end: r.end });
  }

  // Selecting an event here focuses it on the graph canvas — one shared selection concept
  // rather than two that drift apart.
  function showOnGraph(ev) {
    graph.focusEvent(ev.id);
    route.go(ROUTES.GRAPH);
  }

  // Keyboard scrubbing: the playhead is the timeline's cursor, so it has to be reachable
  // without a pointer. Arrows step, Shift+arrows take a coarse step, Home/End jump to the
  // ends of the visible window.
  function onkeydown(e) {
    if (isTypingTarget(e.target)) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (!summary || summary.dated === 0) return;

    const span = Math.max(view.end - view.start, TIMELINE.MIN_VIEW_SPAN);
    const step = span * (e.shiftKey ? TIMELINE.KEY_STEP_COARSE : TIMELINE.KEY_STEP);
    const at = playhead === null ? view.start + span / 2 : playhead;

    switch (e.key) {
      case 'ArrowLeft':
        e.preventDefault();
        playhead = Math.max(at - step, 0);
        break;
      case 'ArrowRight':
        e.preventDefault();
        playhead = Math.min(at + step, 1);
        break;
      case 'Home':
        e.preventDefault();
        playhead = view.start;
        break;
      case 'End':
        e.preventDefault();
        playhead = view.end;
        break;
      case 'Escape':
        if (playhead !== null) {
          e.preventDefault();
          playhead = null;
        }
        break;
      default:
        return;
    }
    // Keep the playhead in view: scrubbing past the edge should pan, not lose the cursor.
    if (playhead !== null && (playhead < view.start || playhead > view.end)) {
      const start = Math.min(Math.max(playhead - span / 2, 0), 1 - span);
      setView({ start, end: start + span });
    }
  }

  function fmtPlayhead() {
    if (playhead === null) return '';
    const t = fracToTime(playhead);
    return t ? t.toLocaleString() : '';
  }

  onMount(() => {
    loadSummary();
    // Needed to name the graph lanes; cheap and idempotent.
    graph.loadGraphs();
  });
</script>

<svelte:window {onkeydown} />

<div class="tl">
  <AppBar title={UI.NAV_TIMELINE}>
    {#if summary}
      <span class="count">
        {fmtNum(summary.dated)}
        {UI.TIMELINE_DATED}
      </span>
    {/if}
    <Button variant="text" onclick={() => route.go(ROUTES.DASHBOARD)}>{UI.NAV_DASHBOARD}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.EVENTS)}>{UI.NAV_EVENTS}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.GRAPH)}>{UI.NAV_GRAPH}</Button>
    <Button variant="tonal" onclick={() => theme.toggle()}>
      {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
    </Button>
  </AppBar>

  {#if summary && summary.dated > 0}
    <!-- Timeline-specific controls, grouped together and next to the chart they act on —
         rather than scattered among the app bar's navigation. -->
    <div class="toolbar">
      <div class="mode" role="group" aria-label={UI.TIMELINE_MODE_PAN + ' / ' + UI.TIMELINE_MODE_SELECT}>
        <button
          class="seg"
          class:on={dragMode === TIMELINE_MODE.PAN}
          type="button"
          title={UI.TIMELINE_MODE_PAN_HINT}
          aria-pressed={dragMode === TIMELINE_MODE.PAN}
          onclick={() => setDragMode(TIMELINE_MODE.PAN)}
        >✋ {UI.TIMELINE_MODE_PAN}</button>
        <button
          class="seg"
          class:on={dragMode === TIMELINE_MODE.SELECT}
          type="button"
          title={UI.TIMELINE_MODE_SELECT_HINT}
          aria-pressed={dragMode === TIMELINE_MODE.SELECT}
          onclick={() => setDragMode(TIMELINE_MODE.SELECT)}
        >⬚ {UI.TIMELINE_MODE_SELECT}</button>
      </div>

      <div class="zoom" role="group" aria-label={UI.TIMELINE_ZOOM_LABEL}>
        <button class="zbtn" type="button" title={UI.TIMELINE_ZOOM_OUT} aria-label={UI.TIMELINE_ZOOM_OUT}
          onclick={() => zoom(1 + TIMELINE.ZOOM_BUTTON_STEP)} disabled={atFullExtent}>−</button>
        <button class="zbtn" type="button" title={UI.TIMELINE_ZOOM_IN} aria-label={UI.TIMELINE_ZOOM_IN}
          onclick={() => zoom(1 - TIMELINE.ZOOM_BUTTON_STEP)}>+</button>
        <button class="zbtn wide" type="button" onclick={resetView} disabled={atFullExtent}>{UI.ACTION_ZOOM_RESET}</button>
      </div>

      <Select compact label={UI.TIMELINE_GROUP_BY} options={groupOptions} value={groupBy} onchange={setGroupBy} />

      <!-- The gesture legend changes with the mode, so the modifier that is NOT the default
           is always spelled out rather than left to be discovered. -->
      <span class="legend">
        {dragMode === TIMELINE_MODE.SELECT ? UI.TIMELINE_SELECT_LEGEND : UI.TIMELINE_PAN_LEGEND}
      </span>

      <Button variant="text" onclick={loadSummary}>{UI.ACTION_RETRY}</Button>
    </div>
  {/if}

  {#if loading}
    <div class="busy"><ProgressBar /></div>
  {/if}

  {#if summary && summary.undated > 0}
    <!-- Stated, not hidden: the timeline cannot place these, and says where to find them. -->
    <div class="notice">
      <span><b>{fmtNum(summary.undated)}</b> {UI.TIMELINE_UNDATED_EXCLUDED}</span>
      <Button variant="text" onclick={() => route.go(ROUTES.EVENTS)}>{UI.TIMELINE_SEE_EVENTS}</Button>
    </div>
  {/if}

  <!-- A flag the analyst set on an undated event cannot be placed here. Saying so is the
       same honesty the undated notice above applies to the events themselves — otherwise a
       missing mark reads as the flag having been lost. -->
  {#if flaggedUndated > 0}
    <div class="notice mine">
      <span><b>{fmtNum(flaggedUndated)}</b> {UI.TIMELINE_FLAGGED_UNDATED}</span>
      <Button variant="text" onclick={() => route.go(ROUTES.EVENTS)}>{UI.TIMELINE_SEE_EVENTS}</Button>
    </div>
  {/if}

  {#if !summary || summary.dated === 0}
    <p class="empty">{UI.TIMELINE_EMPTY}</p>
  {:else}
    <div class="chart">
      {#if displayLanes.length > 0}
        <!-- Lane labels live in a DOM gutter beside the plot, not painted over the bars.
             Long identifiers (SIDs) are truncated with the full value on hover, and each
             lane's total sits in a fixed column — so the labels are readable and volume is
             legible, which drawing them on the canvas could not achieve. The gutter reserves
             the plot's height minus the axis strip, and each lane row flexes evenly to match
             the canvas's own per-lane division. -->
        <div class="lanegutter" style="bottom: {TIMELINE.AXIS_H}px; width: {TIMELINE.LANE_LABEL_W}px" aria-hidden="true">
          {#each displayLanes as lane (lane.key)}
            <div class="lanelabel" title={lane.key}>
              <span class="lname">{lane.key}</span>
              <span class="lcount">{fmtNum(lane.total)}</span>
            </div>
          {/each}
        </div>
      {/if}
      <div class="plot" style="padding-left: {displayLanes.length > 0 ? TIMELINE.LANE_LABEL_W : 0}px">
        <TimelineCanvas
          buckets={summary.buckets}
          lanes={displayLanes}
          {view}
          {marks}
          {playhead}
          {dragMode}
          onViewChange={setView}
          onRangeSelect={applyRange}
          onHover={(h) => (hover = h)}
          onPlayheadMove={(f) => (playhead = f)}
        />
      </div>
      {#if hover && hoverBucket}
        <!-- Follows the cursor, offset so it never sits under the pointer itself. hover.x is
             relative to the canvas, which is inset by the lane gutter, so the gutter width is
             added back to place the tip correctly within the chart. -->
        <div
          class="tip"
          style="left: {(displayLanes.length > 0 ? TIMELINE.LANE_LABEL_W : 0) + hover.x + 12}px; top: {Math.max(hover.y - 34, 4)}px"
        >
          <b>{hoverBucket.count}</b>
          {hoverBucket.count === 1 ? UI.RELATION_ONE_EVENT : UI.RELATION_MANY_EVENTS}
          <span class="tiptime">{fmt(hoverBucket.start)}</span>
        </div>
      {/if}
    </div>

    <!-- Overview: the full extent with the current window drawn over it. Only worth showing
         once zoomed in — at full extent the window is the whole strip and it says nothing. -->
    {#if !atFullExtent}
      <!-- Inset by the lane gutter so the overview's extent lines up under the plot, not the
           labels — the window frame then sits directly below the part of the chart it maps. -->
      <div class="overview" style="height: {TIMELINE.OVERVIEW_H}px; padding-left: {displayLanes.length > 0 ? TIMELINE.LANE_LABEL_W : 0}px">
        <TimelineOverview buckets={summary.buckets} {view} onViewChange={setView} />
      </div>
    {/if}

    <div class="axis" style="padding-left: calc(var(--space-5) + {displayLanes.length > 0 ? TIMELINE.LANE_LABEL_W : 0}px)">
      <span>{fmt(windowRange?.from)}</span>
      {#if playhead !== null}
        <span class="playhead" title={UI.TIMELINE_PLAYHEAD_HINT}>
          ▲ {fmtPlayhead()}
          <button class="clear" type="button" onclick={() => (playhead = null)}>×</button>
        </span>
      {:else}
        <span class="hint">{UI.TIMELINE_HINT}</span>
      {/if}
      <span>{fmt(windowRange?.to)}</span>
    </div>

    <div class="window">
      <div class="wtitle">
        <h3>{UI.TIMELINE_IN_VIEW}</h3>
        <span class="wcount">{fmtNum(windowEvents.length)}</span>
      </div>
      {#if windowEvents.length === 0}
        <p class="empty small">{UI.TIMELINE_WINDOW_EMPTY}</p>
      {:else}
        <ul class="rows">
          {#each windowEvents as ev (ev.id)}
            <li>
              <button
                class="row"
                class:selected={ev.id === selectedId}
                class:related={relatedSet.has(ev.id)}
                type="button"
                onclick={() => selectEvent(ev)}
                ondblclick={() => showOnGraph(ev)}
                title={UI.TIMELINE_ROW_HINT}
              >
                <span class="ts">{fmt(ev.timestamp)}</span>
                <b class="eid">{ev.event_id}</b>
                <span class="prov">{ev.provider}</span>
                <span class="chan">{ev.channel}</span>
                {#if adjacency[ev.id]}
                  <span class="rel">🔗{adjacency[ev.id].count}</span>
                {:else}
                  <span></span>
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tl {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .count {
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-variant);
    margin-right: var(--space-3);
  }
  .busy {
    padding: 0 var(--space-5);
  }
  /* Timeline-specific controls, grouped on their own strip beneath the app bar. */
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    flex-wrap: wrap;
    padding: var(--space-2) var(--space-5);
    border-bottom: 1px solid var(--color-outline);
    background: var(--color-surface);
  }
  /* Pan/Select segmented toggle. */
  .mode {
    display: inline-flex;
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .seg {
    border: none;
    background: var(--color-surface);
    color: var(--color-on-surface-variant);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    padding: var(--space-1) var(--space-3);
    cursor: pointer;
    transition: background var(--motion-fast) var(--motion-ease), color var(--motion-fast) var(--motion-ease);
  }
  .seg + .seg {
    border-left: 1px solid var(--color-outline);
  }
  .seg.on {
    background: var(--color-primary);
    color: var(--color-on-primary);
  }
  .seg:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  .zoom {
    display: inline-flex;
    gap: var(--space-1);
  }
  .zbtn {
    min-width: 30px;
    height: 30px;
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-on-surface);
    font-size: 1rem;
    line-height: 1;
    cursor: pointer;
    transition: background var(--motion-fast) var(--motion-ease);
  }
  .zbtn.wide {
    width: auto;
    padding: 0 var(--space-3);
    font-size: 0.8rem;
    font-family: var(--font-sans);
  }
  .zbtn:hover:not(:disabled) {
    background: var(--color-surface-variant);
  }
  .zbtn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .zbtn:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .legend {
    font-family: var(--font-sans);
    font-size: 0.75rem;
    color: var(--color-on-surface-muted);
    margin-left: auto;
  }
  .overview {
    flex: 0 0 auto;
  }
  .notice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-5);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    color: var(--color-on-surface-variant);
    background: var(--color-surface-variant);
    border-bottom: 1px solid var(--color-outline);
  }
  /* A notice about the analyst's own marks, in the accent that identifies authored data. */
  .notice.mine {
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 8%, transparent);
  }
  .chart {
    position: relative;
    /* Lanes need room; grouped views get a taller chart than the plain histogram. */
    flex: 0 0 clamp(160px, 34vh, 380px);
    min-height: 0;
    border-bottom: 1px solid var(--color-outline);
  }
  /* Holds the canvas; padding-left (set inline in grouped mode) reserves the lane gutter, so
     the canvas measures its own width as the plot area alone and needs no coordinate change. */
  .plot {
    width: 100%;
    height: 100%;
  }
  /* Lane label gutter: overlays the reserved left strip, top-aligned, ending above the axis
     so it lines up with the plot rows. Each row flexes evenly to match the canvas's own
     plotH / laneCount division. */
  .lanegutter {
    position: absolute;
    top: 0;
    left: 0;
    /* width and bottom are set inline from the LANE_LABEL_W / AXIS_H consts. */
    display: flex;
    flex-direction: column;
    pointer-events: none;
    z-index: 2;
  }
  .lanelabel {
    flex: 1 1 0;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: 0 var(--space-2) 0 var(--space-3);
    border-bottom: 1px solid color-mix(in srgb, var(--color-outline) 40%, transparent);
    overflow: hidden;
  }
  .lname {
    /* min-width:0 lets a flex child shrink below its content so the ellipsis engages;
       without it a long SID would force the gutter wider instead of truncating. */
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-variant);
  }
  .lcount {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 0.68rem;
    color: var(--color-on-surface-muted);
  }
  .tip {
    position: absolute;
    pointer-events: none;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    box-shadow: var(--elevation-2);
    padding: var(--space-1) var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface);
    white-space: nowrap;
    z-index: 5;
  }
  .tiptime {
    display: block;
    font-family: var(--font-mono);
    font-size: 0.66rem;
    color: var(--color-on-surface-variant);
  }
  .playhead {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-mono);
    color: var(--color-accent);
    font-weight: 700;
  }
  .clear {
    background: none;
    border: none;
    color: inherit;
    cursor: pointer;
    font-size: 0.9rem;
    line-height: 1;
    padding: 0 2px;
  }
  .axis {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-5);
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--color-on-surface-variant);
    border-bottom: 1px solid var(--color-outline);
  }
  .hint {
    font-family: var(--font-sans);
    color: var(--color-on-surface-muted);
  }
  .window {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-4) var(--space-5);
  }
  .wtitle {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: var(--space-3);
  }
  .wtitle h3 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.78rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-variant);
  }
  .wcount {
    font-family: var(--font-mono);
    font-size: 0.78rem;
    color: var(--color-on-surface-variant);
  }
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .row {
    display: grid;
    grid-template-columns: 190px 70px 1.4fr 1fr 52px;
    gap: var(--space-3);
    align-items: center;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    border-bottom: 1px solid var(--color-outline);
    padding: var(--space-2) var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.82rem;
    color: var(--color-on-surface);
    cursor: pointer;
  }
  .row:hover {
    background: var(--color-surface-variant);
  }
  .row:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  /* Selection and its correlations, matching the marks drawn on the canvas above. */
  .row.selected {
    background: color-mix(in srgb, var(--color-accent) 18%, transparent);
  }
  .row.related {
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
  }
  .rel {
    font-size: 0.68rem;
    font-weight: 700;
    color: var(--color-primary);
    text-align: right;
  }
  .ts {
    font-family: var(--font-mono);
    font-size: 0.74rem;
    color: var(--color-on-surface-variant);
  }
  .eid {
    color: var(--color-primary);
  }
  .prov,
  .chan {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty {
    padding: var(--space-6);
    text-align: center;
    font-family: var(--font-sans);
    color: var(--color-on-surface-muted);
  }
  .empty.small {
    padding: var(--space-4);
    font-size: 0.85rem;
  }
</style>
