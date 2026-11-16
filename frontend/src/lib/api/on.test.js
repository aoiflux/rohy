import { describe, it, expect, vi } from 'vitest';
import { on } from './index.js';

// A stand-in for the runtime's event registry: each On adds one listener and returns the function
// that removes that listener alone, as the runtime's does.
const events = vi.hoisted(() => {
  const listeners = new Map();
  return {
    listeners,
    On(name, cb) {
      const list = listeners.get(name) ?? [];
      list.push(cb);
      listeners.set(name, list);
      return () => listeners.set(name, listeners.get(name).filter((l) => l !== cb));
    },
    dispatch(name, data) {
      for (const cb of listeners.get(name) ?? []) cb({ name, data, sender: '' });
    },
  };
});
vi.mock('@wailsio/runtime', async (orig) => ({ ...(await orig()), Events: { On: events.On } }));

describe('on()', () => {
  it('hands the handler the data the backend emitted, not the runtime wrapper', () => {
    const seen = [];
    on('ingest:progress', (d) => seen.push(d));
    events.dispatch('ingest:progress', { done: 3, total: 9 });
    events.dispatch('ingest:progress', null);
    expect(seen).toEqual([{ done: 3, total: 9 }, null]);
  });

  it('unsubscribes its own handler and leaves every other one listening', () => {
    const a = [];
    const b = [];
    const offA = on('maintenance:state', (d) => a.push(d));
    on('maintenance:state', (d) => b.push(d));
    offA();
    events.dispatch('maintenance:state', 1);
    expect(a).toEqual([]);
    expect(b).toEqual([1]);
  });
});
