// Case-maintenance state.
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
import { errorCode } from '../lib/api/errors.js';
import { CHANNELS, ERROR_CODES, SEAL_STATE, UI } from '../lib/consts/index.js';
import { outcomeView, redactionFailure } from '../lib/maintenance/redaction.js';
import { backfillBanner } from '../lib/maintenance/backfill.js';
import { download } from '../lib/export.js';

const empty = () => ({
  /** @type {{total:number,current:number,stale:number}|null} */
  status: null,
  /** @type {any|null} the last integrity report. */
  report: null,
  /**
   * @type {any|null} the case's forensic posture — what it can and cannot establish.
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
  /** The pass `running` names when this page did not start it, '' otherwise: a backfill from the
   *  Rules page, taken on from maintenance:state. Only a pass taken on is let go by that channel. */
  adopted: '',
  /** Whether Stop reaches the backend's running pass (maintenance:state). The page offers Stop only
   *  when this is true and the pass it shows is a backfill. */
  stoppable: false,
  /** @type {{done:number,total:number}|null} backfill progress, while one runs. */
  progress: null,
  /** The last thing that finished, so the view can confirm rather than just stop showing a spinner. */
  lastResult: '',
  /** How many seals this store has asked for, or been told a pass ran (noteSeal). A view holding
   *  anything built against the image before — a custody report — drops it when this moves.
   *  Counted on failure too: a seal whose fingerprint could not be RECORDED still compacted, so the
   *  image moved either way. */
  seals: 0,
  /** How many redactions this store has run or finished. A view holding a copy of an
   *  event's content — the drawer, a canvas card — drops it when this moves. */
  redactions: 0,
  /** @type {any[]|null} redactions that did not finish; null until read. */
  pending: null,
  /** @type {any|null} the case's checkpoints and their receipts; null until read. */
  checkpoints: null,
  /** @type {any|null} what the last publish did: its statement, the seal it ran, its gaps. */
  published: null,
  error: '',
});

