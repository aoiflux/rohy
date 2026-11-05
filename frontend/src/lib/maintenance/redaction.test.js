import { describe, it, expect } from 'vitest';
import { reasonProblem, previewView, outcomeView, redactionFailure, redactedLine, pendingLine } from './redaction.js';
import { UI, REDACT_STEP, REDACT_STEP_STATE, MAX_REDACTION_REASON_BYTES } from '../consts/index.js';

// Redaction in the UI (plan §14.5, item 40). The risk is a false statement about evidence: a
// redaction shown as done that did not finish, or a preview that hides what it cannot reach.

const preview = {
  plan: {
    entry: {
      event: 7,
      targets: [
        { node: 7, hash_normalized: 'a' },
        { node: 9, hash_normalized: 'b' },
      ],
      relations: [20, 21],
    },
    analyst_relations: [{ id: 30 }],
    payload_copies: 3,
  },
  findings: [{ key: 'a', note: 'contractor laptop', tags: ['lateral'], flagged: true }],
  blocked: '',
};

const allDone = {
  entry: 1,
  event: 7,
  done: true,
  steps: Object.values(REDACT_STEP).map((name) => ({ name, state: REDACT_STEP_STATE.DONE, count: 1 })),
};

describe('lib/maintenance / redaction reason', () => {
  it('requires a reason and measures it as the backend does', () => {
    expect(reasonProblem('')).toBe(UI.REDACT_REASON_EMPTY);
    expect(reasonProblem('   ')).toBe(UI.REDACT_REASON_EMPTY);
    expect(reasonProblem(undefined)).toBe(UI.REDACT_REASON_EMPTY);
    expect(reasonProblem('erasure request')).toBe('');
    // Bytes, not characters: two-byte letters reach the limit at half the length.
    expect(reasonProblem('é'.repeat(MAX_REDACTION_REASON_BYTES / 2))).toBe('');
    expect(reasonProblem('é'.repeat(MAX_REDACTION_REASON_BYTES / 2 + 1))).toContain(String(MAX_REDACTION_REASON_BYTES));
  });
});

describe('lib/maintenance / redaction preview', () => {
  it('lists what goes, what stays, and what rohy cannot reach', () => {
    const v = previewView(preview);
    expect(v.ok).toBe(true);
    expect(v.goes).toContain(UI.REDACT_GOES_RECORD);
    expect(v.goes.join(' ')).toContain('1 other event');
    expect(v.goes.join(' ')).toContain('2 relation');
    expect(v.goes.join(' ')).toContain('3 payload');
    expect(v.stays.join(' ')).toContain('1 relation(s) drawn');
    expect(v.stays).toContain(UI.REDACT_STAYS_HASHES);
    expect(v.findings).toEqual([{ key: 'a', note: 'contractor laptop', tags: ['lateral'], flagged: true }]);
    // 🔒 Always said: what is outside rohy's reach.
    expect(v.notReached).toEqual([...UI.REDACT_UNREACHED_LIST]);
  });

  it('a lone event with nothing attached names no siblings, relations or notes', () => {
    const v = previewView({
      plan: { entry: { targets: [{ node: 7 }], relations: [] }, analyst_relations: [], payload_copies: 1 },
      findings: [],
    });
    expect(v.goes.join(' ')).not.toContain('other event');
    expect(v.goes.join(' ')).not.toContain('a rule made');
    expect(v.stays).not.toContain(UI.REDACT_STAYS_FINDINGS);
  });

  it('carries a block, and treats an empty plan as nothing to confirm', () => {
    expect(previewView({ ...preview, blocked: 'journal unreadable' }).blocked).toBe('journal unreadable');
    for (const bad of [null, {}, { plan: {} }, { plan: { entry: { targets: [] } } }]) {
      const v = previewView(bad);
      expect(v.ok).toBe(false);
      expect(v.blocked).not.toBe('');
    }
  });
});

describe('lib/maintenance / redaction outcome', () => {
  it('a finished redaction lists every step done', () => {
    const v = outcomeView(allDone);
    expect(v.done).toBe(true);
    expect(v.message).toBe(UI.REDACT_DONE);
    expect(v.steps.map((s) => s.name)).toEqual(Object.values(REDACT_STEP));
  });

  // 🔒 Fail closed, every way it can be read wrong.
  it('is not done unless the backend says so and every step is done', () => {
    expect(outcomeView({ ...allDone, done: false }).done).toBe(false);
    expect(outcomeView({ ...allDone, done: 'true' }).done).toBe(false);
    const missing = { ...allDone, steps: allDone.steps.slice(0, 4) };
    const m = outcomeView(missing);
    expect(m.done).toBe(false);
    expect(m.steps[4].tone).toBe(REDACT_STEP_STATE.NOT_REACHED);
    const odd = { ...allDone, steps: allDone.steps.map((s, i) => (i === 2 ? { ...s, state: 'later_state' } : s)) };
    const o = outcomeView(odd);
    expect(o.done).toBe(false);
    expect(o.steps[2].tone).toBe(REDACT_STEP_STATE.NOT_REACHED);
    expect(o.steps[2].stateLabel).toContain('later_state');
  });

  it('a stopped redaction names the failed step and says how to finish it', () => {
    const steps = allDone.steps.map((s, i) =>
      i === 3
        ? { ...s, state: REDACT_STEP_STATE.FAILED, error: 'snapshot unreadable' }
        : i > 3
          ? { name: s.name, state: REDACT_STEP_STATE.NOT_REACHED }
          : s,
    );
    const v = outcomeView({ entry: 4, done: false, steps });
    expect(v.done).toBe(false);
    expect(v.message).toBe(UI.REDACT_UNFINISHED);
    expect(v.steps[3]).toMatchObject({ tone: REDACT_STEP_STATE.FAILED, error: 'snapshot unreadable' });
    expect(v.entry).toBe(4);
  });

  it('a call that failed says so with the error', () => {
    expect(redactionFailure(new Error('busy')).error).toBe('busy');
    expect(redactionFailure('busy').done).toBe(false);
    expect(outcomeView(null).ok).toBe(false);
  });
});

describe('lib/maintenance / redacted event lines', () => {
  it('says when and why from the ledger, and says so when there is no ledger', () => {
    expect(redactedLine({ redaction: { at: '2026-11-05T09:00:00Z', reason: 'erasure' } })).toBe(
      'Redacted 2026-11-05: erasure',
    );
    expect(redactedLine({})).toBe(UI.REDACTED_NO_LEDGER);
    expect(pendingLine({ event: 7, at: '2026-11-05T09:00:00Z', reason: 'erasure' })).toBe(
      'Event 7, started 2026-11-05: erasure',
    );
  });
});
