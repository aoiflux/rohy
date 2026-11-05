/**
 * Reading a redaction (plan §14.5, item 40).
 *
 * What a redaction does is decided and done in Go. What lives here is how its preview and its
 * outcome become something the drawer can lay out.
 *
 * 🔒 Fail closed. A redaction reads as done only when the backend says so AND every step it knows
 * ran to done; a step state this build does not know is shown, and counted, as not done.
 * 🔒 Never overstated. The copy says where rohy removed the content from, and lists what it cannot
 * reach, every time.
 */
import { UI, REDACT_STEP, REDACT_STEP_STATE, MAX_REDACTION_REASON_BYTES } from '../consts/index.js';

const STEPS = Object.values(REDACT_STEP);
const KNOWN_STATES = new Set(Object.values(REDACT_STEP_STATE));

/** @param {string} s @param {Record<string, string|number>} subs */
const fill = (s, subs) => Object.entries(subs).reduce((t, [k, v]) => t.replace(k, String(v)), s);

/**
 * reasonProblem says what is wrong with a reason, or '' when it can be kept. Measured in UTF-8
 * bytes, as the backend measures it.
 * @param {string} reason
 */
export function reasonProblem(reason) {
  const r = String(reason ?? '').trim();
  if (r === '') return UI.REDACT_REASON_EMPTY;
  const bytes = new TextEncoder().encode(r).length;
  if (bytes > MAX_REDACTION_REASON_BYTES) {
    return fill(UI.REDACT_REASON_LONG, { '%n': bytes, '%m': MAX_REDACTION_REASON_BYTES });
  }
  return '';
}

/**
 * @typedef {{key:string, note:string, tags:string[], flagged:boolean}} KeptFinding
 * @typedef {{ok:boolean, blocked:string, goes:string[], stays:string[], findings:KeptFinding[],
 *   notReached:string[]}} PreviewView
 */

/**
 * previewView turns a PreviewRedaction result into the dialog's three lists.
 * @param {any} p
 * @returns {PreviewView}
 */
export function previewView(p) {
  const plan = p && p.plan;
  const entry = plan && plan.entry;
  const targets = entry && Array.isArray(entry.targets) ? entry.targets : null;
  if (!targets || targets.length === 0) {
    // Nothing to show is not something to confirm.
    return { ok: false, blocked: UI.REDACT_FAILED, goes: [], stays: [], findings: [], notReached: [] };
  }
  const relations = Array.isArray(entry.relations) ? entry.relations.length : 0;
  const drawn = Array.isArray(plan.analyst_relations) ? plan.analyst_relations.length : 0;
  const copies = Number(plan.payload_copies) || 0;
  const findings = (Array.isArray(p.findings) ? p.findings : [])
    .filter((f) => f && typeof f.key === 'string')
    .map((f) => ({
      key: f.key,
      note: typeof f.note === 'string' ? f.note : '',
      tags: Array.isArray(f.tags) ? f.tags.filter((t) => typeof t === 'string') : [],
      flagged: !!f.flagged,
    }));

  const goes = [UI.REDACT_GOES_RECORD];
  if (copies > 0) goes.push(fill(UI.REDACT_GOES_RAW, { '%n': copies }));
  if (targets.length > 1) goes.push(fill(UI.REDACT_GOES_SIBLINGS, { '%n': targets.length - 1 }));
  if (relations > 0) goes.push(fill(UI.REDACT_GOES_RELATIONS, { '%n': relations }));
  goes.push(UI.REDACT_GOES_COPIES);

  const stays = [UI.REDACT_STAYS_NODE];
  if (drawn > 0) stays.push(fill(UI.REDACT_STAYS_RELATIONS, { '%n': drawn }));
  if (findings.length > 0) stays.push(UI.REDACT_STAYS_FINDINGS);
  stays.push(UI.REDACT_STAYS_HASHES);

  return {
    ok: true,
    blocked: typeof p.blocked === 'string' ? p.blocked : '',
    goes,
    stays,
    findings,
    notReached: [...UI.REDACT_UNREACHED_LIST],
  };
}

/**
 * @typedef {{name:string, label:string, tone:string, stateLabel:string, error:string,
 *   notes:string[], count:number}} StepLine
 * @typedef {{ok:boolean, done:boolean, message:string, error:string, steps:StepLine[],
 *   entry:number}} OutcomeView
 */

/**
 * outcomeView turns a RedactEvent or FinishRedaction result into the step list.
 * @param {any} o
 * @returns {OutcomeView}
 */
export function outcomeView(o) {
  if (!o || typeof o !== 'object') return redactionFailure(UI.REDACT_FAILED);
  const got = new Map((Array.isArray(o.steps) ? o.steps : []).filter((s) => s && typeof s.name === 'string').map((s) => [s.name, s]));
  // Every step this build knows is listed, in order, whether or not the backend reported it: a
  // step missing from the outcome did not happen as far as anyone can tell.
  const steps = STEPS.map((name) => {
    const s = got.get(name) || {};
    const state = typeof s.state === 'string' ? s.state : REDACT_STEP_STATE.NOT_REACHED;
    const known = KNOWN_STATES.has(state);
    return {
      name,
      label: UI.REDACT_STEP_LABEL[name],
      tone: known ? state : REDACT_STEP_STATE.NOT_REACHED,
      stateLabel: known ? UI.REDACT_STEP_STATE_LABEL[state] : UI.REDACT_STEP_UNKNOWN.replace('%s', state),
      error: typeof s.error === 'string' ? s.error : '',
      notes: Array.isArray(s.notes) ? s.notes.filter((n) => typeof n === 'string') : [],
      count: Number(s.count) || 0,
    };
  });
  const done = o.done === true && steps.every((s) => s.tone === REDACT_STEP_STATE.DONE);
  return {
    ok: true,
    done,
    message: done ? UI.REDACT_DONE : UI.REDACT_UNFINISHED,
    error: typeof o.error === 'string' ? o.error : '',
    steps,
    entry: Number(o.entry) || 0,
  };
}

/**
 * redactionFailure is the view for a call that did not start a redaction.
 * @param {unknown} err
 * @returns {OutcomeView}
 */
export function redactionFailure(err) {
  const text = err && typeof err === 'object' && 'message' in err ? String(err.message) : String(err);
  return { ok: false, done: false, message: '', error: text, steps: [], entry: 0 };
}

/**
 * redactedLine says when and why an event was redacted, from GetEvent's ledger record.
 * @param {any} detail
 */
export function redactedLine(detail) {
  const r = detail && detail.redaction;
  if (!r || typeof r.reason !== 'string') return UI.REDACTED_NO_LEDGER;
  const at = typeof r.at === 'string' ? r.at.slice(0, 10) : '';
  return fill(UI.REDACTED_ON, { '%d': at, '%r': r.reason });
}

/**
 * pendingLine describes an unfinished redaction for the Maintenance list.
 * @param {any} e a journal entry
 */
export function pendingLine(e) {
  const at = e && typeof e.at === 'string' ? e.at.slice(0, 10) : '';
  return fill(UI.REDACT_PENDING_ROW, { '%e': e ? e.event : '', '%d': at, '%r': e ? e.reason : '' });
}
