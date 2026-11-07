import { describe, it, expect, afterEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { render } from 'svelte/server';
import { UI } from '../lib/consts/index.js';
import { maintenance } from '../stores/maintenance.js';
import MaintenanceView from './MaintenanceView.svelte';

// Stop on the Maintenance page.
//
// CancelMaintenance reaches only a backfill, the one pass that carries a cancellation
// (api_maintenance.go). A case check runs under context.Background() and the fixes run to the end,
// so a Stop shown beside them did nothing. Worse, a check takes no maintenance lock, so pressing
// that Stop during a check could cancel a backfill started from the Rules page instead.
//
// The API is stubbed and each pass is held open, so the page can be rendered while it runs.

function deferred() {
  let resolve = () => {};
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const gates = {
  /** @type {{promise: Promise<any>, resolve: Function}|null} */ check: null,
  /** @type {{promise: Promise<any>, resolve: Function}|null} */ backfill: null,
};

vi.mock('../lib/api/index.js', async (orig) => ({
  ...(await orig()),
  checkIntegrity: vi.fn(async (deep) => {
    await gates.check?.promise;
    return { findings: [], counts: {}, deep };
  }),
  backfillCorrelationKeys: vi.fn(async () => {
    await gates.backfill?.promise;
    return { examined: 0, projected: 0 };
  }),
  correlationKeyStatus: vi.fn(async () => ({ total: 0, current: 0, stale: 0 })),
}));

/** The pass a test started, so it can be let finish before the store is reset. */
let pending = Promise.resolve();

afterEach(async () => {
  gates.check?.resolve();
  gates.backfill?.resolve();
  await pending;
  gates.check = null;
  gates.backfill = null;
  pending = Promise.resolve();
  maintenance.reset();
});

describe('routes / MaintenanceView — Stop', () => {
  // 🔒 The owner's case. A Stop that does nothing reads as a check refusing to stop, and pressed
  // here it could stop somebody else's backfill (api_integrity.go: context.Background()).
  it('offers no Stop while a case check runs, because nothing can stop a check', () => {
    gates.check = deferred();
    pending = maintenance.check(false);
    expect(get(maintenance).running).toBe('check');

    const { body } = render(MaintenanceView);
    // The page is mid-check: the banner says so, and only Stop is withheld.
    expect(body).toContain(UI.MAINT_RUNNING.check);
    expect(body).not.toContain(UI.MAINT_CANCEL);
  });

  it('offers Stop while a backfill runs, the one pass CancelMaintenance reaches', () => {
    gates.backfill = deferred();
    pending = maintenance.backfill();
    expect(get(maintenance).running).toBe('backfill');
    maintenance.onState({ pass: 'backfill', stoppable: true, seq: 1 });

    const { body } = render(MaintenanceView);
    expect(body).toContain(UI.MAINT_RUNNING.backfill);
    expect(body).toContain(UI.MAINT_CANCEL);
  });

  // 🔒 Stop reaches the pass over the events, not the seal after it. A Stop shown through the seal
  // did nothing, which reads as a seal refusing to stop.
  it('withdraws Stop for the seal after the pass, and keeps showing the backfill', () => {
    gates.backfill = deferred();
    pending = maintenance.backfill();
    maintenance.onState({ pass: 'backfill', stoppable: true, seq: 1 });
    maintenance.onState({ pass: 'backfill', stoppable: false, seq: 2 });

    const { body } = render(MaintenanceView);
    expect(body).toContain(UI.MAINT_RUNNING.backfill);
    expect(body).not.toContain(UI.MAINT_CANCEL);
  });

  it('shows a backfill started on the Rules page, with Stop for it', () => {
    maintenance.onState({ pass: 'backfill', stoppable: true, seq: 1 });
    expect(get(maintenance).running).toBe('backfill');

    const { body } = render(MaintenanceView);
    expect(body).toContain(UI.MAINT_RUNNING.backfill);
    expect(body).toContain(UI.MAINT_CANCEL);
  });

  // The owner's case again, with the Rules page backfill running beside the check: Stop would reach
  // that backfill, not the check this page shows.
  it('offers no Stop during a check even while a Rules page backfill can be stopped', () => {
    gates.check = deferred();
    pending = maintenance.check(false);
    maintenance.onState({ pass: 'backfill', stoppable: true, seq: 1 });

    const { body } = render(MaintenanceView);
    expect(body).toContain(UI.MAINT_RUNNING.check);
    expect(body).not.toContain(UI.MAINT_CANCEL);
  });
});
