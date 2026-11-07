import { describe, expect, it } from 'vitest';
import { backfillBanner, backfillNotice } from './backfill.js';
import { UI } from '../consts/index.js';

describe('the rules page line after a backfill', () => {
  it('says how many it filled in, and that the case was sealed when it was', () => {
    expect(backfillNotice({ projected: 7 })).toEqual({ kind: 'success', text: `${UI.BACKFILL_DONE} 7` });
    expect(backfillNotice({ projected: 7, sealed: { state: 'sealed' } })).toEqual({
      kind: 'success',
      text: `${UI.BACKFILL_DONE} 7. ${UI.BACKFILL_SEALED}`,
    });
  });

  it('names a stopped pass as stopped, and says when it sealed what it had done', () => {
    expect(backfillNotice({ cancelled: true })).toEqual({ kind: 'info', text: UI.BACKFILL_PARTIAL });
    expect(backfillNotice({ cancelled: true, sealed: { state: 'sealed' } })).toEqual({
      kind: 'info',
      text: `${UI.BACKFILL_PARTIAL} ${UI.BACKFILL_SEALED}`,
    });
  });

  it('shows a seal that did not finish beside the result', () => {
    const refused = backfillNotice({ projected: 2, seal_error: 'the case was not sealed: x' });
    expect(refused).toEqual({ kind: 'error', text: `${UI.BACKFILL_SEAL_PROBLEM} the case was not sealed: x` });
    const failed = backfillNotice({ projected: 2, sealed: { state: 'failed' }, seal_error: 'sealing the case failed: x' });
    expect(failed).toEqual({ kind: 'error', text: `${UI.BACKFILL_SEAL_PROBLEM} sealing the case failed: x` });
  });

  // 🔒 A seal that compacted but could not be recorded is a seal (api_seal.go). Told it did not
  // finish, the analyst would seal again and move the fingerprint a second time; not told the
  // fingerprint changed, they would keep the one that is now stale. The Maintenance page says the
  // same, as the backfill_sealed banner beside the error.
  it('names a seal that was not recorded as a seal, with what was not recorded beside it', () => {
    const gap = 'the case was sealed, but its fingerprint could not be recorded in the history: disk full';
    expect(backfillNotice({ projected: 5, sealed: { state: 'sealed' }, seal_error: gap })).toEqual({
      kind: 'error',
      text: `${UI.BACKFILL_DONE} 5. ${UI.BACKFILL_SEALED} ${gap}`,
    });
  });

  // 🔒 The analyst who pressed Stop is never told the fields were filled in, and is still told to
  // run it again, even when the seal after the stopped pass did not finish.
  it('names a stopped pass as stopped even when its seal did not finish', () => {
    const n = backfillNotice({ cancelled: true, sealed: { state: 'failed' }, seal_error: 'x' });
    expect(n).toEqual({ kind: 'error', text: `${UI.BACKFILL_PARTIAL_SEAL_PROBLEM} x` });
    expect(n.text.startsWith(UI.BACKFILL_PARTIAL)).toBe(true);
    expect(n.text).not.toContain(UI.BACKFILL_SEAL_PROBLEM);
  });

  it('names a stopped pass whose seal was not recorded as stopped and sealed', () => {
    expect(backfillNotice({ cancelled: true, sealed: { state: 'sealed' }, seal_error: 'not recorded: x' })).toEqual({
      kind: 'error',
      text: `${UI.BACKFILL_PARTIAL} ${UI.BACKFILL_SEALED} not recorded: x`,
    });
  });
});

describe('the Maintenance page banner after a backfill', () => {
  it('names the result, and says when the case was sealed', () => {
    expect(backfillBanner({ projected: 3 })).toEqual({ lastResult: 'backfill', error: '' });
    expect(backfillBanner({ projected: 3, sealed: { state: 'sealed' } })).toEqual({
      lastResult: 'backfill_sealed',
      error: '',
    });
  });

  it('names a stopped pass as stopped', () => {
    expect(backfillBanner({ cancelled: true })).toEqual({ lastResult: 'backfill_partial', error: '' });
    expect(backfillBanner({ cancelled: true, sealed: { state: 'sealed' } })).toEqual({
      lastResult: 'backfill_partial_sealed',
      error: '',
    });
  });

  it('shows a seal problem beside the result, not instead of it', () => {
    expect(backfillBanner({ projected: 2, sealed: { state: 'sealed' }, seal_error: 'not recorded' })).toEqual({
      lastResult: 'backfill_sealed',
      error: 'not recorded',
    });
  });

  // 🔒 A page that did not start the pass gets no rejection, only the outcome. A failed pass must
  // read as failed there, never as fields filled in.
  it('shows a failed pass as its error, with no done banner', () => {
    expect(backfillBanner({ projected: 1, sealed: { state: 'sealed' }, error: 'disk full' })).toEqual({
      lastResult: '',
      error: 'disk full',
    });
  });

  it('every result it names has a banner line', () => {
    const results = [
      { projected: 1 },
      { projected: 1, sealed: { state: 'sealed' } },
      { cancelled: true },
      { cancelled: true, sealed: { state: 'sealed' } },
    ].map((r) => backfillBanner(r).lastResult);
    for (const key of results) expect(UI.MAINT_DONE[key], key).toBeTruthy();
  });
});
