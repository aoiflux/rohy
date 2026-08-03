import { describe as suite, it, expect } from 'vitest';
import { build, visibleAt, fracToTime, timeToFrac, advance, describe } from './replay.js';

const T = (s) => new Date(Date.UTC(2026, 5, 1, 9, 0, s)).toISOString();
const at = (s) => Date.UTC(2026, 5, 1, 9, 0, s);

const node = (id, secs) => ({
  event: { id, timestamp: secs === null ? undefined : T(secs) },
});

const canvas = () => ({
  1: node(1, 0),
  2: node(2, 10),
  3: node(3, 20),
  4: node(4, null), // a catalogue row: no timestamp at all
});

const edges = () => [
  { id: 10, from: 1, to: 2 },
  { id: 11, from: 2, to: 3 },
  { id: 12, from: 3, to: 4 }, // touches the undated row
];

suite('build', () => {
  it('orders an edge by its LATER endpoint, not by when the rule ran', () => {
    // 🔒 created_at is a few milliseconds of one afternoon. A graph animated in that order shows
    // the shape of the tooling, not the shape of the incident.
    const m = build(canvas(), edges());
    expect(m.edgeAt.get('10')).toBe(at(10));
    expect(m.edgeAt.get('11')).toBe(at(20));
  });

  it('spans the dated events', () => {
    const m = build(canvas(), edges());
    expect(m.from).toBe(at(0));
    expect(m.to).toBe(at(20));
    expect(m.dated).toBe(3);
    expect(m.total).toBe(4);
  });

  it('shows an undated node from the start rather than animating it in at t=0', () => {
    // 🔒 An event with no timestamp is not an event from the epoch. Placing it first would assert
    // it was the earliest thing that happened.
    const m = build(canvas(), edges());
    expect(m.undatedNodes).toEqual([4]);
    const first = visibleAt(m, m.from);
    expect(first.nodes.has('4')).toBe(true);
  });

  it('marks an edge with an undated endpoint approximate rather than placing it', () => {
    const m = build(canvas(), edges());
    expect(m.approximateEdges).toEqual([12]);
    // It appears once BOTH its endpoints are on screen — the earliest point at which drawing it
    // is even coherent — which here is when node 3 arrives.
    expect(m.edgeAt.get('12')).toBe(at(20));
  });

  it('shows an edge between two undated nodes from the start', () => {
    const m = build({ 1: node(1, null), 2: node(2, null) }, [{ id: 9, from: 1, to: 2 }]);
    expect(m.approximateEdges).toEqual([9]);
    expect(m.from).toBeNull();
    // With no axis at all there is nothing to scrub, but the content is still all present.
    expect(m.edgeAt.get('9')).toBe(-Infinity);
  });

  it('ignores an edge whose endpoint is not on this canvas', () => {
    const m = build(canvas(), [...edges(), { id: 99, from: 1, to: 404 }]);
    expect(m.edgeAt.has('99')).toBe(false);
  });

  it('treats an unparseable timestamp as no timestamp rather than as NaN time', () => {
    const m = build({ 1: { event: { id: 1, timestamp: 'nonsense' } } }, []);
    expect(m.undatedNodes).toEqual([1]);
    expect(m.from).toBeNull();
  });

  it('survives an empty canvas', () => {
    const m = build({}, []);
    expect(m.from).toBeNull();
    expect(m.total).toBe(0);
  });
});

suite('visibleAt', () => {
  it('reveals nodes and edges as their moment is reached', () => {
    const m = build(canvas(), edges());
    const early = visibleAt(m, at(5));
    expect([...early.nodes].sort()).toEqual(['1', '4']);
    expect(early.edges.size).toBe(0);

    const mid = visibleAt(m, at(10));
    expect(mid.nodes.has('2')).toBe(true);
    expect(mid.edges.has('10')).toBe(true);
    expect(mid.edges.has('11')).toBe(false);
  });

  it('shows everything at the end', () => {
    const m = build(canvas(), edges());
    const end = visibleAt(m, m.to);
    expect(end.nodes.size).toBe(4);
    expect(end.edges.size).toBe(3);
  });

  it('is null when replay is off, so the canvas filters nothing', () => {
    // A null result means "no replay", not "nothing visible" — the difference between the canvas
    // drawing the whole graph and drawing none of it.
    const m = build(canvas(), edges());
    expect(visibleAt(m, null)).toBeNull();
    expect(visibleAt(m, undefined)).toBeNull();
    expect(visibleAt(null, 5)).toBeNull();
  });
});

