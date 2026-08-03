import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

// The API is stubbed so the interesting behaviours — one pass at a time, and re-checking after a
// fix — can be driven without a backend. Both are things a wrong answer makes invisible: a second
// pass the backend silently refuses, or a report that keeps asking for a repair already done.

const state = {
  report: { findings: [], counts: {}, deep: false },
  status: { total: 10, current: 10, stale: 0 },
  failCheck: false,
  failBackfill: false,
  checks: 0,
  backfills: 0,
  repairs: 0,
  cancels: 0,
  /** Resolves the next backfill manually, so "a pass is running" can be observed. */
  gate: null,
};

vi.mock('../lib/api/index.js', () => ({
  correlationKeyStatus: vi.fn(async () => state.status),
  checkIntegrity: vi.fn(async (deep) => {
    state.checks += 1;
    if (state.failCheck) throw new Error('backend unavailable');
    return { ...state.report, deep };
  }),
  backfillCorrelationKeys: vi.fn(async () => {
    state.backfills += 1;
    if (state.gate) await state.gate;
    if (state.failBackfill) throw new Error('backfill failed');
    return { examined: 10, projected: 10 };
  }),
  repairRelationIndex: vi.fn(async () => {
    state.repairs += 1;
    return 3;
  }),
  rebuildIndexes: vi.fn(async () => undefined),
  cancelMaintenance: vi.fn(async () => {
    state.cancels += 1;
  }),
}));

const { maintenance } = await import('./maintenance.js');

beforeEach(() => {
  Object.assign(state, {
    report: { findings: [], counts: {}, deep: false },
    status: { total: 10, current: 10, stale: 0 },
    failCheck: false,
    failBackfill: false,
    checks: 0,
    backfills: 0,
    repairs: 0,
    cancels: 0,
    gate: null,
  });
  maintenance.reset();
});

describe('initial state', () => {
  it('holds nothing until asked', () => {
    // 🔒 Everything this store fronts is proportional to the size of the case. Nothing runs on
    // its own.
    expect(get(maintenance)).toMatchObject({ status: null, report: null, running: '', error: '' });
  });
});

describe('check', () => {
  it('records the report and carries the deep flag through', async () => {
    await maintenance.check(true);
    const s = get(maintenance);
    expect(s.report.deep).toBe(true);
    expect(s.running).toBe('');
  });

  it('reports a failure rather than leaving a stale report on screen', async () => {
    await maintenance.check(false);
    state.failCheck = true;
    await maintenance.check(false);
    const s = get(maintenance);
    expect(s.error).toContain('backend unavailable');
    expect(s.running).toBe('');
  });
});

describe('one pass at a time', () => {
  it('refuses to start a second pass while one runs', async () => {
    // 🔒 The backend serialises these over one store. Two buttons that both looked enabled would
    // be a promise it refuses to keep.
    let release;
    state.gate = new Promise((r) => (release = r));

    const first = maintenance.backfill();
    expect(get(maintenance).running).toBe('backfill');

    const second = await maintenance.repair();
    expect(second).toBe(false);
    expect(state.repairs).toBe(0);

    release();
    await first;
    expect(get(maintenance).running).toBe('');
  });

  it('lets the next pass start once the first has finished', async () => {
    await maintenance.backfill();
    expect(await maintenance.repair()).toBe(true);
    expect(state.repairs).toBe(1);
  });
});

describe('re-checking after a fix', () => {
  it('re-runs the check, so the screen shows the state AFTER the repair', async () => {
    // A report left stale after its own remedy would keep asking for something already done.
    await maintenance.check(false);
    const before = state.checks;
    await maintenance.repair();
    expect(state.checks).toBeGreaterThan(before);
  });

  it('keeps the depth of the check it is replacing', async () => {
    await maintenance.check(true);
    await maintenance.repair();
    expect(get(maintenance).report.deep).toBe(true);
  });

  it('refreshes the correlation status after a backfill, but not after other fixes', async () => {
    // The backfill is the only pass that changes it, and re-reading the whole case to confirm
    // nothing changed would be a scan for no reason.
    state.status = { total: 10, current: 4, stale: 6 };
    await maintenance.loadStatus();
    state.status = { total: 10, current: 10, stale: 0 };

    await maintenance.repair();
    expect(get(maintenance).status.stale).toBe(6);

    await maintenance.backfill();
    expect(get(maintenance).status.stale).toBe(0);
  });
});

describe('progress and cancellation', () => {
  it('takes progress from the event channel and clears it when the pass ends', async () => {
    maintenance.onProgress({ done: 5, total: 10 });
    expect(get(maintenance).progress).toEqual({ done: 5, total: 10 });
    await maintenance.backfill();
    expect(get(maintenance).progress).toBeNull();
  });

  it('cancels without throwing when there is nothing to cancel', () => {
    expect(() => maintenance.cancel()).not.toThrow();
    expect(state.cancels).toBe(1);
  });
});

describe('failure handling', () => {
  it('clears the running flag on failure, so the screen is not stuck', async () => {
    state.failBackfill = true;
    await maintenance.backfill();
    const s = get(maintenance);
    expect(s.running).toBe('');
    expect(s.error).toContain('backfill failed');
  });

  it('lets the error be dismissed', async () => {
    state.failBackfill = true;
    await maintenance.backfill();
    maintenance.clearError();
    expect(get(maintenance).error).toBe('');
  });
});
