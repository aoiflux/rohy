import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { UI } from '../lib/consts/index.js';

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
  correlationKeyStatus: vi.fn(async () => {
    state.statusReads += 1;
    return state.status;
  }),
  checkIntegrity: vi.fn(async (deep) => {
    state.checks += 1;
    if (state.checkGate) await state.checkGate;
    if (state.failCheck) throw new Error('backend unavailable');
    return { ...state.report, deep };
  }),
  backfillCorrelationKeys: vi.fn(async () => {
    state.backfills += 1;
    if (state.gate) await state.gate;
    if (state.failBackfill) throw new Error('backfill failed');
    if (state.backfillThrows) throw state.backfillThrows;
    // What the seal after the pass did, when the test says it ran one.
    return { examined: 10, projected: 10, ...state.backfillSeal };
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
    if (state.failPosture) throw new Error('posture unreadable');
    return { available: true, compacted: true, roots: { snapshot: state.snapshot } };
  }),
  verifyImage: vi.fn(async () => ({ digest: 'match', roots_checked: true })),
  publishCheckpoint: vi.fn(async () => ({ sealed: false })),
  checkpoints: vi.fn(async () => []),
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
    statusReads: 0,
    backfillSeal: {},
    backfillThrows: null,
    checkGate: null,
    failPosture: false,
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

