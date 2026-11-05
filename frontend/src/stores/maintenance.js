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
import { CHANNELS, SEAL_STATE, UI } from '../lib/consts/index.js';
import { outcomeView, redactionFailure } from '../lib/maintenance/redaction.js';

const empty = () => ({
  /** @type {{total:number,current:number,stale:number}|null} */
  status: null,
  /** @type {any|null} the last integrity report. */
  report: null,
  /**
   * @type {any|null} the case's forensic posture (§14) — what it can and cannot establish.
   *
   * Null means "not read yet", which is deliberately distinct from a posture whose `available`
   * is false. The first is a question nobody has asked; the second is an answer.
   */
  forensics: null,
  /**
   * @type {any|null} the last image verification.
   *
   * Kept separately from `report` rather than folded into it because it is the one maintenance
   * result the analyst copies out of the app — `computed` is the digest they compare against one
   * they retained elsewhere, and that comparison is what turns damage detection into tamper
   * detection.
   */
  image: null,
  /** Which pass is in flight: '' | 'check' | 'backfill' | 'repair' | 'rebuild' | 'verify' | 'compact'. */
  running: '',
  /** @type {{done:number,total:number}|null} backfill progress, while one runs. */
  progress: null,
  /** The last thing that finished, so the view can confirm rather than just stop showing a spinner. */
  lastResult: '',
  /** How many seals this store has asked for. A view holding anything built against the image
   *  before — a custody report — drops it when this moves. Counted on failure too: a seal whose
   *  fingerprint could not be RECORDED still compacted, so the image moved either way. */
  seals: 0,
  /** How many redactions this store has run or finished (plan §14.5). A view holding a copy of an
   *  event's content — the drawer, a canvas card — drops it when this moves. */
  redactions: 0,
  /** @type {any[]|null} redactions that did not finish; null until read. */
  pending: null,
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

  // The seal after an ingest happens outside this store and changes what it shows: a new
  // fingerprint, and an image the last verification was not about. Listened for from the first
  // posture read. Only the cheap posture read follows (configuration plus the roots the seal just
  // computed); the integrity report is left for the analyst to re-run, as everything expensive is.
  let wired = false;
  function wire() {
    if (wired) return;
    wired = true;
    api.on(CHANNELS.CASE_SEALED, (outcome) => {
      // "unchanged" moved nothing, so there is nothing to refresh.
      if (!outcome || outcome.state !== SEAL_STATE.SEALED) return;
      // 🔒 Dropped, not kept: a verdict about the image the seal replaced looks current, and
      // "verified" is exactly the word somebody acts on.
      update((s) => ({ ...s, image: null }));
      loadForensics();
    });
  }

  /** loadForensics reads the posture. Cheap, and worth knowing before anything else on the page is
   *  read: it decides what the rest of the numbers can be taken to mean. */
  async function loadForensics() {
    wire();
    try {
      const forensics = await api.forensicStatus();
      update((s) => ({ ...s, forensics }));
      return forensics;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  /** verifyImage re-derives the image. Expensive, so it is a button and never implicit.
   *
   *  🔒 The previous result is cleared BEFORE the call, not replaced after it. A verdict computed
   *  against the image as it was ten minutes ago looks current, and "verified" is exactly the kind
   *  of word somebody acts on — the same reasoning that drops a stale testbench result on edit. */
  async function verify() {
    if (busy()) return null;
    update((s) => ({ ...s, running: 'verify', error: '', lastResult: '', image: null }));
    try {
      const image = await api.verifyImage();
      update((s) => ({ ...s, image, running: '', lastResult: 'verify' }));
      return image;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  /** exportFingerprint hands back the artefact and records that a copy was taken.
   *
   *  🔒 The posture is re-read afterwards, because the export is what changes it — the caveat
   *  becomes an informational note. Not re-reading would leave the screen still asking for
   *  something that had just been done, which is the same reason a repair re-runs the check. */
  async function exportFingerprint() {
    if (busy()) return null;
    update((s) => ({ ...s, running: 'export', error: '' }));
    try {
      const doc = await api.exportCaseFingerprint();
      // A read-only session hands the value over and records nothing (item 41); say so.
      const note = doc && doc.not_recorded ? `${UI.MAINT_EXPORT_NOT_RECORDED} ${doc.not_recorded}` : '';
      update((s) => ({ ...s, running: '', lastResult: 'export', error: note }));
      await loadForensics();
      return doc;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  /** compact folds the WAL into a new image, then re-reads the posture — compaction is what
   *  computes the roots, so the thing it changes is precisely the posture. Any prior image
   *  verification is dropped: it was about a different image. */
  async function compact() {
    if (busy()) return false;
    update((s) => ({ ...s, running: 'compact', error: '', lastResult: '', image: null }));
    try {
      await api.compactCase();
    } catch (e) {
      update((s) => ({ ...s, seals: s.seals + 1 }));
      fail(e);
      return false;
    }
    update((s) => ({ ...s, running: '', lastResult: 'compact', seals: s.seals + 1 }));
    // 🔒 The seal has happened, and it moved the fingerprint. Refreshing what the page shows is a
    // separate step: its failure is reported, but it must not unsay the seal — a caller told
    // "failed" would seal again, moving the fingerprint a second time.
    try {
      await loadForensics();
      const report = await api.checkIntegrity(Boolean(get({ subscribe }).report?.deep));
      update((s) => ({ ...s, report }));
    } catch (e) {
      fail(e);
    }
    return true;
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

  /** loadPending reads the redactions that did not finish. */
  async function loadPending() {
    try {
      const pending = await api.pendingRedactions();
      update((s) => ({ ...s, pending: Array.isArray(pending) ? pending : [] }));
      return pending;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  /** After a redaction ran, whatever it did: it may have written, and it sealed if it finished. */
  async function afterRedaction() {
    update((s) => ({ ...s, running: '', redactions: s.redactions + 1, seals: s.seals + 1, image: null }));
    await loadPending();
    try {
      await loadForensics();
    } catch (_) {
      // loadForensics reports its own failure.
    }
  }

  /**
   * redact removes an event's content. Returns the outcome view, never throws.
   *
   * 🔒 Refused, not queued, while another pass runs here; the backend refuses it too while
   * anything else holds the case.
   * @param {number} id
   * @param {string} reason
   */
  async function redact(id, reason) {
    if (busy()) return redactionFailure(UI.DETAIL_PROOF_BUSY);
    update((s) => ({ ...s, running: 'redact', error: '', lastResult: '' }));
    let outcome;
    try {
      outcome = await api.redactEvent({ event: id, reason });
    } catch (e) {
      // Nothing started, so nothing changed: the counters stay where they are.
      update((s) => ({ ...s, running: '' }));
      return redactionFailure(e);
    }
    await afterRedaction();
    return outcomeView(outcome);
  }

  /** finishRedactions runs every pending redaction's steps again, then re-checks the case. */
  async function finishRedactions() {
    if (busy()) return [];
    update((s) => ({ ...s, running: 'finish_redaction', error: '', lastResult: '' }));
    const pending = (await api.pendingRedactions().catch(() => [])) || [];
    const outcomes = [];
    for (const e of pending) {
      try {
        outcomes.push(outcomeView(await api.finishRedaction(e.id)));
      } catch (err) {
        outcomes.push(redactionFailure(err));
      }
    }
    await afterRedaction();
    const allDone = outcomes.every((o) => o.done);
    update((s) => ({
      ...s,
      lastResult: allDone ? 'finish_redaction' : '',
      error: allDone ? '' : outcomes.map((o) => o.error || o.message).filter(Boolean).join(' '),
    }));
    try {
      const report = await api.checkIntegrity(Boolean(get({ subscribe }).report?.deep));
      update((s) => ({ ...s, report }));
    } catch (e) {
      fail(e);
    }
    return outcomes;
  }

  return {
    subscribe,
    current: () => get({ subscribe }),
    loadPending,
    redact,
    finishRedactions,
    loadStatus,
    loadForensics,
    check,
    verify,
    compact,
    exportFingerprint,
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
