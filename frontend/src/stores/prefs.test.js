import { describe, it, expect, vi, beforeEach } from 'vitest';
import { PREFS } from '../lib/consts/index.js';

// Node's own localStorage throws without a backing file, so each test gets a plain one.
function storage(initial = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k) => (data.has(k) ? data.get(k) : null),
    setItem: (k, v) => data.set(k, String(v)),
    removeItem: (k) => data.delete(k),
    data,
  };
}

async function fresh(initial) {
  const store = storage(initial);
  vi.stubGlobal('localStorage', store);
  vi.resetModules();
  const { prefs } = await import('./prefs.js');
  return { prefs, store };
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

describe('prefs: format on save', () => {
  it('is off until the author turns it on', async () => {
    const { prefs } = await fresh();
    expect(prefs.current().formatOnSave).toBe(false);
  });

  it('survives a restart once turned on', async () => {
    const first = await fresh();
    first.prefs.setFormatOnSave(true);
    const saved = first.store.data.get(PREFS.KEY);
    const { prefs } = await fresh({ [PREFS.KEY]: saved });
    expect(prefs.current().formatOnSave).toBe(true);
  });

  it('ignores a stored value that is not a boolean', async () => {
    const { prefs } = await fresh({ [PREFS.KEY]: JSON.stringify({ formatOnSave: 'yes' }) });
    expect(prefs.current().formatOnSave).toBe(false);
  });
});
