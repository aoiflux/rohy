/**
 * Reading a custody report (plan §14.4, §15.6, item 39).
 *
 * The report is assembled and worded in Go; its messages are shown as Go wrote them, so there is
 * one source for what each layer says. What lives here is how the report becomes something the
 * drawer can lay out: labels, a tone per layer, and the counts.
 *
 * 🔒 Gaps, never a verdict. Nothing here, for any input, says the event's custody is accounted for.
 * 🔒 Fail closed. A state this build does not know is shown, and counted, as not established.
 */
import { UI } from '../consts/index.js';

/** Layer states, mirroring backend consts.Custody{Established,NotEstablished,Broken}. */
export const CUSTODY_STATE = Object.freeze({
  ESTABLISHED: 'established',
  NOT_ESTABLISHED: 'not_established',
  BROKEN: 'broken',
});

/** Report forms, mirroring backend consts.CustodyForm*. */
export const CUSTODY_FORM = Object.freeze({
  CASE_ONLY: 'case_only',
  KEPT: 'kept_fingerprint',
  NONE: 'none',
});

/** Presence values, mirroring backend consts.CustodyPresence*. */
export const CUSTODY_PRESENCE = Object.freeze({
  UNAVAILABLE: 'unavailable',
  IN_SEAL: 'in_seal',
  WRITTEN_SINCE_SEAL: 'written_since_seal',
  NO_FINGERPRINT: 'no_fingerprint',
  REMOVED_SINCE_SEAL: 'removed_since_seal',
  REDACTED: 'redacted',
  // Still in the case, its content removed by a recorded redaction (item 40).
  REDACTED_CONTENT: 'redacted_content',
  NOT_IN_CASE: 'not_in_case',
});

const KNOWN_STATES = new Set(Object.values(CUSTODY_STATE));
const KNOWN_PRESENCE = new Set(Object.values(CUSTODY_PRESENCE));

/**
 * caseMark is what a custody report goes stale on: a seal (from an ingest or the Maintenance
 * page), an ingest starting or ending, a maintenance pass starting or ending. The drawer drops its
 * report when the mark changes.
 *
 * 🔒 A mark, not the stores themselves. Both stores replace their whole state on every update —
 * every progress tick — so watching them directly drops the report continuously and no report can
 * be obtained during an ingest or a backfill.
 * @param {{seals?:number, state?:string}|null|undefined} ingestion
 * @param {{seals?:number, running?:string}|null|undefined} maintenance
 * @returns {string}
 */
export function caseMark(ingestion, maintenance) {
  const i = ingestion || {};
  const m = maintenance || {};
  return `${i.seals ?? 0}|${i.state ?? ''}|${m.seals ?? 0}|${m.running ?? ''}|${m.redactions ?? 0}`;
}

/**
 * @typedef {{key:string, label:string, tone:string, stateLabel:string, message:string,
 *   notes:string[], remedy:string, cause:string}} CustodyLine
 * @typedef {{ok:boolean, error:string, presence:string, form:string, counts:string,
 *   snapshot:string, layers:CustodyLine[], records:CustodyLine[], establishes:string[],
 *   doesNot:string[], broken:number, notEstablished:number}} CustodyView
 */

/** @param {any} l @returns {CustodyLine} */
function line(l) {
  const state = l && typeof l.state === 'string' ? l.state : '';
  const known = KNOWN_STATES.has(state);
  const key = l && typeof l.layer === 'string' ? l.layer : '';
  return {
    key,
    label: Object.hasOwn(UI.CUSTODY_LAYER_LABEL, key) ? UI.CUSTODY_LAYER_LABEL[key] : key,
    // An unknown state is drawn the way an unmade check is, never the way a made one is.
    tone: known ? state : CUSTODY_STATE.NOT_ESTABLISHED,
    stateLabel: known ? UI.CUSTODY_STATE_LABEL[state] : UI.CUSTODY_STATE_UNKNOWN.replace('%s', String(state)),
    message: l && typeof l.message === 'string' ? l.message : '',
    notes: l && Array.isArray(l.notes) ? l.notes.filter((n) => typeof n === 'string') : [],
    remedy: l && typeof l.remedy === 'string' ? l.remedy : '',
    cause: l && typeof l.cause === 'string' ? l.cause : '',
  };
}

/** @param {unknown} v @returns {string[]} */
const strings = (v) => (Array.isArray(v) ? v.filter((s) => typeof s === 'string') : []);

/**
 * custodyView turns an EventCustody result into what the drawer shows.
 * @param {any} report
 * @returns {CustodyView}
 */
export function custodyView(report) {
  if (!report || typeof report !== 'object') return custodyFailure(UI.CUSTODY_EMPTY);
  const layers = Array.isArray(report.layers) ? report.layers.map(line) : [];
  const records = Array.isArray(report.case_records) ? report.case_records.map(line) : [];
  // Counted here from the lines as drawn, so an unknown state counts as what it is shown as.
  let broken = 0;
  let notEstablished = 0;
  for (const l of [...layers, ...records]) {
    if (l.tone === CUSTODY_STATE.BROKEN) broken++;
    else if (l.tone === CUSTODY_STATE.NOT_ESTABLISHED) notEstablished++;
  }
  const presence = KNOWN_PRESENCE.has(report.presence)
    ? String(report.presence_message || '')
    : UI.CUSTODY_PRESENCE_UNKNOWN.replace('%s', String(report.presence));
  const form = Object.hasOwn(UI.CUSTODY_FORM_LABEL, report.form)
    ? UI.CUSTODY_FORM_LABEL[report.form]
    : UI.CUSTODY_FORM_UNKNOWN.replace('%s', String(report.form));
  const counts =
    broken + notEstablished === 0
      ? UI.CUSTODY_COUNTS_NONE
      : UI.CUSTODY_COUNTS.replace('%b', String(broken)).replace('%n', String(notEstablished));
  return {
    ok: true,
    error: '',
    presence,
    form,
    counts,
    snapshot: typeof report.snapshot === 'string' ? report.snapshot : '',
    layers,
    records,
    establishes: strings(report.establishes),
    doesNot: strings(report.does_not_establish),
    broken,
    notEstablished,
  };
}

/**
 * custodyFailure is the view for a call that failed outright.
 * @param {unknown} err
 * @returns {CustodyView}
 */
export function custodyFailure(err) {
  const text = err && typeof err === 'object' && 'message' in err ? String(err.message) : String(err);
  return {
    ok: false,
    error: `${UI.CUSTODY_FAILED} ${text}`,
    presence: '',
    form: '',
    counts: '',
    snapshot: '',
    layers: [],
    records: [],
    establishes: [],
    doesNot: [],
    broken: 0,
    notEstablished: 0,
  };
}
