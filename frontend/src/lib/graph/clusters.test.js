import { describe, it, expect } from 'vitest';
import {
  cardCorners,
  convexHull,
  centroid,
  padOutward,
  hullPath,
  clusterHull,
  memberIndex,
  proxyCards,
  remapEdges,
  toggle,
  isProxy,
  PROXY_PREFIX,
} from './clusters.js';

const GEOM = { width: 200, height: 100, pad: 20 };

const nodes = () => ({
  1: { x: 0, y: 0 },
  2: { x: 400, y: 0 },
  3: { x: 400, y: 300 },
});

describe('convexHull', () => {
  it('drops interior points', () => {
    const hull = convexHull([
      { x: 0, y: 0 },
      { x: 10, y: 0 },
      { x: 10, y: 10 },
      { x: 0, y: 10 },
      { x: 5, y: 5 }, // inside
    ]);
    expect(hull).toHaveLength(4);
    expect(hull.some((p) => p.x === 5 && p.y === 5)).toBe(false);
  });

  it('collapses duplicate points rather than emitting a zero-length edge', () => {
    const hull = convexHull([
      { x: 0, y: 0 },
      { x: 0, y: 0 },
      { x: 10, y: 0 },
      { x: 0, y: 10 },
    ]);
    expect(hull).toHaveLength(3);
  });

  it('returns the distinct points when there is no hull to compute', () => {
    expect(convexHull([{ x: 1, y: 1 }])).toHaveLength(1);
    expect(convexHull([])).toEqual([]);
  });

  it('ignores non-finite coordinates instead of producing NaN geometry', () => {
    const hull = convexHull([{ x: 0, y: 0 }, { x: NaN, y: 3 }, { x: 10, y: 0 }, { x: 0, y: 10 }]);
    expect(hull.every((p) => Number.isFinite(p.x) && Number.isFinite(p.y))).toBe(true);
  });
});

describe('padOutward', () => {
  it('pushes every vertex away from the centre', () => {
    const square = [
      { x: -10, y: -10 },
      { x: 10, y: -10 },
      { x: 10, y: 10 },
      { x: -10, y: 10 },
    ];
    const padded = padOutward(square, 5);
    for (let i = 0; i < square.length; i += 1) {
      expect(Math.abs(padded[i].x)).toBeGreaterThan(Math.abs(square[i].x));
      expect(Math.abs(padded[i].y)).toBeGreaterThan(Math.abs(square[i].y));
    }
  });

  it('leaves a vertex sitting exactly on the centroid alone rather than dividing by zero', () => {
    expect(padOutward([{ x: 0, y: 0 }], 5)).toEqual([{ x: 0, y: 0 }]);
  });
});

describe('hullPath', () => {
  it('closes the path', () => {
    const d = hullPath([{ x: 0, y: 0 }, { x: 1, y: 0 }, { x: 0, y: 1 }]);
    expect(d.startsWith('M')).toBe(true);
    expect(d.endsWith('Z')).toBe(true);
  });

  it('is empty below three points, so nothing degenerate is drawn', () => {
    expect(hullPath([{ x: 0, y: 0 }, { x: 1, y: 1 }])).toBe('');
    expect(hullPath(null)).toBe('');
  });
});

describe('clusterHull', () => {
  it('encloses the cards, not their origins', () => {
    // 🔒 A hull through the top-left corners cuts through half the cards it claims to contain,
    // so a node inside a group would render outside its outline.
    const hull = clusterHull([1], nodes(), { ...GEOM, pad: 0 });
    expect(hull).not.toBeNull();
    // The bottom-right corner of the single card must be on the outline.
    expect(hull.path).toContain(`${GEOM.width} ${GEOM.height}`);
  });

  it('hangs its label from the topmost point, so a label never lands on a card', () => {
    const hull = clusterHull([1, 2, 3], nodes(), GEOM);
    expect(hull.anchor.y).toBeLessThan(0); // above every card top (y=0) because of the padding
  });

  it('skips members that are no longer on the canvas', () => {
    // A cluster computed a moment ago can name a node that has since been removed. Anchoring it
    // at the origin would stretch the outline across the whole world.
    const withGhost = clusterHull([1, 999], nodes(), GEOM);
    const without = clusterHull([1], nodes(), GEOM);
    expect(withGhost.path).toBe(without.path);
    expect(withGhost.present).toBe(1);
  });

  it('is null when nothing it names is on the canvas', () => {
    expect(clusterHull([999], nodes(), GEOM)).toBeNull();
    expect(clusterHull([], nodes(), GEOM)).toBeNull();
  });

  it('still draws an outline when every card sits at the same point', () => {
    // Degenerate but reachable: a graph loaded before any layout ran.
    const stacked = { 1: { x: 50, y: 50 }, 2: { x: 50, y: 50 } };
    const hull = clusterHull([1, 2], stacked, GEOM);
    expect(hull.path).not.toBe('');
  });
});

