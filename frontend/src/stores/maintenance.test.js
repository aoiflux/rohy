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
  compactCase: vi.fn(async () => {
    state.seals += 1;
    if (state.failSeal) throw new Error('seal failed');
  }),
  forensicStatus: vi.fn(async () => {
    state.postureReads += 1;
    return { available: true, compacted: true, roots: { snapshot: state.snapshot } };
  }),
  verifyImage: vi.fn(async () => ({ digest: 'match', roots_checked: true })),
  // Captures the listeners the store attaches, so a backend event can be delivered by hand.
  on: vi.fn((channel, handler) => {
    handlers[channel] = handler;
    return () => {};
  }),
}));

const handlers = {};

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
    seals: 0,
    failSeal: false,
    postureReads: 0,
    snapshot: 'aa',
  });
  maintenance.reset();
});

describe('compact', () => {
  it('reports a seal that happened, even when refreshing the report afterwards fails', async () => {
    // 🔒 The seal moved the fingerprint. Saying it failed would send the analyst to seal again —
    // moving it a second time — and hide the fingerprint that now needs keeping.
    state.failCheck = true;
    expect(await maintenance.compact()).toBe(true);
    expect(state.seals).toBe(1);
    expect(get(maintenance).running).toBe('');
    expect(get(maintenance).lastResult).toBe('compact');
    expect(get(maintenance).error).toContain('backend unavailable');
  });

  it('a seal that failed is a failure', async () => {
    state.failSeal = true;
    expect(await maintenance.compact()).toBe(false);
    expect(get(maintenance).error).toContain('seal failed');
    expect(get(maintenance).running).toBe('');
  });

  // The drawer drops a custody report when this moves: the report described the image the seal
  // replaced. 🔒 A failed call counts too — a seal whose fingerprint could not be recorded still
  // compacted, and the image moved.
  it('counts every seal it asked for, failed ones included', async () => {
    expect(get(maintenance).seals).toBe(0);
    await maintenance.compact();
    expect(get(maintenance).seals).toBe(1);
    state.failSeal = true;
    await maintenance.compact();
    expect(get(maintenance).seals).toBe(2);
  });

  it('a pass that is refused because another runs asks for no seal', async () => {
    const first = maintenance.compact();
    expect(await maintenance.compact()).toBe(false);
    await first;
    expect(get(maintenance).seals).toBe(1);
  });
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

describe('the seal after an ingest', () => {
  // The seal happens in the ingest pipeline, outside this store, and changes what the store shows.
  const flush = () => new Promise((r) => setTimeout(r, 0));

  it('re-reads the posture and drops the image verdict when the case was sealed', async () => {
    await maintenance.loadForensics();
    await maintenance.verify();
    expect(get(maintenance).image).not.toBeNull();

    state.snapshot = 'bb';
    handlers['case:sealed']({ state: 'sealed', snapshot: 'bb' });
    await flush();

    const s = get(maintenance);
    expect(s.forensics.roots.snapshot).toBe('bb');
    // 🔒 A verdict about the image the seal replaced must not read as current.
    expect(s.image).toBeNull();
  });

  it('does nothing when the seal changed nothing', async () => {
    await maintenance.loadForensics();
    await maintenance.verify();
    const reads = state.postureReads;

    handlers['case:sealed']({ state: 'unchanged', snapshot: 'aa' });
    await flush();

    expect(state.postureReads).toBe(reads);
    expect(get(maintenance).image).not.toBeNull();
  });
});