// 🔒 A deep re-check verifies the image, and the backend holds its maintenance lock for that step
// (VerifyImage). The page stays busy until the re-check returns, so no button looks enabled that the
// backend would refuse. A quick re-check takes no lock and leaves the page free.
describe('the re-check after a pass', () => {
  /** holdChecks makes the next integrity checks wait; the function it returns lets them go. */
  const holdChecks = () => {
    let release;
    state.checkGate = new Promise((r) => (release = r));
    return () => {
      state.checkGate = null;
      release();
    };
  };

  const passes = {
    repair: () => maintenance.repair(),
    backfill: () => maintenance.backfill(),
    compact: () => maintenance.compact(),
    publish: () => maintenance.publishCheckpoint(),
  };

  for (const [name, start] of Object.entries(passes)) {
    it(`keeps the page busy until a deep re-check after ${name} returns`, async () => {
      await maintenance.check(true);
      const before = state.checks;
      const release = holdChecks();
      const pass = start();
      await vi.waitFor(() => expect(state.checks).toBe(before + 1));
      expect(get(maintenance).running).toBe('check');
      expect(await maintenance.repair()).toBe(false);
      release();
      await pass;
      expect(get(maintenance).running).toBe('');
      expect(get(maintenance).report.deep).toBe(true);
    });
  }

  it('leaves the page free during a quick re-check, which takes no lock', async () => {
    await maintenance.check(false);
    const before = state.checks;
    const release = holdChecks();
    const pass = maintenance.repair();
    await vi.waitFor(() => expect(state.checks).toBe(before + 1));
    expect(get(maintenance).running).toBe('');
    release();
    await pass;
  });

  it('clears the busy flag when a deep re-check fails', async () => {
    await maintenance.check(true);
    state.failCheck = true;
    await maintenance.repair();
    expect(get(maintenance).running).toBe('');
    expect(get(maintenance).error).toContain('backend unavailable');
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

  // 🔒 A read that failed neither started nor ended a pass. Freeing the page on it would enable
  // every button while the pass still holds the backend's lock.
  it('a read that fails while a pass runs shows why and leaves the page busy', async () => {
    let release;
    state.gate = new Promise((r) => (release = r));
    const pass = maintenance.backfill();
    state.failPosture = true;
    await maintenance.loadForensics();
    expect(get(maintenance).running).toBe('backfill');
    expect(get(maintenance).error).toContain('posture unreadable');
    release();
    await pass;
    expect(get(maintenance).running).toBe('');
  });

  // The same read failing during a compact's refresh must not free the page before its deep
  // re-check, which holds the lock.
  it('keeps a deep re-check busy when the posture read before it fails', async () => {
    await maintenance.check(true);
    state.failPosture = true;
    let release;
    state.checkGate = new Promise((r) => (release = r));
    const before = state.checks;
    const pass = maintenance.compact();
    await vi.waitFor(() => expect(state.checks).toBe(before + 1));
    expect(get(maintenance).running).toBe('check');
    state.checkGate = null;
    release();
    expect(await pass).toBe(true);
    expect(get(maintenance).running).toBe('');
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

describe('the seal after a backfill', () => {
  // A backfill that rewrote events seals them before it returns, and says so on its result rather
  // than on case:sealed, which the ingestion store reads as the ingest's own seal.
  const flush = () => new Promise((r) => setTimeout(r, 0));
  const verified = async () => {
    await maintenance.loadForensics();
    await maintenance.verify();
    expect(get(maintenance).image).not.toBeNull();
  };

  it('drops the image verdict, counts the seal and re-reads the posture when the backfill sealed', async () => {
    await verified();
    state.snapshot = 'bb';
    state.backfillSeal = { sealed: { state: 'sealed', snapshot: 'bb' } };

    expect(await maintenance.backfill()).toBe(true);
    await flush();

    const s = get(maintenance);
    // 🔒 A verdict about the image the seal replaced must not read as current.
    expect(s.image).toBeNull();
    expect(s.seals).toBe(1);
    expect(s.forensics.roots.snapshot).toBe('bb');
    expect(s.lastResult).toBe('backfill_sealed');
    expect(s.running).toBe('');
  });

  it('shows a seal that did not finish cleanly beside the result, not instead of it', async () => {
    state.backfillSeal = {
      sealed: { state: 'sealed' },
      seal_error: 'the case was sealed, but its fingerprint could not be recorded in the history: x',
    };

    expect(await maintenance.backfill()).toBe(true);

    const s = get(maintenance);
    expect(s.lastResult).toBe('backfill_sealed');
    expect(s.error).toContain('could not be recorded');
    expect(s.seals).toBe(1);
    expect(state.statusReads).toBe(1);
  });

  it('counts a backfill that failed as it ran as a seal, but not one that was refused', async () => {
    await verified();
    state.backfillThrows = Object.assign(new Error('backfill failed'), { code: 'persistence_error' });
    await maintenance.backfill();
    let s = get(maintenance);
    expect(s.seals).toBe(1);
    expect(s.image).toBeNull();
    expect(s.error).toContain('backfill failed');

    await verified();
    state.backfillThrows = Object.assign(new Error('a redaction is running'), { code: 'case_busy' });
    await maintenance.backfill();
    s = get(maintenance);
    expect(s.seals).toBe(1);
    expect(s.image).not.toBeNull();
  });

  it('moves nothing when the backfill sealed nothing', async () => {
    for (const seal of [{}, { sealed: { state: 'unavailable' } }]) {
      await verified();
      const reads = state.postureReads;
      state.backfillSeal = seal;

      await maintenance.backfill();
      await flush();

      const s = get(maintenance);
      expect(s.seals).toBe(0);
      expect(s.image).not.toBeNull();
      expect(state.postureReads).toBe(reads);
      expect(s.lastResult).toBe('backfill');
    }
  });

  it('names a stopped pass as stopped, and says when it sealed what it had done', async () => {
    state.backfillSeal = { cancelled: true };
    await maintenance.backfill();
    expect(get(maintenance).lastResult).toBe('backfill_partial');

    state.backfillSeal = { cancelled: true, sealed: { state: 'sealed' } };
    await maintenance.backfill();
    expect(get(maintenance).lastResult).toBe('backfill_partial_sealed');
    expect(get(maintenance).seals).toBe(1);
  });

  it('noteSeal from another view drops the image verdict', async () => {
    // The rules page runs the backfill without this store and hands it the seal.
    await verified();
    maintenance.noteSeal({ state: 'unchanged' });
    expect(get(maintenance).image).not.toBeNull();
    expect(get(maintenance).seals).toBe(0);

    maintenance.noteSeal({ state: 'sealed' });
    expect(get(maintenance).image).toBeNull();
    expect(get(maintenance).seals).toBe(1);
  });
});

describe('the done banner after a backfill', () => {
  it('has copy for every result a backfill can end in', () => {
    for (const key of ['backfill', 'backfill_sealed', 'backfill_partial', 'backfill_partial_sealed']) {
      expect(UI.MAINT_DONE[key], key).toBeTruthy();
    }
    expect(UI.MAINT_DONE.backfill_partial_sealed).toContain(UI.MAINT_DONE.backfill_partial);
  });
});
