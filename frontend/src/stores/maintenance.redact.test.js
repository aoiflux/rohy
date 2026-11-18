import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { REDACT_STEP, REDACT_STEP_STATE } from '../lib/consts/index.js';

// Redaction through the maintenance store: what moves when, so every view
// holding a copy of the event's content knows to drop it.

const done = (entry) => ({
  entry,
  event: 7,
  done: true,
  steps: Object.values(REDACT_STEP).map((name) => ({ name, state: REDACT_STEP_STATE.DONE })),
});

const state = { pending: [], refuse: false, finished: [], checks: 0, redacted: [] };

vi.mock('../lib/api/index.js', () => ({
  redactEvent: vi.fn(async (req) => {
    if (state.refuse) throw new Error('a redaction cannot start while an ingest is running');
    state.redacted.push(req);
    return done(1);
  }),
  pendingRedactions: vi.fn(async () => state.pending),
  finishRedaction: vi.fn(async (id) => {
    state.finished.push(id);
    return done(id);
  }),
  checkIntegrity: vi.fn(async (deep) => {
    state.checks += 1;
    if (state.checkGate) await state.checkGate;
    return { findings: [], deep };
  }),
  forensicStatus: vi.fn(async () => ({ available: true })),
  on: vi.fn(),
}));

const { maintenance } = await import('./maintenance.js');

beforeEach(() => {
  maintenance.reset();
  Object.assign(state, { pending: [], refuse: false, finished: [], checks: 0, redacted: [], checkGate: null });
});

describe('stores/maintenance redaction', () => {
  it('a redaction that ran moves the marks views drop their copies on', async () => {
    const out = await maintenance.redact(7, 'erasure request');
    expect(out.done).toBe(true);
    expect(state.redacted).toEqual([{ event: 7, reason: 'erasure request' }]);
    const s = get(maintenance);
    expect(s.redactions).toBe(1);
    expect(s.seals).toBe(1);
    expect(s.running).toBe('');
    expect(s.pending).toEqual([]);
  });

  it('a redaction that did not start moves nothing and says why', async () => {
    state.refuse = true;
    const out = await maintenance.redact(7, 'erasure');
    expect(out.ok).toBe(false);
    expect(out.error).toContain('ingest is running');
    const s = get(maintenance);
    expect(s.redactions).toBe(0);
    expect(s.seals).toBe(0);
    expect(s.running).toBe('');
  });

  it('finishing runs every pending redaction and re-checks the case', async () => {
    state.pending = [{ id: 3 }, { id: 5 }];
    const outs = await maintenance.finishRedactions();
    expect(state.finished).toEqual([3, 5]);
    expect(outs.every((o) => o.done)).toBe(true);
    expect(state.checks).toBe(1);
    const s = get(maintenance);
    expect(s.redactions).toBe(1);
    expect(s.lastResult).toBe('finish_redaction');
  });

  // 🔒 A deep re-check verifies the image under the backend's maintenance lock, so the page stays
  // busy until it returns: a button left enabled meanwhile would be refused.
  it('stays busy until a deep re-check after finishing returns', async () => {
    await maintenance.check(true);
    state.pending = [{ id: 3 }];
    let release;
    state.checkGate = new Promise((r) => (release = r));
    const finishing = maintenance.finishRedactions();
    await vi.waitFor(() => expect(state.checks).toBe(2));
    expect(get(maintenance).running).toBe('check');
    expect(get(maintenance).lastResult).toBe('finish_redaction');
    state.checkGate = null;
    release();
    await finishing;
    const s = get(maintenance);
    expect(s.running).toBe('');
    expect(s.report.deep).toBe(true);
  });
});
