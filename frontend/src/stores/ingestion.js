// Ingestion store (P5.2 + P2-L.5 UI feedback). Subscribes once to the backend
// ingestion event channels and reduces them into a single reactive state: lifecycle
// status, live progress, and last error. Also exposes start/cancel actions and pure
// helpers for percent/ETA so the async pipeline never blocks the UI.
import { writable } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { CHANNELS, INGEST_STATE, INGEST_LIFECYCLE, SOURCES, UI } from '../lib/consts/index.js';
import { snackbar } from './snackbar.js';

const EMPTY_PROGRESS = Object.freeze({
  chunks_parsed: 0,
  chunks_total: 0,
  records_read: 0,
  records_persisted: 0,
  records_duplicate: 0,
  records_skipped: 0,
  last_record_id: 0,
  records_undated: 0,
});

// EMPTY_TOTALS accumulates across the files of one request. Per-file progress resets on
// every Started, so without this a folder ingest would keep re-displaying the current
// file's counters as if they were the job's.
const EMPTY_TOTALS = Object.freeze({
  records_read: 0,
  records_persisted: 0,
  records_duplicate: 0,
  records_skipped: 0,
  records_undated: 0,
});

/** Adds one file's finished progress into the request's running totals. */
function addTotals(totals, p) {
  const base = totals || EMPTY_TOTALS;
  if (!p) return { ...base };
  return {
    records_read: base.records_read + (p.records_read || 0),
    records_persisted: base.records_persisted + (p.records_persisted || 0),
    records_duplicate: base.records_duplicate + (p.records_duplicate || 0),
    records_skipped: base.records_skipped + (p.records_skipped || 0),
    records_undated: base.records_undated + (p.records_undated || 0),
  };
}

function initialState() {
  return {
    state: INGEST_STATE.IDLE,
    // lifecycle is the BACKEND's state (idle/active/paused/stopping). It is reported, not
    // inferred: pause/resume is owned by the pipeline, so the UI must not guess (P8).
    lifecycle: INGEST_LIFECYCLE.IDLE,
    progress: { ...EMPTY_PROGRESS },
    path: '',
    // Position of the file being worked on within the whole request (1-based), and how many
    // files the request covers. Both 0 for a live capture, which is not a file job.
    fileIndex: 0,
    fileTotal: 0,
    // Totals for files already finished in this request; the file in flight is not counted
    // here until it completes, so `totals + progress` is always the whole job without
    // double counting.
    doneTotals: { ...EMPTY_TOTALS },
    startedAt: 0,
    finishedAt: 0,
    lastError: null,
  };
}

function create() {
  const store = writable(initialState());
  const { subscribe, set, update } = store;
  let wired = false;

  // wire attaches the backend event listeners exactly once.
  function wire() {
    if (wired) return;
    wired = true;
    api.on(CHANNELS.INGEST_STARTED, (d) =>
      update((s) => {
        const idx = (d && d.file_index) || 0;
        // A folder ingest fires Started once per file. Only the first one begins a new
        // request: the rest continue it, so the accumulated totals and the start time must
        // survive them or the job's own numbers would reset on every file.
        const isFirst = idx <= 1;
        return {
          ...s,
          state: INGEST_STATE.RUNNING,
          path: (d && d.path) || '',
          fileIndex: idx,
          fileTotal: (d && d.file_total) || 0,
          // Fold the PREVIOUS file's final numbers in here rather than at its completion.
          // Completion leaves that file's summary in `progress`, so if it were folded there
          // too, `doneTotals + progress` would count the last file twice.
          doneTotals: isFirst ? { ...EMPTY_TOTALS } : addTotals(s.doneTotals, s.progress),
          startedAt: isFirst ? Date.now() : s.startedAt,
          finishedAt: 0,
          lastError: null,
          progress: { ...EMPTY_PROGRESS, chunks_total: (d && d.chunks_total) || 0 },
        };
      }),
    );
    api.on(CHANNELS.INGEST_PROGRESS, (p) => update((s) => ({ ...s, progress: p || s.progress })));
    api.on(CHANNELS.INGEST_COMPLETE, (sum) => {
      let lastFile = true;
      let jobUndated = 0;
      update((s) => {
        const p = (sum && sum.progress) || s.progress;
        // Ingest completes PER FILE. Treating every one as the end of the job made a
        // multi-file run flash "complete" after the first file and report only that file's
        // counts. The run is finished only when the last file finishes.
        lastFile = !s.fileTotal || s.fileIndex >= s.fileTotal;
        // Not folded into doneTotals here: this file's summary stays in `progress`, and the
        // next Started folds it in. jobTotals() is therefore always doneTotals + progress.
        jobUndated = addTotals(s.doneTotals, p).records_undated;
        return {
          ...s,
          state: lastFile ? INGEST_STATE.COMPLETE : INGEST_STATE.RUNNING,
          finishedAt: lastFile ? Date.now() : 0,
          progress: p,
        };
      });
      // Undated rows are stored but kept out of timeline analysis; say so at the moment
      // they are ingested, so the counts never look like data went missing (P22). Said once
      // for the whole job rather than once per file, or a folder would produce a burst of
      // near-identical notices.
      if (lastFile && jobUndated > 0) snackbar.info(`${jobUndated} ${UI.UNDATED_INGESTED_SUFFIX}`);
    });
    api.on(CHANNELS.INGEST_CANCELLED, (sum) =>
      update((s) => {
        const p = (sum && sum.progress) || s.progress;
        return {
          ...s,
          state: INGEST_STATE.CANCELLED,
          finishedAt: Date.now(),
          progress: p,
        };
      }),
    );
    api.on(CHANNELS.INGEST_STATE, (s) =>
      update((prev) => ({ ...prev, lifecycle: s || INGEST_LIFECYCLE.IDLE })),
    );
    api.on(CHANNELS.INGEST_ERROR, (e) => {
      const msg = (e && e.message) || 'ingestion error';
      snackbar.error(msg);
      update((s) => ({ ...s, lastError: e || { message: msg } }));
    });
  }

  async function startFiles(paths, idempotent) {
    wire();
    try {
      await api.startIngest({ source: SOURCES.FILE, paths, idempotent: !!idempotent });
    } catch (err) {
      const msg = String(err && err.message ? err.message : err);
      snackbar.error(msg);
      update((s) => ({ ...s, state: INGEST_STATE.ERROR, lastError: { message: msg } }));
    }
  }

  const startFile = (path, idempotent) => startFiles([path], idempotent);

  // startLive begins a live event-log capture. In continuous mode the run stays open,
  // streaming new records until cancel() stops it, and each channel resumes from its
  // durable bookmark rather than re-reading from the beginning (P7).
  async function startLive(channels, { idempotent = true, continuous = true } = {}) {
    wire();
    try {
      await api.startIngest({
        source: SOURCES.LIVE,
        channels,
        idempotent: !!idempotent,
        continuous: !!continuous,
      });
      return true;
    } catch (err) {
      const msg = String(err && err.message ? err.message : err);
      snackbar.error(msg);
      update((s) => ({ ...s, state: INGEST_STATE.ERROR, lastError: { message: msg } }));
      return false;
    }
  }

  async function cancel() {
    try {
      await api.cancelIngestion();
    } catch (_) {
      /* no active job */
    }
  }

  // pause/resume delegate to the backend and let the ingest:state event update the store,
  // so the UI can never show a pause the pipeline did not actually take.
  async function pause() {
    try {
      await api.pauseIngestion();
      return true;
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
      return false;
    }
  }

  async function resume() {
    try {
      await api.resumeIngestion();
      return true;
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
      return false;
    }
  }

  // syncLifecycle pulls the authoritative state once at mount, so a view opened while a
  // capture is already running (or paused) renders correctly instead of showing idle.
  async function syncLifecycle() {
    try {
      const s = await api.ingestState();
      update((prev) => ({ ...prev, lifecycle: s || INGEST_LIFECYCLE.IDLE }));
    } catch (_) {
      /* backend unavailable */
    }
  }

  return {
    subscribe,
    wire,
    startFile,
    startFiles,
    startLive,
    cancel,
    pause,
    resume,
    syncLifecycle,
    reset: () => set(initialState()),
  };
}

