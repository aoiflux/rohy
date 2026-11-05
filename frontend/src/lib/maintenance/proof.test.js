import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import EventDetail from '../../components/events/EventDetail.svelte';
import { PROOF_STATE, proofOutcome, proofFailure } from './proof.js';
import { UI } from '../consts/index.js';

// Event proofs in the UI (plan §14.3, item 38).
//
// As with the fingerprint copy, the risk here is a false statement about evidence: a "saved" that
// reads as "proven", a proof with no retained fingerprint presented as useful, or a damaged record
// routed around with a seal button. These tests are mostly about which of those the drawer says.

const OK = {
  state: PROOF_STATE.OK,
  event: 7,
  snapshot: 'ab'.repeat(32),
  file_name: 'event-7-proof.json',
  document: '{"format":"rohy-event-proof"}\n',
  fingerprint_exported: true,
};

describe('lib/maintenance / proof outcome', () => {
  it('a built proof is saved and names the fingerprint it needs', () => {
    const o = proofOutcome(OK);
    expect(o.saved).toBe(true);
    expect(o.snapshot).toBe(OK.snapshot);
    expect(o.warnings).toEqual([]);
    expect(o.sealable).toBe(false);
  });

  // 🔒 Said every time it applies: a proof checkable only against a value inside the case proves
  // nothing.
  it('warns when no copy of that fingerprint has left the app', () => {
    const o = proofOutcome({ ...OK, fingerprint_exported: false });
    expect(o.saved).toBe(true);
    expect(o.warnings).toContain(UI.DETAIL_PROOF_NO_FINGERPRINT);
  });

  // 🔒 Fail closed: "ok" without the document is not a saved proof.
  it('an ok with nothing to save is not a success', () => {
    for (const missing of ['document', 'file_name', 'snapshot']) {
      const r = { ...OK, [missing]: '' };
      expect(proofOutcome(r).saved).toBe(false);
    }
  });

  it('every state the backend reports has its own copy', () => {
    for (const state of Object.values(PROOF_STATE)) {
      if (state === PROOF_STATE.OK) continue;
      const o = proofOutcome({ state, event: 7, fingerprint_exported: false });
      expect(o.saved).toBe(false);
      expect(o.message).toBe(UI.DETAIL_PROOF_STATE[state]);
    }
  });

  it('offers a seal exactly where sealing is the fix, and says it moves the fingerprint', () => {
    const sealable = [
      PROOF_STATE.NOT_SEALED,
      PROOF_STATE.WRITTEN_SINCE_SEAL,
      PROOF_STATE.CHANGED_SINCE_SEAL,
      PROOF_STATE.IMAGE_OUTDATED,
    ];
    for (const state of sealable) {
      const o = proofOutcome({ state });
      expect(o.sealable).toBe(true);
      expect(o.warnings).toContain(UI.DETAIL_PROOF_RESEAL_NOTE);
    }
    expect(proofOutcome({ state: PROOF_STATE.UNAVAILABLE }).sealable).toBe(false);
    expect(proofOutcome({ state: PROOF_STATE.TOO_LARGE }).sealable).toBe(false);
  });

  // 🔒 Damage is reported, not routed around: the payload store is outside the sealed image, so a
  // seal would repair nothing and hide the finding.
  it('a raw record that no longer matches, or cannot be read, is damage, with no seal button', () => {
    for (const state of [
      PROOF_STATE.RAW_MISMATCH,
      PROOF_STATE.RAW_UNREADABLE,
      PROOF_STATE.IMAGE_DAMAGED,
      PROOF_STATE.RECORD_EMPTY,
    ]) {
      const o = proofOutcome({ state });
      expect(o.damage).toBe(true);
      expect(o.sealable).toBe(false);
      expect(o.saved).toBe(false);
      expect(o.message).toBe(UI.DETAIL_PROOF_STATE[state]);
    }
  });

  // A redaction that did not finish is neither damage nor fixed by a seal: the remedy is to finish it.
  it('a redacted event, finished or not, is not damage and offers no seal', () => {
    for (const state of [PROOF_STATE.RAW_REDACTED, PROOF_STATE.REDACTED]) {
      const o = proofOutcome({ state });
      expect(o.damage).toBe(false);
      expect(o.sealable).toBe(false);
      expect(o.saved).toBe(false);
      expect(o.message).toBe(UI.DETAIL_PROOF_STATE[state]);
    }
  });

  it('a state this build does not know reads as unprovable, never as saved', () => {
    for (const state of ['from_a_later_build', 'constructor', '__proto__', undefined]) {
      const o = proofOutcome({ state });
      expect(o.saved).toBe(false);
      expect(o.sealable).toBe(false);
      expect(o.message).toContain('cannot be proven');
    }
    expect(proofOutcome(null).saved).toBe(false);
  });

  it('a failed call says so with the error', () => {
    expect(proofFailure(new Error('disk full')).message).toBe(`${UI.DETAIL_PROOF_FAILED} disk full`);
    expect(proofFailure('busy').message).toBe(`${UI.DETAIL_PROOF_FAILED} busy`);
    expect(proofFailure('busy').saved).toBe(false);
  });
});

describe('components/events / EventDetail proof section', () => {
  it('offers the export and says what the file contains', () => {
    const event = { id: 7, event_id: '4624', hash_raw: 'a', hash_normalized: 'b', timestamp: '2026-08-09T10:00:00Z' };
    const { body } = render(EventDetail, { props: { event } });
    expect(body).toContain(UI.DETAIL_PROOF_SAVE);
    expect(body).toContain('rohyctl verify-proof');
    // 🔒 The disclosure is stated, not rounded down to "nothing about other events".
    expect(body).toContain('no other event’s content');
    expect(body).toContain('how many records the case has');
    // Nothing has been asked yet, so nothing is claimed.
    expect(body).not.toContain(UI.DETAIL_PROOF_SAVED);
    expect(body).not.toContain(UI.DETAIL_PROOF_SEAL);
  });
});
