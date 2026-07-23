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

function initialState() {
  return {
    state: INGEST_STATE.IDLE,
    // lifecycle is the BACKEND's state (idle/active/paused/stopping). It is reported, not
    // inferred: pause/resume is owned by the pipeline, so the UI must not guess (P8).
    lifecycle: INGEST_LIFECYCLE.IDLE,
    progress: { ...EMPTY_PROGRESS },
    path: '',
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
      update((s) => ({
        ...s,
        state: INGEST_STATE.RUNNING,
        path: (d && d.path) || '',
        startedAt: Date.now(),
        finishedAt: 0,
        lastError: null,
        progress: { ...EMPTY_PROGRESS, chunks_total: (d && d.chunks_total) || 0 },
      })),
    );
    api.on(CHANNELS.INGEST_PROGRESS, (p) => update((s) => ({ ...s, progress: p || s.progress })));
    api.on(CHANNELS.INGEST_COMPLETE, (sum) => {
      update((s) => ({
        ...s,
        state: INGEST_STATE.COMPLETE,
        finishedAt: Date.now(),
        progress: (sum && sum.progress) || s.progress,
      }));
      // Undated rows are stored but kept out of timeline analysis; say so at the moment
      // they are ingested, so the counts never look like data went missing (P22).
      const undated = (sum && sum.progress && sum.progress.records_undated) || 0;
      if (undated > 0) snackbar.info(`${undated} ${UI.UNDATED_INGESTED_SUFFIX}`);
    });
    api.on(CHANNELS.INGEST_CANCELLED, (sum) =>
      update((s) => ({
        ...s,
        state: INGEST_STATE.CANCELLED,
        finishedAt: Date.now(),
        progress: (sum && sum.progress) || s.progress,
      })),
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

/** Fraction 0..1 of chunks parsed. */
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