export const ingestion = create();

// --- pure view helpers ---

/**
 * Fraction 0..1 across the WHOLE request, not just the file in flight.
 *
 * Files already finished count as complete, and the one in flight contributes its own
 * fraction of a single file's worth. A single-file run reduces to that file's fraction, so
 * callers do not need to special-case it. Returns null when the job cannot be sized —
 * a live capture, or a source whose chunk count is unknown — so the bar can go
 * indeterminate rather than invent a percentage.
 */
export function overallFraction(state) {
  if (!state) return null;
  const total = state.fileTotal || 0;
  const current = progressFraction(state.progress);
  if (total <= 1) {
    return state.progress && state.progress.chunks_total ? current : null;
  }
  const done = Math.max(0, (state.fileIndex || 1) - 1);
  // A file whose chunk total is unknown still counts as "in progress", contributing
  // nothing rather than distorting the job's fraction.
  const inFlight = state.progress && state.progress.chunks_total ? current : 0;
  return Math.min(1, (done + inFlight) / total);
}

/** Files still to start after the one in flight. Zero when the job is a single file. */
export function filesRemaining(state) {
  if (!state || !state.fileTotal) return 0;
  return Math.max(0, state.fileTotal - (state.fileIndex || 0));
}

/**
 * Counts for the whole request: everything finished, plus the file in flight. This is what
 * a folder ingest should display — per-file counters reset on every file and would
 * otherwise look like the job kept starting over.
 */
export function jobTotals(state) {
  if (!state) return { ...EMPTY_TOTALS };
  return addTotals(state.doneTotals, state.progress);
}

/** Fraction 0..1 of chunks parsed, for the file currently being read. */
export function progressFraction(progress) {
  if (!progress || !progress.chunks_total) return 0;
  return Math.min(1, progress.chunks_parsed / progress.chunks_total);
}

/**
 * Records per second since the run started — the live-capture counter, where a chunk-based
 * percentage is meaningless because a continuous capture has no total. Returns null until
 * there is enough elapsed time to be meaningful.
 */
export function recordsPerSecond(state) {
  const p = state && state.progress;
  if (!p || !state.startedAt) return null;
  const end = state.finishedAt || Date.now();
  const elapsed = (end - state.startedAt) / 1000;
  if (elapsed < 1) return null;
  return Math.round(p.records_read / elapsed);
}

/** Rough seconds remaining based on elapsed time and chunk throughput. */
export function etaSeconds(state) {
  const p = state && state.progress;
  if (!p || !p.chunks_total || !p.chunks_parsed || !state.startedAt) return null;
  const elapsed = (Date.now() - state.startedAt) / 1000;
  const rate = p.chunks_parsed / elapsed; // chunks/sec
  if (rate <= 0) return null;
  const remaining = p.chunks_total - p.chunks_parsed;
  return Math.max(0, Math.round(remaining / rate));
}