function create() {
  const { subscribe, update, set } = writable(empty());

  /** note shows a failure and leaves `running` alone. 🔒 A read that failed neither started nor
   *  ended a pass, so it must not free the page while one runs: the loaders below use it, and a
   *  loader runs inside a pass, beside one (the seal after an ingest re-reads the posture), or on
   *  its own. */
  const note = (e) => {
    const message = String(e && e.message ? e.message : e);
    update((s) => ({ ...s, error: message }));
    return message;
  };

  /** fail ends the pass that failed and shows why. */
  const fail = (e) => {
    const message = note(e);
    update((s) => ({ ...s, running: '' }));
    return message;
  };

  /** recheckRunning is what `running` holds while the quiet re-check after a pass runs.
   *
   *  🔒 A deep re-check verifies the image, and the backend holds its maintenance lock for that step
   *  (VerifyImage). The page stays busy until it returns, so no button looks enabled that the
   *  backend would refuse. A quick re-check takes no lock and leaves the page free. */
  const recheckRunning = (deep) => (deep ? 'check' : '');

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
      note(e);
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
    api.on(CHANNELS.MAINTENANCE_STATE, onState);
    api.on(CHANNELS.MAINTENANCE_COMPLETE, onComplete);
    api.on(CHANNELS.CASE_SEALED, (outcome) => {
      // "unchanged" moved nothing, so there is nothing to refresh.
      if (!outcome || outcome.state !== SEAL_STATE.SEALED) return;
      // 🔒 Dropped, not kept: a verdict about the image the seal replaced looks current, and
      // "verified" is exactly the word somebody acts on.
      update((s) => ({ ...s, image: null }));
      loadForensics();
    });
  }

  /** The seq of the last maintenance:state taken. States are emitted outside the backend's lock,
   *  so one pass's end and the next one's start can arrive in either order. */
  let stateSeq = 0;

  /**
   * onState takes in which pass holds the backend's maintenance lock.
   *
   * 🔒 A pass this page started stays this page's: its promise ends it, and its re-check may still
   * run after the backend is idle. Only an idle page takes a pass on, and only a pass taken on is let
   * go here. A check takes no maintenance lock, so a Rules page backfill can run beside one; the
   * page keeps showing the check, and the view withholds Stop because the pass shown is not the
   * backfill. A backfill that starts while the page is busy is taken on at its next state, the seal.
   * @param {{pass?: string, stoppable?: boolean, seq?: number}|null|undefined} st
   */
  function onState(st) {
    if (!st || typeof st.seq !== 'number' || st.seq < stateSeq) return;
    stateSeq = st.seq;
    const pass = st.pass || '';
    const stoppable = Boolean(pass && st.stoppable);
    const s = get({ subscribe });
    if (pass && (s.running === '' || s.adopted)) {
      const fresh = s.adopted !== pass;
      update((v) => ({
        ...v,
        running: pass,
        adopted: pass,
        stoppable,
        ...(fresh ? { progress: null, lastResult: '', error: '' } : {}),
      }));
      return;
    }
    if (!pass && s.adopted) {
      const was = s.adopted;
      update((v) => ({ ...v, running: '', adopted: '', stoppable: false, progress: null }));
      if (was === 'backfill') loadStatus();
      return;
    }
    update((v) => ({ ...v, stoppable }));
  }

  /** onComplete shows the outcome of a backfill this page took on. Its own backfill reports through
   *  its promise. The seal is not counted here: the page that started the pass takes it in
   *  (noteSeal), and counting it twice would drop a custody report for nothing. */
  function onComplete(res) {
    if (get({ subscribe }).adopted !== 'backfill') return;
    const { lastResult, error } = backfillBanner(res);
    update((s) => ({ ...s, lastResult, error }));
  }

  /** syncRunning asks the backend which pass is running, so a page opened mid-run shows it instead
   *  of looking idle. Listening starts first, so no change between the two is missed. */
  async function syncRunning() {
    wire();
    try {
      onState(await api.maintenanceState());
    } catch (e) {
      note(e);
    }
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
      note(e);
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
      // A read-only session hands the value over and records nothing; say so.
      const note = doc && doc.not_recorded ? `${UI.MAINT_EXPORT_NOT_RECORDED} ${doc.not_recorded}` : '';
      update((s) => ({ ...s, running: '', lastResult: 'export', error: note }));
      await loadForensics();
      return doc;
    } catch (e) {
      fail(e);
      return null;
    }
  }

  /** loadCheckpoints reads the case's checkpoints and receipts. Cheap. */
  async function loadCheckpoints() {
    try {
      const checkpoints = await api.checkpoints();
      update((s) => ({ ...s, checkpoints }));
      return checkpoints;
    } catch (e) {
      note(e);
      return null;
    }
  }

  /** publishCheckpoint seals if needed, records a checkpoint and saves its statement.
   *
   *  🔒 The statement is saved here, wherever the publish was started, and the digest stays on the
   *  page whether or not the save worked: a checkpoint the case holds and the analyst never received
   *  can never be published. A seal may have run, so it counts as one. */
  async function publishCheckpoint() {
    if (busy()) return null;
    update((s) => ({ ...s, running: 'publish', error: '', lastResult: '', published: null }));
    let published;
    try {
      published = await api.publishCheckpoint();
    } catch (e) {
      update((s) => ({ ...s, seals: s.seals + 1 }));
      fail(e);
      return null;
    }
    const shown = get({ subscribe }).report;
    const deep = Boolean(shown?.deep);
    update((s) => ({
      ...s,
      running: shown ? recheckRunning(deep) : '',
      lastResult: 'publish',
      published,
      seals: published?.sealed ? s.seals + 1 : s.seals,
      image: published?.sealed ? null : s.image,
    }));
    if (published?.document) download(published.file_name, 'application/json', published.document);
    await loadCheckpoints();
    await loadForensics();
    if (shown) {
      try {
        const report = await api.checkIntegrity(deep);
        update((s) => ({ ...s, report, running: '' }));
      } catch (e) {
        fail(e);
      }
    }
    return published;
  }

  /** saveStatement saves the statement of an earlier checkpoint again. A read. */
  async function saveStatement(seq) {
    try {
      const exp = await api.checkpointStatement(seq);
      if (exp?.document) download(exp.file_name, 'application/json', exp.document);
      return exp;
    } catch (e) {
      note(e);
      return null;
    }
  }

  /** recordReceipt records what the anchor gave back, then re-reads the checkpoints. Throws, so the
   *  form can keep what was typed and say why it was refused. */
  async function recordReceipt(seq, ref, publishedAt) {
    const out = await api.recordCheckpointReceipt(seq, ref, publishedAt);
    await loadCheckpoints();
    return out;
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
    const deep = Boolean(get({ subscribe }).report?.deep);
    update((s) => ({ ...s, running: recheckRunning(deep), lastResult: 'compact', seals: s.seals + 1 }));
    // 🔒 The seal has happened, and it moved the fingerprint. Refreshing what the page shows is a
    // separate step: its failure is reported, but it must not unsay the seal — a caller told
    // "failed" would seal again, moving the fingerprint a second time.
    try {
      await loadForensics();
      const report = await api.checkIntegrity(deep);
      update((s) => ({ ...s, report, running: '' }));
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
      const deep = Boolean(get({ subscribe }).report?.deep);
      update((s) => ({ ...s, progress: null, lastResult: kind, ...(result || {}), running: recheckRunning(deep) }));
      // Re-check quietly: it writes nothing, so it cannot make things worse.
      const report = await api.checkIntegrity(deep);
      update((s) => ({ ...s, report, running: '' }));
      if (kind === 'backfill') await loadStatus();
      return true;
    } catch (e) {
      fail(e);
      return false;
    }
  }

  /**
   * noteSeal takes in a seal a pass ran on its own: the backfill seals what it rewrote. A seal that
   * ran moved the fingerprint, so the image verdict is dropped, the count moves (a custody report
   * goes stale on it) and the posture is re-read. "failed" is counted because a poisoned seal
   * replaced the image; a seal whose fingerprint could not be recorded arrives as "sealed".
   * "unchanged" and "unavailable" moved nothing.
   * @param {{state?: string}|null|undefined} seal
   */
  function noteSeal(seal) {
    if (seal?.state !== SEAL_STATE.SEALED && seal?.state !== SEAL_STATE.FAILED) return;
    update((s) => ({ ...s, seals: s.seals + 1, image: null }));
    loadForensics();
  }

  /** noteFailedBackfill takes in a backfill that ran and failed (persistence_error; a refusal
   *  carries another code). The pass may have sealed what it rewrote before it failed, and the
   *  outcome that would say so did not come back. Counted, because a dropped verdict costs less
   *  than a stale one. */
  function noteFailedBackfill(e) {
    if (errorCode(e) === ERROR_CODES.PERSISTENCE) noteSeal({ state: SEAL_STATE.FAILED });
  }

  /** backfill fills in the correlation fields, and takes in the seal the backend ran after it. */
  async function backfill() {
    return run('backfill', async () => {
      let res;
      try {
        res = await api.backfillCorrelationKeys();
      } catch (e) {
        noteFailedBackfill(e);
        throw e;
      }
      noteSeal(res?.sealed);
      // run() merges this into the state. 🔒 A seal that did not finish cleanly is shown beside the
      // result, never instead of it: what the pass rewrote is durable. A stopped pass is named as
      // stopped, so the banner never says the fields were filled in when only part of them were.
      return backfillBanner(res);
    });
  }

  /** loadPending reads the redactions that did not finish. */
  async function loadPending() {
    try {
      const pending = await api.pendingRedactions();
      update((s) => ({ ...s, pending: Array.isArray(pending) ? pending : [] }));
      return pending;
    } catch (e) {
      note(e);
      return null;
    }
  }

  /** After a redaction ran, whatever it did: it may have written, and it sealed if it finished.
   *  running is what the page holds next: '' when nothing follows, or the re-check's flag. */
  async function afterRedaction(running = '') {
    update((s) => ({ ...s, running, redactions: s.redactions + 1, seals: s.seals + 1, image: null }));
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
    const deep = Boolean(get({ subscribe }).report?.deep);
    await afterRedaction(recheckRunning(deep));
    const allDone = outcomes.every((o) => o.done);
    update((s) => ({
      ...s,
      lastResult: allDone ? 'finish_redaction' : '',
      error: allDone ? '' : outcomes.map((o) => o.error || o.message).filter(Boolean).join(' '),
    }));
    try {
      const report = await api.checkIntegrity(deep);
      update((s) => ({ ...s, report, running: '' }));
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
    loadCheckpoints,
    publishCheckpoint,
    saveStatement,
    recordReceipt,
    backfill,
    noteSeal,
    noteFailedBackfill,
    syncRunning,
    onState,
    repair: () => run('repair', api.repairRelationIndex),
    rebuild: () => run('rebuild', api.rebuildIndexes),

    /** onProgress is fed by the maintenance:progress channel while a backfill runs. */
    onProgress: (p) => update((s) => ({ ...s, progress: p })),
    cancel: () => {
      api.cancelMaintenance().catch(() => {});
    },
    clearError: () => update((s) => ({ ...s, error: '' })),
    reset: () => {
      stateSeq = 0;
      set(empty());
    },
  };
}

export const maintenance = create();
