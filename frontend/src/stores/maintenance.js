// Case-maintenance state (P30).
//
// One store for the whole Maintenance view: the correlation-key status, the last integrity
// report, and whichever pass is currently running.
//
// 🔒 Nothing here runs on its own. Every field is filled by an explicit call, because everything
// this store fronts is proportional to the size of the case — and work that taxes a launch to
// re-prove something almost always true is not a safety feature (PERFORMANCE.md §12a).
//
// `running` is a single flag rather than one per action on purpose: the backend serialises these
// passes over one store, so two buttons that both looked enabled would be a promise the backend
// refuses to keep.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';

const empty = () => ({
  /** @type {{total:number,current:number,stale:number}|null} */
  status: null,
  /** @type {any|null} the last integrity report. */
  report: null,
  /** Which pass is in flight: '' | 'check' | 'backfill' | 'repair' | 'rebuild'. */
  running: '',
  /** @type {{done:number,total:number}|null} backfill progress, while one runs. */
  progress: null,
  /** The last thing that finished, so the view can confirm rather than just stop showing a spinner. */
  lastResult: '',
  error: '',
});

function create() {
  const { subscribe, update, set } = writable(empty());

  const fail = (e) => {
    const message = String(e && e.message ? e.message : e);
    update((s) => ({ ...s, running: '', error: message }));
    return message;
  };

  /** guard refuses to start a second pass rather than queueing one the backend would reject. */
  function busy() {
    return get({ subscribe }).running !== '';
  }

  async function loadStatus() {
    try {
      const status = await api.correlationKeyStatus();
      update((s) => ({ ...s, status }));
      return status;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  async function check(deep) {
    if (busy()) return null;
    update((s) => ({ ...s, running: 'check', error: '', lastResult: '' }));
    try {
      const report = await api.checkIntegrity(deep);
      update((s) => ({ ...s, report, running: '' }));
      return report;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  /** run performs a repair action and then RE-CHECKS, so the screen shows the state after the
   *  fix rather than the state that prompted it. A report left stale after its own remedy would
   *  keep asking for something already done. */
  async function run(kind, fn) {
    if (busy()) return false;
    update((s) => ({ ...s, running: kind, error: '', lastResult: '' }));
    try {
      const result = await fn();
      update((s) => ({ ...s, running: '', progress: null, lastResult: kind, ...(result || {}) }));
      // Re-check quietly: it reads and writes nothing, so it cannot make things worse.
      const report = await api.checkIntegrity(Boolean(get({ subscribe }).report?.deep));
      update((s) => ({ ...s, report }));
      if (kind === 'backfill') await loadStatus();
      return true;
    } catch (e) {
      fail(e);
      return false;
    }
  }

  return {
    subscribe,
    current: () => get({ subscribe }),
    loadStatus,
    check,
    backfill: () => run('backfill', api.backfillCorrelationKeys),
    repair: () => run('repair', api.repairRelationIndex),
    rebuild: () => run('rebuild', api.rebuildIndexes),

    /** onProgress is fed by the maintenance:progress channel while a backfill runs. */
    onProgress: (p) => update((s) => ({ ...s, progress: p })),
    cancel: () => {
      api.cancelMaintenance().catch(() => {});
    },
    clearError: () => update((s) => ({ ...s, error: '' })),
    reset: () => set(empty()),
  };
}

export const maintenance = create();