describe('memberIndex', () => {
  const clusters = [
    { id: 'a', node_ids: [1, 2], size: 2 },
    { id: 'b', node_ids: [2, 3], size: 2 },
  ];

  it('maps only the members of collapsed clusters', () => {
    const idx = memberIndex(clusters, new Set(['a']));
    expect(idx.get('1')).toBe(`${PROXY_PREFIX}a`);
    expect(idx.has('3')).toBe(false);
  });

  it('assigns a node in two collapsed clusters to the first, stably', () => {
    // Rule grouping genuinely overlaps. Order is the backend's, so the choice does not change
    // between runs — but it is a choice, which is why clusters carry `overlapping`.
    const idx = memberIndex(clusters, new Set(['a', 'b']));
    expect(idx.get('2')).toBe(`${PROXY_PREFIX}a`);
  });

  it('is empty when nothing is collapsed', () => {
    expect(memberIndex(clusters, new Set()).size).toBe(0);
    expect(memberIndex(null, null).size).toBe(0);
  });
});

describe('proxyCards', () => {
  const clusters = [{ id: 'a', label: 'Group of 2', node_ids: [1, 2], size: 2 }];

  it('sits at the centre of the cards it replaces, so the graph does not jump', () => {
    const [card] = proxyCards(clusters, new Set(['a']), nodes(), GEOM);
    // Members centre at (100,50) and (500,50); the proxy centres at (300,50).
    expect(card.x + GEOM.width / 2).toBe(300);
    expect(card.y + GEOM.height / 2).toBe(50);
  });

  it('reports the cluster’s own size, not how many members happen to be on the canvas', () => {
    // 🔒 A collapsed card that under-reported what it contains would hide exactly the thing it
    // exists to disclose.
    const partial = [{ id: 'a', label: 'x', node_ids: [1, 999], size: 2 }];
    const [card] = proxyCards(partial, new Set(['a']), nodes(), GEOM);
    expect(card.size).toBe(2);
    expect(card.onCanvas).toBe(1);
  });

  it('produces nothing for a cluster whose members have all gone', () => {
    const gone = [{ id: 'a', label: 'x', node_ids: [999], size: 1 }];
    expect(proxyCards(gone, new Set(['a']), nodes(), GEOM)).toEqual([]);
  });

  it('marks its id as a proxy so a card is never mistaken for an event', () => {
    const [card] = proxyCards(clusters, new Set(['a']), nodes(), GEOM);
    expect(isProxy(card.id)).toBe(true);
    expect(isProxy(1)).toBe(false);
  });
});

describe('remapEdges', () => {
  const edges = [
    { id: 10, from: 1, to: 2 }, // both inside cluster a
    { id: 11, from: 2, to: 3 }, // crosses out of a
    { id: 12, from: 3, to: 3 },
  ];
  const clusters = [{ id: 'a', node_ids: [1, 2], size: 2 }];

  it('re-points a crossing edge at the collapsed cluster', () => {
    // 🔒 Never dropped. An edge that vanished when a group closed would make the collapsed card
    // look less connected than it is — and collapsing is done precisely to see those links.
    const { edges: out } = remapEdges(edges, memberIndex(clusters, new Set(['a'])));
    const crossing = out.find((e) => e.id === 11);
    expect(crossing.from).toBe(`${PROXY_PREFIX}a`);
    expect(crossing.to).toBe(3);
  });

  it('hides an edge internal to one collapsed cluster and counts it', () => {
    const { edges: out, internal } = remapEdges(edges, memberIndex(clusters, new Set(['a'])));
    expect(out.some((e) => e.id === 10)).toBe(false);
    expect(internal).toBe(1);
  });

  it('keeps the relation id, so a re-pointed edge still inspects the real relation', () => {
    const { edges: out } = remapEdges(edges, memberIndex(clusters, new Set(['a'])));
    expect(out.map((e) => e.id)).toContain(11);
  });

  it('leaves a self-loop between two real nodes alone rather than counting it as hidden', () => {
    // Internal means both endpoints landed in the same COLLAPSED cluster — hence the proxy
    // check in remapEdges. A bare `from === to` test would report an ordinary self-loop as
    // something the collapse hid.
    const { edges: out, internal } = remapEdges(
      [{ id: 20, from: 3, to: 3 }],
      memberIndex(clusters, new Set(['a'])),
    );
    expect(out).toHaveLength(1);
    expect(internal).toBe(0);
  });

  it('does not copy edges it did not change', () => {
    const idx = memberIndex(clusters, new Set(['a']));
    const { edges: out } = remapEdges(edges, idx);
    expect(out.find((e) => e.id === 12)).toBe(edges[2]);
  });

  it('is a pass-through when nothing is collapsed', () => {
    expect(remapEdges(edges, new Map()).edges).toBe(edges);
  });
});

describe('toggle', () => {
  it('flips one cluster and returns a new set', () => {
    const a = new Set(['x']);
    const b = toggle(a, 'y');
    expect(b.has('x')).toBe(true);
    expect(b.has('y')).toBe(true);
    expect(a.has('y')).toBe(false); // the original is untouched — the canvas re-renders on identity
    expect(toggle(b, 'y').has('y')).toBe(false);
  });
});

describe('cardCorners', () => {
  it('returns the four corners in order', () => {
    expect(cardCorners({ x: 1, y: 2 }, 10, 5)).toEqual([
      { x: 1, y: 2 },
      { x: 11, y: 2 },
      { x: 11, y: 7 },
      { x: 1, y: 7 },
    ]);
  });
});

describe('centroid', () => {
  it('averages the points', () => {
    expect(centroid([{ x: 0, y: 0 }, { x: 10, y: 20 }])).toEqual({ x: 5, y: 10 });
  });
  it('is the origin for nothing, rather than NaN', () => {
    expect(centroid([])).toEqual({ x: 0, y: 0 });
  });
});
