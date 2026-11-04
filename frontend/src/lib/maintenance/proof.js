/**
 * Reading an event-proof result (plan §14.3, item 38).
 *
 * The proof is built and checked in Go. What lives here is how its answer becomes something an
 * analyst can act on: a saved file and the fingerprint it needs, or the reason there is no file
 * yet and the one action that fixes it.
 *
 * 🔒 Fail closed. Only an `ok` that carries a document reads as saved; anything else — including a
 * state this build does not know — is shown as "cannot be proven", never as success.
 */
import { UI } from '../consts/index.js';

/** States, mirroring backend consts.EventProof*. */
export const PROOF_STATE = Object.freeze({
  OK: 'ok',
  UNAVAILABLE: 'unavailable',
  NOT_SEALED: 'not_sealed',
  WRITTEN_SINCE_SEAL: 'written_since_seal',
  CHANGED_SINCE_SEAL: 'changed_since_seal',
  IMAGE_OUTDATED: 'image_outdated',
  RAW_MISMATCH: 'raw_mismatch',
  TOO_LARGE: 'too_large',
  RAW_UNREADABLE: 'raw_unreadable',
  IMAGE_DAMAGED: 'image_damaged',
});

/** The states a seal fixes. raw_mismatch, raw_unreadable and image_damaged are deliberately
 *  absent: they are damage, and sealing repairs nothing — over a damaged image it would build a
 *  new one on top of it. */
const DAMAGE = new Set([PROOF_STATE.RAW_MISMATCH, PROOF_STATE.RAW_UNREADABLE, PROOF_STATE.IMAGE_DAMAGED]);

const SEALABLE = new Set([
  PROOF_STATE.NOT_SEALED,
  PROOF_STATE.WRITTEN_SINCE_SEAL,
  PROOF_STATE.CHANGED_SINCE_SEAL,
  PROOF_STATE.IMAGE_OUTDATED,
]);

/**
 * @typedef {{saved:boolean, message:string, snapshot:string, warnings:string[], sealable:boolean,
 *   damage:boolean}} ProofOutcome
 */

/**
 * proofOutcome turns an ExportEventProof result into what the drawer shows.
 * @param {{state?:string, snapshot?:string, fingerprint_exported?:boolean, document?:string,
 *   file_name?:string}|null|undefined} result
 * @returns {ProofOutcome}
 */
export function proofOutcome(result) {
  const state = result ? result.state : undefined;
  if (state === PROOF_STATE.OK && result.document && result.file_name && result.snapshot) {
    return {
      saved: true,
      message: UI.DETAIL_PROOF_SAVED,
      snapshot: result.snapshot,
      warnings: result.fingerprint_exported ? [] : [UI.DETAIL_PROOF_NO_FINGERPRINT],
      sealable: false,
      damage: false,
    };
  }
  const states = UI.DETAIL_PROOF_STATE;
  const known = typeof state === 'string' && Object.hasOwn(states, state);
  const sealable = SEALABLE.has(state);
  return {
    saved: false,
    message: known ? states[state] : UI.DETAIL_PROOF_UNKNOWN.replace('%s', String(state)),
    snapshot: '',
    warnings: sealable ? [UI.DETAIL_PROOF_RESEAL_NOTE] : [],
    sealable,
    damage: DAMAGE.has(state),
  };
}

/**
 * proofFailure is the outcome for a call that failed outright, as opposed to one that answered
 * with a state.
 * @param {unknown} err
 * @returns {ProofOutcome}
 */
export function proofFailure(err) {
  const text = err && typeof err === 'object' && 'message' in err ? String(err.message) : String(err);
  return { saved: false, message: `${UI.DETAIL_PROOF_FAILED} ${text}`, snapshot: '', warnings: [], sealable: false, damage: false };
}
