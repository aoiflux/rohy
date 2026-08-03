import { describe as suite, it, expect } from 'vitest';
import { intensity, describe, lanesFor, cellTitle, short, totals, sliceToView } from './heatmap.js';

const lanes = () => [
  { key: 'brute-force', total: 6, counts: [1, 2, 3] },
  { key: 'log-cleared', total: 3, counts: [0, 3, 0] },
];

suite('intensity', () => {
  it('scales linearly against the matrix maximum', () => {
    expect(intensity(5, 10)).toBe(0.5);
    expect(intensity(10, 10)).toBe(1);
  });

  it('treats an empty cell as empty, not as the palest shade', () => {
    // 🔒 "Nothing happened here" and "almost nothing happened here" are different findings and
    // must not be a few percent of lightness apart.
    expect(intensity(0, 10)).toBe(0);
  });

  it('never exceeds 1, so a stale max cannot produce an out-of-range colour', () => {
    expect(intensity(50, 10)).toBe(1);
  });

  it('is zero rather than NaN or Infinity for a degenerate maximum', () => {
    expect(intensity(3, 0)).toBe(0);
    expect(intensity(3, undefined)).toBe(0);
    expect(intensity(undefined, 10)).toBe(0);
  });
});

suite('describe', () => {
  it('reports what could not be placed', () => {
    const got = describe({ total: 10, placed: 7, undated: 3 });
    expect(got.notes).toEqual([{ kind: 'undated', count: 3 }]);
    expect(got.empty).toBe(false);
  });

  it('reports what fell outside the requested window', () => {
    const got = describe({ total: 10, placed: 4, undated: 1, outside: 5 });
    expect(got.notes.map((n) => n.kind)).toEqual(['undated', 'outside']);
  });

  it('surfaces a discrepancy rather than absorbing it into the placed count', () => {
    // If the parts do not add up, this reader and the backend disagree about what happened to a
    // relation. Better an explicit unknown than a number quietly one short.
    const got = describe({ total: 10, placed: 4, undated: 1, outside: 1 });
    expect(got.notes.find((n) => n.kind === 'unaccounted')).toEqual({ kind: 'unaccounted', count: 4 });
  });

  it('says nothing when everything was placed', () => {
    expect(describe({ total: 5, placed: 5 }).notes).toEqual([]);
  });

  it('is empty for a summary with nothing placed, and survives a null one', () => {
    expect(describe({ total: 3, placed: 0, undated: 3 }).empty).toBe(true);
    expect(describe(null)).toEqual({ empty: true, notes: [] });
  });
});

suite('lanesFor', () => {
  it('keeps the busiest and reports how many it dropped', () => {
    // 🔒 A matrix silently showing eight of twenty rules reads as a case with eight rules.
    const got = lanesFor(lanes(), 1);
    expect(got.lanes).toHaveLength(1);
    expect(got.lanes[0].key).toBe('brute-force');
    expect(got.hidden).toBe(1);
  });

  it('is a pass-through under the limit', () => {
    const all = lanes();
    expect(lanesFor(all, 10)).toEqual({ lanes: all, hidden: 0 });
    expect(lanesFor(all, 0).lanes).toBe(all);
  });

  it('survives no lanes at all', () => {
    expect(lanesFor(null, 5)).toEqual({ lanes: [], hidden: 0 });
  });
});

suite('totals', () => {
  it('sums every lane per bucket', () => {
    // Derived from the lanes rather than read off the buckets, so the strip and the matrix can
    // never show different numbers for the same column.
    expect(totals(lanes(), 3)).toEqual([1, 5, 3]);
  });

  it('pads to the bucket count when a lane is short', () => {
    expect(totals([{ counts: [1] }], 3)).toEqual([1, 0, 0]);
  });

  it('is empty for no lanes', () => {
    expect(totals(null, 2)).toEqual([0, 0]);
    expect(totals(lanes(), 0)).toEqual([]);
  });
});

suite('cellTitle and short', () => {
  it('puts the number in reach, because colour cannot express one', () => {
    const title = cellTitle({ key: 'brute-force' }, { start: '2026-06-01T09:00:00Z', end: '2026-06-01T09:01:00Z' }, 4);
    expect(title).toContain('brute-force');
    expect(title).toContain('4');
    expect(title).toContain('06-01 09:00:00');
  });

  it('renders an unreadable instant as nothing rather than as Invalid Date', () => {
    expect(short('nonsense')).toBe('');
    expect(short(undefined)).toBe('');
    expect(cellTitle({ key: 'x' }, null, 0)).not.toContain('Invalid');
  });
});

suite('sliceToView', () => {
  const counts = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

  it('cuts to the zoom window', () => {
    expect(sliceToView(counts, { start: 0.2, end: 0.5 })).toEqual([3, 4, 5]);
  });

  it('is a pass-through at full extent', () => {
    expect(sliceToView(counts, { start: 0, end: 1 })).toEqual(counts);
    expect(sliceToView(counts, null)).toBe(counts);
  });

  it('keeps at least one column for a window narrower than a bucket', () => {
    // Otherwise a deep zoom would blank the strip while the histogram beneath it still drew.
    expect(sliceToView(counts, { start: 0.51, end: 0.512 })).toHaveLength(1);
  });

  it('clamps a window that runs past either end', () => {
    expect(sliceToView(counts, { start: -1, end: 2 })).toEqual(counts);
  });

  it('ignores an inverted or non-finite window rather than returning nothing', () => {
    expect(sliceToView(counts, { start: 0.8, end: 0.2 })).toBe(counts);
    expect(sliceToView(counts, { start: NaN, end: NaN })).toBe(counts);
  });

  it('survives an empty array', () => {
    expect(sliceToView([], { start: 0.2, end: 0.5 })).toEqual([]);
  });
});
