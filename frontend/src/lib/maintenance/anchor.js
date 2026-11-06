/**
 * Checkpoints on the Maintenance page (plan §14.7, item 42).
 *
 * The checks run and are worded in Go; what lives here is how their answers are laid out.
 *
 * 🔒 Never a tick. The best a held digest can say is that the case still holds what the checkpoint
 * committed to — not when it was published, by whom, or whether anything in the case is true. So no
 * state here is drawn as "ok": the best is drawn as information.
 * 🔒 Fail closed. A state this build does not know is drawn as a warning.
 */
import { UI } from '../consts/index.js';
import { CUSTODY_STATE } from './custody.js';

/** Check states, mirroring backend consts.AnchorCheck*. */
export const ANCHOR_CHECK = Object.freeze({
  UNAVAILABLE: 'unavailable',
  SEAL_FINGERPRINT: 'seal_fingerprint',
  NOT_FOUND: 'not_found',
  UNREAD: 'unread',
  BROKEN: 'broken',
  MATCHES: 'matches',
  DESCENDS: 'descends',
  UNPLACED: 'unplaced',
  INCOMPLETE: 'incomplete',
});

const TONE = Object.freeze({
  [ANCHOR_CHECK.MATCHES]: 'info',
  [ANCHOR_CHECK.DESCENDS]: 'info',
  [ANCHOR_CHECK.BROKEN]: 'err',
  [ANCHOR_CHECK.NOT_FOUND]: 'err',
});

/** checkTone is how an answer is drawn: 'info', 'warn' or 'err' — never 'ok'. @param {string} state */
export function checkTone(state) {
  return Object.hasOwn(TONE, state) ? TONE[state] : 'warn';
}

const KNOWN_LAYER_STATES = new Set(Object.values(CUSTODY_STATE));

/**
 * anchorCheckView lays out a CheckCheckpoint answer.
 * @param {any} r
 */
export function anchorCheckView(r) {
  const state = r && typeof r.state === 'string' ? r.state : '';
  const layers = Array.isArray(r?.layers) ? r.layers : [];
  return {
    state,
    tone: checkTone(state),
    message: typeof r?.message === 'string' ? r.message : '',
    layers: layers.map((l) => {
      const s = typeof l?.state === 'string' ? l.state : '';
      const known = KNOWN_LAYER_STATES.has(s);
      const key = typeof l?.layer === 'string' ? l.layer : '';
      return {
        key,
        label: Object.hasOwn(UI.ANCHOR_LAYER_LABEL, key) ? UI.ANCHOR_LAYER_LABEL[key] : key,
        tone: known ? s : CUSTODY_STATE.NOT_ESTABLISHED,
        stateLabel: known ? UI.CUSTODY_STATE_LABEL[s] : UI.CUSTODY_STATE_UNKNOWN.replace('%s', String(s)),
        message: typeof l?.message === 'string' ? l.message : '',
        notes: Array.isArray(l?.notes) ? l.notes : [],
      };
    }),
    notes: Array.isArray(r?.notes) ? r.notes : [],
    establishes: Array.isArray(r?.establishes) ? r.establishes : [],
    doesNot: Array.isArray(r?.does_not_establish) ? r.does_not_establish : [],
  };
}

/** latest is the newest checkpoint in a Checkpoints() list, or null. @param {any} list */
export function latest(list) {
  const cs = Array.isArray(list?.checkpoints) ? list.checkpoints : [];
  return cs.length ? cs[cs.length - 1] : null;
}

/**
 * receiptTime turns a datetime-local input's value into the RFC 3339 time the backend reads: '' for
 * none, null for a value that is not a time.
 * @param {string} local
 * @returns {string|null}
 */
export function receiptTime(local) {
  const v = String(local ?? '').trim();
  if (!v) return '';
  const t = new Date(v);
  return Number.isNaN(t.getTime()) ? null : t.toISOString();
}