suite('fracToTime / timeToFrac', () => {
  it('round-trips a scrub position', () => {
    const m = build(canvas(), edges());
    expect(timeToFrac(m, fracToTime(m, 0.5))).toBeCloseTo(0.5, 10);
    expect(fracToTime(m, 0)).toBe(m.from);
    expect(fracToTime(m, 1)).toBe(m.to);
  });

  it('clamps a position outside 0..1', () => {
    const m = build(canvas(), edges());
    expect(fracToTime(m, -3)).toBe(m.from);
    expect(fracToTime(m, 9)).toBe(m.to);
  });

  it('is null when the graph has no time axis at all', () => {
    // 🔒 A graph of nothing but catalogue rows has nothing to scrub along, and inventing an axis
    // would be inventing times.
    const m = build({ 1: node(1, null) }, []);
    expect(fracToTime(m, 0.5)).toBeNull();
    expect(timeToFrac(m, 123)).toBeNull();
  });

  it('collapses a zero span rather than dividing by it', () => {
    const m = build({ 1: node(1, 5), 2: node(2, 5) }, []);
    expect(fracToTime(m, 0.7)).toBe(at(5));
    expect(timeToFrac(m, at(5))).toBe(0);
  });
});

suite('advance', () => {
  const m = () => build(canvas(), edges());

  it('scales playback to the span, so any case plays in about the same wall-clock time', () => {
    // Wall-clock playback would take three weeks for a three-week case and be over before it was
    // seen for a four-second one.
    const model = m();
    const span = model.to - model.from;
    const { t } = advance(model, model.from, 1000, 1, 10000);
    expect(t - model.from).toBeCloseTo(span / 10, 6);
  });

  it('doubles the step at 2×', () => {
    const model = m();
    const one = advance(model, model.from, 1000, 1, 10000).t;
    const two = advance(model, model.from, 1000, 2, 10000).t;
    expect(two - model.from).toBeCloseTo(2 * (one - model.from), 6);
  });

  it('stops exactly at the end rather than overshooting', () => {
    const model = m();
    const { t, done } = advance(model, model.from, 60000, 1, 1000);
    expect(t).toBe(model.to);
    expect(done).toBe(true);
  });

  it('starts from the beginning when nothing has played yet', () => {
    const model = m();
    expect(advance(model, null, 1, 1, 10000).t).toBeGreaterThan(model.from);
  });

  it('finishes immediately on a graph with no axis', () => {
    expect(advance(build({ 1: node(1, null) }, []), null, 100, 1, 1000).done).toBe(true);
  });

  it('completes a zero-span graph instead of looping forever at one instant', () => {
    const model = build({ 1: node(1, 5), 2: node(2, 5) }, []);
    expect(advance(model, model.from, 16, 1, 1000).done).toBe(true);
  });
});

suite('describe', () => {
  it('reports what cannot be placed, so a node that never animates does not look like a bug', () => {
    const got = describe(build(canvas(), edges()));
    expect(got.playable).toBe(true);
    expect(got.notes).toEqual([
      { kind: 'undated', count: 1 },
      { kind: 'approximate', count: 1 },
    ]);
  });

  it('says nothing when everything is placed', () => {
    const clean = build({ 1: node(1, 0), 2: node(2, 5) }, [{ id: 1, from: 1, to: 2 }]);
    expect(describe(clean).notes).toEqual([]);
  });

  it('is unplayable when nothing carries a time', () => {
    const got = describe(build({ 1: node(1, null) }, []));
    expect(got.playable).toBe(false);
    expect(got.notes).toContainEqual({ kind: 'undated', count: 1 });
  });

  it('survives a null model', () => {
    expect(describe(null)).toEqual({ playable: false, notes: [] });
  });
});
