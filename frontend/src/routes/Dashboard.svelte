<script>
  // Dashboard (P5.1). Home surface: ingest an EVTX file with live progress, view
  // store stats, toggle theme, and preview ingested events. Everything flows through
  // the stores and the API wrapper — no direct binding calls, no async blocking.
  import { onMount } from 'svelte';
  import { theme } from '../stores/theme.js';
  import { route } from '../stores/router.js';
  import { ingestion, progressFraction, etaSeconds, recordsPerSecond } from '../stores/ingestion.js';
  import { events } from '../stores/events.js';
  import { findings } from '../stores/findings.js';
  import * as api from '../lib/api/index.js';
  import {
    UI,
    INGEST_STATE,
    THEMES,
    ROUTES,
    LARGE_DATASET_BYTES,
    PROTECTED_CHANNELS,
    INGEST_LIFECYCLE,
    FINDINGS,
  } from '../lib/consts/index.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Card from '../components/material/Card.svelte';
  import Button from '../components/material/Button.svelte';
  import Checkbox from '../components/material/Checkbox.svelte';
  import ProgressBar from '../components/material/ProgressBar.svelte';
  import List from '../components/material/List.svelte';
  import ListItem from '../components/material/ListItem.svelte';
  import { snackbar } from '../stores/snackbar.js';
  import { openShortcuts } from '../stores/shortcuts.js';

  let selectedFiles = $state(/** @type {string[]} */ ([]));
  let idempotent = $state(true);
  let stats = $state({ events: 0, relations: 0 });
  let totalBytes = $state(0);

  async function mergeFiles(paths) {
    const set = new Set(selectedFiles);
    for (const p of paths || []) set.add(p);
    selectedFiles = [...set].sort();
    const was = totalBytes;
    try {
      totalBytes = (await api.totalSize(selectedFiles)) || 0;
    } catch (_) {
      /* backend unavailable */
    }
    // Warn once as the selection crosses the large-dataset threshold.
    if (was <= LARGE_DATASET_BYTES && totalBytes > LARGE_DATASET_BYTES) {
      snackbar.warn(UI.WARN_LARGE_DATASET);
    }
  }

  function humanSize(bytes) {
    if (!bytes) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
    return `${(bytes / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}`;
  }
  async function pickFiles() {
    try {
      mergeFiles(await api.pickEVTXFiles());
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }
  async function pickFolder() {
    try {
      const found = (await api.pickEVTXFolder()) || [];
      mergeFiles(found);
      if (found.length === 0) snackbar.info(UI.NO_FILES_SELECTED);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }
  function baseName(p) {
    return p.split(/[\\/]/).pop();
  }

  const running = $derived($ingestion.state === INGEST_STATE.RUNNING);
  const fraction = $derived(progressFraction($ingestion.progress));
  const eta = $derived(etaSeconds($ingestion));

  // Pause/resume state comes from the backend (P8) — never inferred from progress.
  const paused = $derived($ingestion.lifecycle === INGEST_LIFECYCLE.PAUSED);
  const stopping = $derived($ingestion.lifecycle === INGEST_LIFECYCLE.STOPPING);
  const canPause = $derived($ingestion.lifecycle === INGEST_LIFECYCLE.ACTIVE);

  async function togglePause() {
    if (paused) {
      if (await ingestion.resume()) snackbar.success(UI.INGEST_RESUMED);
    } else if (await ingestion.pause()) {
      snackbar.info(UI.INGEST_PAUSED);
    }
  }

  const STATUS_LABEL = {
    [INGEST_STATE.IDLE]: UI.STATUS_IDLE,
    [INGEST_STATE.RUNNING]: UI.STATUS_RUNNING,
    [INGEST_STATE.COMPLETE]: UI.STATUS_COMPLETE,
    [INGEST_STATE.CANCELLED]: UI.STATUS_CANCELLED,
    [INGEST_STATE.ERROR]: UI.STATUS_ERROR,
  };

  async function refreshStats() {
    try {
      stats = await api.stats();
    } catch (_) {
      /* backend unavailable */
    }
  }

  onMount(() => {
    ingestion.wire();
    refreshStats();
    refreshCapture();
    findings.refreshMeta();
    findings.audit();
    // A capture may already be running (or paused) from before this view was opened.
    ingestion.syncLifecycle();
  });

  // Refresh stats + event preview whenever an ingest completes.
  $effect(() => {
    if ($ingestion.state === INGEST_STATE.COMPLETE) {
      refreshStats();
      events.load();
      // Ingestion is the one thing that turns an orphaned finding back into a live one, so
      // this is exactly when the reconciliation is worth redoing.
      findings.audit();
    }
  });

  const orphans = $derived($findings.audit ? $findings.audit.orphans || [] : []);
  // Before the audit has run there is nothing to subtract, so the sidecar total stands in.
  // It is the honest answer at that moment: not yet reconciled, not yet known to be wrong.
  const liveFindings = $derived($findings.audit ? $findings.audit.live : $findings.stats.total);

  // `starting` covers the gap between the click and the first backend event: opening a
  // large file can take a moment, and without this the button looks like it did nothing.
  let starting = $state(false);

  async function start() {
    if (selectedFiles.length === 0 || starting) return;
    starting = true;
    try {
      await ingestion.startFiles(selectedFiles, idempotent);
    } finally {
      starting = false;
    }
  }

  // --- Live system capture (P7) ---

  let liveChannels = $state(/** @type {string[]} */ ([]));
  let continuous = $state(true);
  let capture = $state({ active: false, continuous: false, channels: [], positions: {} });

  const rate = $derived(recordsPerSecond($ingestion));

  async function refreshCapture() {
    try {
      capture = (await api.captureStatus()) || capture;
    } catch (_) {
      /* backend unavailable */
    }
  }

  function toggleChannel(name) {
    liveChannels = liveChannels.includes(name)
      ? liveChannels.filter((c) => c !== name)
      : [...liveChannels, name];
  }

  // Where a channel would resume from — the whole point of the durable bookmark, so it is
  // worth showing before the user commits to a capture.
  function resumeHint(name) {
    const at = (capture.positions || {})[name] || 0;
    return at > 0 ? `${UI.LIVE_POSITION_PREFIX} ${fmt(at)}` : UI.LIVE_POSITION_NONE;
  }

  async function startCapture() {
    if (liveChannels.length === 0) {
      snackbar.warn(UI.LIVE_NO_CHANNELS);
      return;
    }
    if (await ingestion.startLive(liveChannels, { idempotent, continuous })) {
      snackbar.success(UI.LIVE_CAPTURE_STARTED);
      refreshCapture();
    }
  }

  async function stopCapture() {
    await ingestion.cancel();
    snackbar.info(UI.LIVE_CAPTURE_STOPPED);
    refreshCapture();
  }

  async function resetPositions() {
    try {
      await api.resetCapturePositions('');
      await refreshCapture();
      snackbar.success(UI.LIVE_POSITIONS_RESET);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  // Keep the capture indicator honest while a run is in flight: positions advance as the
  // pipeline persists, and the run can end on its own in drain-once mode.
  $effect(() => {
    if ($ingestion.progress.records_persisted >= 0 && running) refreshCapture();
  });

  function fmt(n) {
    return new Intl.NumberFormat().format(n || 0);
  }
  function fmtEta(sec) {
    if (sec === null || sec === undefined) return '—';
    if (sec < 60) return `${sec}s`;
    const m = Math.floor(sec / 60);
    const s = sec % 60;
    return `${m}m ${s}s`;
  }
  function fmtTime(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleString();
  }
</script>

<div class="page">
<AppBar title={UI.APP_NAME}>
  <span class="stat">{UI.LABEL_EVENTS}: <b>{fmt(stats.events)}</b></span>
  <span class="stat">{UI.LABEL_RELATIONS}: <b>{fmt(stats.relations)}</b></span>
  <!-- The analyst's own tally, kept visually apart from the two derived counts beside it
       (P25): how much of this case a person has actually judged. -->
  {#if $findings.stats.total > 0}
    <!-- Counts the findings that still resolve to an event in this case. Reporting the raw
         sidecar total would claim work the case cannot show. -->
    <span class="stat mine" title={orphans.length ? UI.FINDING_ORPHAN_HINT : UI.FINDING_HINT}>
      ★ {UI.LABEL_FINDINGS}: <b>{fmt($findings.stats.flagged)}</b> / {fmt(liveFindings)}
      {#if orphans.length}<span class="oflag">+{fmt(orphans.length)} {UI.FINDING_ORPHAN_SHORT}</span>{/if}
    </span>
  {/if}
  <Button variant="text" onclick={() => route.go(ROUTES.EVENTS)}>{UI.NAV_EVENTS}</Button>
  <Button variant="text" onclick={() => route.go(ROUTES.GRAPH)}>{UI.NAV_GRAPH}</Button>
  <Button variant="text" onclick={() => route.go(ROUTES.RULES)}>{UI.NAV_RULES}</Button>
    <Button variant="text" onclick={() => route.go(ROUTES.TIMELINE)}>{UI.NAV_TIMELINE}</Button>
  <Button variant="text" onclick={openShortcuts} title={UI.ACTION_SHORTCUTS}>?</Button>
  <Button variant="tonal" onclick={() => theme.toggle()}>
    {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
  </Button>
</AppBar>

<main>
  <section class="col">
    <Card>
      <h3>{UI.ACTION_INGEST_FILE}</h3>
      <div class="form">
        <div class="row">
          <Button variant="tonal" onclick={pickFiles} disabled={running}>{UI.ACTION_SELECT_FILES}</Button>
          <Button variant="tonal" onclick={pickFolder} disabled={running}>{UI.ACTION_SELECT_FOLDER}</Button>
          {#if selectedFiles.length > 0}
            <Button
              variant="text"
              onclick={() => {
                selectedFiles = [];
                totalBytes = 0;
              }}
              disabled={running}
            >
              {UI.ACTION_CLEAR_SELECTION}
            </Button>
          {/if}
        </div>

        {#if selectedFiles.length === 0}
          <p class="hint">{UI.NO_FILES_SELECTED}</p>
        {:else}
          <p class="hint"><b>{selectedFiles.length}</b> {UI.FILES_SELECTED_SUFFIX} · {humanSize(totalBytes)}</p>
          <div class="filelist">
            <List>
              {#each selectedFiles.slice(0, 100) as f (f)}
                <ListItem>
                  <span class="fname" title={f}>{baseName(f)}</span>
                </ListItem>
              {/each}
            </List>
          </div>
        {/if}

        <Checkbox label={UI.LABEL_IDEMPOTENT} bind:checked={idempotent} disabled={running} />
        <div class="row">
          {#if running}
            <Button variant="tonal" onclick={togglePause} disabled={stopping || (!canPause && !paused)}>
              {paused ? UI.ACTION_RESUME : UI.ACTION_PAUSE}
            </Button>
            <Button variant="outlined" onclick={() => ingestion.cancel()} disabled={stopping}>
              {UI.ACTION_CANCEL}
            </Button>
          {:else}
            <Button onclick={start} disabled={selectedFiles.length === 0 || starting}>
              {starting ? UI.INGEST_STARTING : UI.ACTION_START}
            </Button>
          {/if}
        </div>
        {#if paused}
          <p class="hint">{UI.PAUSED_HINT}</p>
        {/if}
      </div>
    </Card>

    <Card>
      <div class="livehead">
        <h3>{UI.LIVE_TITLE}</h3>
        <span class="livedot" class:on={capture.active && !paused} class:held={capture.active && paused}>
          {#if capture.active && paused}
            {UI.STATUS_PAUSED}
          {:else if capture.active}
            {UI.LIVE_CAPTURING}
          {:else}
            {UI.LIVE_IDLE}
          {/if}
        </span>
      </div>
      <div class="form">
        <p class="hint">{UI.LIVE_SUBTITLE}</p>

        <div class="channels">
          {#each PROTECTED_CHANNELS as name (name)}
            <label class="channel">
              <input
                type="checkbox"
                checked={liveChannels.includes(name)}
                disabled={running}
                onchange={() => toggleChannel(name)}
              />
              <span class="cname">{name}</span>
              <span class="cpos">{resumeHint(name)}</span>
            </label>
          {/each}
        </div>

        <Checkbox label={UI.LIVE_CONTINUOUS} bind:checked={continuous} disabled={running} />
        <p class="hint subtle">{UI.LIVE_CONTINUOUS_HINT}</p>

        <div class="row">
          {#if capture.active}
            <Button variant="tonal" onclick={togglePause} disabled={stopping || (!canPause && !paused)}>
              {paused ? UI.ACTION_RESUME : UI.ACTION_PAUSE}
            </Button>
            <Button variant="outlined" onclick={stopCapture} disabled={stopping}>
              {UI.ACTION_STOP_CAPTURE}
            </Button>
            {#if rate !== null && !paused}
              <span class="rate">{fmt(rate)} {UI.LIVE_RATE_SUFFIX}</span>
            {/if}
          {:else}
            <Button onclick={startCapture} disabled={running || liveChannels.length === 0}>
              {UI.ACTION_START_CAPTURE}
            </Button>
            <Button variant="text" onclick={resetPositions} disabled={running}>
              {UI.ACTION_RESET_POSITIONS}
            </Button>
          {/if}
        </div>
      </div>
    </Card>

    <Card>
      <div class="statusline">
        <span>
          {UI.LABEL_STATUS}:
          <b>
            {#if paused}
              {UI.STATUS_PAUSED}
            {:else if stopping}
              {UI.STATUS_STOPPING}
            {:else}
              {STATUS_LABEL[$ingestion.state]}
            {/if}
          </b>
        </span>
        {#if running && !paused}<span>{UI.LABEL_ETA}: <b>{fmtEta(eta)}</b></span>{/if}
      </div>
      <div class="bar">
        <!-- A paused run holds its bar still rather than animating an indeterminate one,
             which would read as "still working". -->
        <ProgressBar value={running && !paused && !$ingestion.progress.chunks_total ? null : fraction} />
      </div>
      <div class="metrics">
        <div><span>{UI.METRIC_CHUNKS}</span><b>{fmt($ingestion.progress.chunks_parsed)} / {fmt($ingestion.progress.chunks_total)}</b></div>
        <div><span>{UI.METRIC_READ}</span><b>{fmt($ingestion.progress.records_read)}</b></div>
        <div><span>{UI.METRIC_PERSISTED}</span><b>{fmt($ingestion.progress.records_persisted)}</b></div>
        <div><span>{UI.METRIC_DUPLICATE}</span><b>{fmt($ingestion.progress.records_duplicate)}</b></div>
        <div><span>{UI.METRIC_SKIPPED}</span><b>{fmt($ingestion.progress.records_skipped)}</b></div>
      </div>
      {#if $ingestion.finishedAt}
        <p class="finished">{UI.LABEL_FINISHED} {fmtTime($ingestion.finishedAt)}</p>
      {/if}
    </Card>
  </section>

  <section class="col">
    <!-- Findings that no longer resolve to an event in this case. Reported rather than
         quietly folded into the totals, and never auto-deleted: re-ingesting the missing
         source brings the events back and these reattach. -->
    {#if orphans.length}
      <Card>
        <div class="events-head">
          <h3 class="mine">{UI.FINDING_ORPHAN_TITLE}</h3>
          <span class="ocount">{orphans.length}</span>
        </div>
        <p class="ohint">
          {$findings.audit && $findings.audit.stale ? UI.FINDING_ORPHAN_STALE : UI.FINDING_ORPHAN_HINT}
        </p>
        <ul class="olist">
          {#each orphans.slice(0, FINDINGS.ORPHAN_PREVIEW) as o (o.key)}
            <li>
              <span class="odesc">{o.descriptor || o.key}</span>
              {#if o.note}<span class="onote">{o.note}</span>{/if}
            </li>
          {/each}
        </ul>
        {#if orphans.length > FINDINGS.ORPHAN_PREVIEW}
          <p class="ohint">{UI.FINDING_ORPHAN_MORE} {orphans.length - FINDINGS.ORPHAN_PREVIEW}</p>
        {/if}
      </Card>
    {/if}

    <Card>
      <div class="events-head">
        <h3>{UI.LABEL_EVENTS}</h3>
        <Button variant="text" onclick={() => events.load()}>{UI.ACTION_RETRY}</Button>
      </div>
      {#if $events.list.length === 0}
        <p class="empty">{UI.EMPTY_EVENTS}</p>
      {:else}
        <List>
          {#each $events.list.slice(0, 50) as ev (ev.id)}
            <ListItem>
              <b class="eid">{ev.event_id}</b>
              {#if ev.deduplication_count > 1}
                <span class="dedup" title={UI.BADGE_DEDUP_TITLE}>×{ev.deduplication_count}</span>
              {/if}
              <span class="prov">{ev.provider}</span>
              <span class="chan">{ev.channel}</span>
              <span class="ts">{fmtTime(new Date(ev.timestamp).getTime())}</span>
            </ListItem>
          {/each}
        </List>
      {/if}
    </Card>
  </section>
</main>
</div>

<style>
  .page {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  main {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-5);
    padding: var(--space-5);
    background: var(--color-background);
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    align-items: start;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    min-width: 0;
  }
  h3 {
    font-family: var(--font-sans);
    font-weight: 800;
    margin: 0 0 var(--space-4);
    color: var(--color-on-surface);
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .row {
    display: flex;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .hint {
    font-family: var(--font-sans);
    font-size: 0.82rem;
    color: var(--color-on-surface-muted);
    margin: 0;
  }
  .filelist {
    max-height: 180px;
    overflow-y: auto;
  }
  .fname {
    font-family: var(--font-mono);
    font-size: 0.8rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* --- Live capture (P7) --- */
  .livehead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .livedot {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    color: var(--color-on-surface-muted);
    white-space: nowrap;
  }
  .livedot::before {
    content: '';
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-outline);
    transition: background var(--motion-medium) var(--motion-ease);
  }
  /* A capture in progress pulses, so "is it still running?" is answerable at a glance. */
  .livedot.on {
    color: var(--color-primary);
  }
  .livedot.on::before {
    background: var(--color-primary);
    animation: pulse 1.6s ease-in-out infinite;
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--color-primary) 55%, transparent);
    }
    50% {
      opacity: 0.65;
      box-shadow: 0 0 0 5px transparent;
    }
  }
  /* Paused reads as "held", not "stopped": solid amber, deliberately not pulsing. */
  .livedot.held {
    color: var(--color-accent);
  }
  .livedot.held::before {
    background: var(--color-accent);
  }
  @media (prefers-reduced-motion: reduce) {
    .livedot.on::before {
      animation: none;
    }
  }
  .channels {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .channel {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface);
    cursor: pointer;
  }
  .cpos {
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    white-space: nowrap;
  }
  .subtle {
    margin-top: calc(-1 * var(--space-2));
  }
  .rate {
    display: inline-flex;
    align-items: center;
    font-family: var(--font-mono);
    font-size: 0.8rem;
    color: var(--color-primary);
  }

  .statusline {
    display: flex;
    justify-content: space-between;
    font-family: var(--font-sans);
    font-size: 0.9rem;
    color: var(--color-on-surface);
    margin-bottom: var(--space-3);
  }
  .bar {
    margin-bottom: var(--space-4);
  }
  .metrics {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(90px, 1fr));
    gap: var(--space-3);
  }
  .metrics div {
    display: flex;
    flex-direction: column;
    font-family: var(--font-sans);
  }
  .metrics span {
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .metrics b {
    font-size: 1rem;
    color: var(--color-on-surface);
  }
  .finished {
    font-family: var(--font-sans);
    font-size: 0.8rem;
    color: var(--color-on-surface-muted);
    margin: var(--space-4) 0 0;
  }
  .stat {
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
  }
  .stat.mine,
  .stat.mine b {
    color: var(--color-accent);
  }
  /* Orphan reporting: stated plainly next to the tally it would otherwise inflate. */
  .oflag {
    color: var(--color-on-surface-muted);
    font-size: 0.75rem;
  }
  h3.mine {
    color: var(--color-accent);
  }
  .ocount {
    font-family: var(--font-sans);
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, transparent);
    border: 1px solid var(--color-accent);
    border-radius: 999px;
    padding: 0 var(--space-3);
  }
  .ohint {
    margin: 0 0 var(--space-3);
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
    line-height: 1.5;
  }
  .olist {
    list-style: none;
    margin: 0 0 var(--space-3);
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .olist li {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-left: 2px solid var(--color-accent);
    padding-left: var(--space-3);
  }
  .odesc {
    font-family: var(--font-mono);
    font-size: 0.74rem;
    color: var(--color-on-surface);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .onote {
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
  }
  .stat b {
    color: var(--color-on-surface);
  }
  .events-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .empty {
    font-family: var(--font-sans);
    color: var(--color-on-surface-muted);
    font-size: 0.9rem;
  }
  .eid {
    color: var(--color-primary);
    min-width: 56px;
  }
  .dedup {
    font-size: 0.68rem;
    font-weight: 700;
    color: var(--color-on-accent, var(--color-on-surface));
    background: var(--color-accent);
    border-radius: var(--radius-sm, 4px);
    padding: 1px 5px;
  }
  .prov {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-on-surface);
  }
  .chan {
    color: var(--color-on-surface-muted);
  }
  .ts {
    color: var(--color-on-surface-muted);
    font-size: 0.8rem;
  }
</style>
