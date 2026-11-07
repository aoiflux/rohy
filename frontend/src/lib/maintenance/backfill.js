/**
 * The correlation backfill's outcome on the rules page (BackfillCorrelationKeys).
 *
 * The pass and the seal the backend runs after it are both decided in Go. What lives here is how the
 * outcome becomes the one line the rules page shows; the Maintenance page shows it as a banner
 * (stores/maintenance.js).
 */
import { SEAL_STATE, UI } from '../consts/index.js';

/**
 * backfillNotice is the snackbar line for a backfill that returned. kind is the snackbar method.
 *
 * 🔒 A stopped pass is named as stopped even when the seal after it did not finish, so the analyst
 * who pressed Stop is never told the fields were filled in, and is still told to run it again.
 *
 * 🔒 A seal that compacted but could not be recorded is a seal (api_seal.go), so it is named as one,
 * with what was not recorded beside it. Told it did not finish, the analyst would seal again and
 * move the fingerprint a second time. The Maintenance page shows the same outcome as the
 * backfill_sealed banner beside the error.
 * @param {any} res
 * @returns {{kind: 'error'|'info'|'success', text: string}}
 */
export function backfillNotice(res) {
  const didSeal = res?.sealed?.state === SEAL_STATE.SEALED;
  if (res?.seal_error && !didSeal) {
    const lead = res?.cancelled ? UI.BACKFILL_PARTIAL_SEAL_PROBLEM : UI.BACKFILL_SEAL_PROBLEM;
    return { kind: 'error', text: `${lead} ${res.seal_error}` };
  }
  const sealed = didSeal ? ` ${UI.BACKFILL_SEALED}` : '';
  const gap = res?.seal_error ? ` ${res.seal_error}` : '';
  if (res?.cancelled) return { kind: gap ? 'error' : 'info', text: UI.BACKFILL_PARTIAL + sealed + gap };
  const done = `${UI.BACKFILL_DONE} ${res?.projected ?? 0}${sealed ? `.${sealed}` : ''}`;
  return { kind: gap ? 'error' : 'success', text: done + gap };
}
