import { describe, it, expect } from 'vitest';
import {
  OUTCOME,
  summarise,
  recreatable,
  toggle,
  unresolvedNodes,
  movedNodes,
  when,
  title,
} from './snapshots.js';

const plan = (over = {}) => ({
  snapshot_id: 'snap-20260727-141133.000',
  nodes_applied: 3,
  nodes_moved: 0,
  nodes_unresolved: 0,
  relations_applied: 2,
  relations_recreatable: 0,
  relations_unresolved: 0,
  reingested: false,
  nodes: [],
  relations: [],
  ...over,
});

describe('summarise', () => {
  it('lists only the buckets that hold something', () => {
    const got = summarise(plan());
    expect(got.rows.map((r) => r.kind)).toEqual(['nodes_applied', 'relations_applied']);
    expect(got.empty).toBe(false);
  });

  it('calls out a re-ingest, because it changes what the result means', () => {
    // Not an error — a restore onto a re-ingested case is exactly what hash-keying makes
    // possible — but it is not a number either, so it reads differently from a count.
    const got = summarise(plan({ reingested: true, nodes_moved: 3, nodes_applied: 0 }));
    expect(got.notes.map((n) => n.kind)).toContain('reingested');
    expect(got.rows.find((r) => r.kind === 'nodes_moved').count).toBe(3);
  });

  it('flags anything that could not be resolved', () => {
    const got = summarise(plan({ nodes_unresolved: 2 }));
    expect(got.notes.map((n) => n.kind)).toContain('unresolved');
  });

  it('reports how many edges are on offer, separately from the counts', () => {
    expect(summarise(plan({ relations_recreatable: 4 })).offered).toBe(4);
  });

  it('is empty for a plan that would do nothing, and survives a null one', () => {
    const nothing = summarise(plan({ nodes_applied: 0, relations_applied: 0 }));
    expect(nothing.empty).toBe(true);
    expect(summarise(null)).toEqual({ rows: [], notes: [], offered: 0, empty: true });
  });
});

describe('recreatable', () => {
  const withRelations = plan({
    relations: [
      { snapshot_id: 1, outcome: OUTCOME.APPLIED },
      { snapshot_id: 2, outcome: OUTCOME.RECREATABLE },
      { snapshot_id: 3, outcome: OUTCOME.UNRESOLVED },
      { snapshot_id: 4, outcome: OUTCOME.RECREATABLE },
    ],
  });

  it('offers only the edges whose events survived but whose link did not', () => {
    expect(recreatable(withRelations).map((r) => r.snapshot_id)).toEqual([2, 4]);
  });

  it('is empty rather than throwing on a plan with no relations', () => {
    expect(recreatable(plan())).toEqual([]);
    expect(recreatable(null)).toEqual([]);
  });
});

describe('toggle', () => {
  it('adds and removes, returning a new set', () => {
    const a = new Set([1]);
    const b = toggle(a, 2);
    expect([...b].sort()).toEqual([1, 2]);
    expect(a.has(2)).toBe(false);
    expect(toggle(b, 1).has(1)).toBe(false);
  });

  it('starts from nothing', () => {
    expect([...toggle(undefined, 7)]).toEqual([7]);
  });
});

describe('unresolvedNodes and movedNodes', () => {
  const withNodes = plan({
    nodes: [
      { snapshot_id: 1, outcome: OUTCOME.APPLIED },
      { snapshot_id: 2, outcome: OUTCOME.UNRESOLVED, descriptor: '4625 HOST-A' },
      { snapshot_id: 3, outcome: OUTCOME.MOVED, live_id: 41 },
    ],
  });

  it('names what is gone rather than only counting it', () => {
    // A bare hash tells the reader nothing; the descriptor is what makes an orphan meaningful.
    const gone = unresolvedNodes(withNodes);
    expect(gone).toHaveLength(1);
    expect(gone[0].descriptor).toBe('4625 HOST-A');
  });

  it('separates the nodes that resolved to a different id', () => {
    expect(movedNodes(withNodes).map((n) => n.live_id)).toEqual([41]);
  });

  it('survives a plan with no nodes', () => {
    expect(unresolvedNodes(null)).toEqual([]);
    expect(movedNodes(plan())).toEqual([]);
  });
});

describe('title and when', () => {
  it('prefers the analyst’s own label', () => {
    expect(title({ label: 'before the reset', taken_at: '2026-07-27T14:11:33Z' })).toBe('before the reset');
  });

  it('falls back to when it was taken, never to a bare id', () => {
    // The id is a filename, not a name.
    expect(title({ id: 'snap-20260727-141133.000', taken_at: '2026-07-27T14:11:33Z' }))
      .toBe('2026-07-27 14:11:33');
  });

  it('uses the id only when there is nothing else at all', () => {
    expect(title({ id: 'snap-x' })).toBe('snap-x');
    expect(title(null)).toBe('');
  });

  it('renders an unreadable timestamp as nothing rather than as Invalid Date', () => {
    expect(when('nonsense')).toBe('');
    expect(title({ id: 'snap-x', taken_at: 'nonsense' })).toBe('snap-x');
  });
});
