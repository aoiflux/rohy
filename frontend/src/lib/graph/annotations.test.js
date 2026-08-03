import { describe as suite, it, expect } from 'vitest';
import {
  KIND,
  ANCHOR,
  resolveAnchor,
  placed,
  arrowPath,
  centre,
  normaliseRegion,
  nextLayerColour,
  describe,
  LAYER_COLOURS,
} from './annotations.js';

const GEOM = { width: 200, height: 100 };
const nodes = () => ({ 5: { x: 100, y: 50 }, 6: { x: 400, y: 50 } });
const nodeOf = () => ({ 'hash-a': 5, 'hash-b': 6 });

const doc = (over = {}) => ({
  layers: [{ id: 'l1', name: 'Initial access', colour: '#c2410c', visible: true, z: 0 }],
  items: [
    {
      id: 'a-1',
      layer: 'l1',
      kind: KIND.NOTE,
      anchor: { kind: ANCHOR.EVENT, hash: 'hash-a' },
      text: 'first failure burst',
    },
  ],
  ...over,
});

suite('resolveAnchor', () => {
  it('places an event anchor beside its card, not over it', () => {
    // A pin over the card would sit on the fields it is a comment about.
    const got = resolveAnchor({ kind: ANCHOR.EVENT, hash: 'hash-a' }, nodeOf(), nodes(), GEOM);
    expect(got).toEqual({ x: 300, y: 50, w: 0, h: 0 });
  });

  it('places a world anchor exactly where it was drawn', () => {
    const got = resolveAnchor({ kind: ANCHOR.WORLD, x: 10, y: 20, w: 600, h: 400 }, {}, {}, GEOM);
    expect(got).toEqual({ x: 10, y: 20, w: 600, h: 400 });
  });

  it('is null when the event has left the case', () => {
    // 🔒 No position, not position zero. A pin at the origin points at the wrong evidence.
    expect(resolveAnchor({ kind: ANCHOR.EVENT, hash: 'gone' }, nodeOf(), nodes(), GEOM)).toBeNull();
  });

  it('is null when the event is in the case but not on this canvas', () => {
    expect(resolveAnchor({ kind: ANCHOR.EVENT, hash: 'hash-a' }, { 'hash-a': 99 }, nodes(), GEOM)).toBeNull();
  });

  it('is null for a malformed anchor rather than producing NaN geometry', () => {
    expect(resolveAnchor(null, {}, {}, GEOM)).toBeNull();
    expect(resolveAnchor({ kind: ANCHOR.WORLD, x: NaN, y: 0 }, {}, {}, GEOM)).toBeNull();
    expect(resolveAnchor({ kind: ANCHOR.EVENT }, {}, {}, GEOM)).toBeNull();
    expect(resolveAnchor({ kind: 'vibes' }, {}, {}, GEOM)).toBeNull();
  });
});

suite('placed', () => {
  it('draws what it can and counts what it cannot', () => {
    const d = doc({
      items: [
        ...doc().items,
        { id: 'a-2', layer: 'l1', kind: KIND.NOTE, anchor: { kind: ANCHOR.EVENT, hash: 'gone' } },
      ],
    });
    const got = placed(d, nodeOf(), nodes(), GEOM);
    expect(got.items).toHaveLength(1);
    expect(got.unplaceable).toBe(1);
  });

  it('needs BOTH ends of an arrow before drawing it', () => {
    const d = doc({
      items: [
        {
          id: 'a-3',
          layer: 'l1',
          kind: KIND.ARROW,
          anchor: { kind: ANCHOR.EVENT, hash: 'hash-a' },
          to: { kind: ANCHOR.EVENT, hash: 'gone' },
        },
      ],
    });
    const got = placed(d, nodeOf(), nodes(), GEOM);
    expect(got.items).toHaveLength(0);
    expect(got.unplaceable).toBe(1);
  });

  it('omits a hidden layer WITHOUT counting it as a problem', () => {
    // 🔒 Hiding a layer is a choice the analyst made. Reporting it as unplaceable would train
    // them to ignore the report.
    const d = doc({ layers: [{ id: 'l1', name: 'x', visible: false, z: 0 }] });
    const got = placed(d, nodeOf(), nodes(), GEOM);
    expect(got.items).toHaveLength(0);
    expect(got.unplaceable).toBe(0);
  });

  it('draws in layer order, so a layer brought forward actually comes forward', () => {
    const d = {
      layers: [
        { id: 'top', name: 'top', visible: true, z: 5 },
        { id: 'bottom', name: 'bottom', visible: true, z: 0 },
      ],
      items: [
        { id: 'a-top', layer: 'top', kind: KIND.NOTE, anchor: { kind: ANCHOR.EVENT, hash: 'hash-a' } },
        { id: 'a-bottom', layer: 'bottom', kind: KIND.NOTE, anchor: { kind: ANCHOR.EVENT, hash: 'hash-b' } },
      ],
    };
    const got = placed(d, nodeOf(), nodes(), GEOM);
    expect(got.items.map((p) => p.item.id)).toEqual(['a-bottom', 'a-top']);
  });

  it('still draws an annotation whose layer has vanished', () => {
    // Better an unstyled mark than a mark that disappears because of a bookkeeping gap.
    const d = doc({ layers: [] });
    const got = placed(d, nodeOf(), nodes(), GEOM);
    expect(got.items).toHaveLength(1);
    expect(got.items[0].layer).toBeNull();
  });

  it('survives an empty or missing document', () => {
    expect(placed(null, {}, {}, GEOM)).toEqual({ items: [], unplaceable: 0 });
    expect(placed({}, {}, {}, GEOM).items).toEqual([]);
  });
});

suite('arrowPath and centre', () => {
  it('runs between the centres of both anchors', () => {
    const d = arrowPath({ x: 0, y: 0, w: 100, h: 100 }, { x: 200, y: 200, w: 0, h: 0 });
    expect(d).toBe('M 50 50 L 200 200');
  });

  it('is empty when an end is missing, rather than drawing to the origin', () => {
    expect(arrowPath(null, { x: 1, y: 1 })).toBe('');
  });

  it('centres a pin on itself', () => {
    expect(centre({ x: 10, y: 20 })).toEqual({ x: 10, y: 20 });
  });
});

suite('normaliseRegion', () => {
  it('turns a drag in any direction into a positive rectangle', () => {
    // A negative width renders as nothing, which reads as a drag that did not take.
    expect(normaliseRegion({ x: 300, y: 200 }, { x: 100, y: 50 })).toEqual({ x: 100, y: 50, w: 200, h: 150 });
  });

  it('handles a zero-size drag', () => {
    expect(normaliseRegion({ x: 5, y: 5 }, { x: 5, y: 5 })).toEqual({ x: 5, y: 5, w: 0, h: 0 });
  });
});

suite('nextLayerColour', () => {
  it('picks one that is not already in use', () => {
    const used = [{ colour: LAYER_COLOURS[0] }, { colour: LAYER_COLOURS[1] }];
    expect(nextLayerColour(used)).toBe(LAYER_COLOURS[2]);
  });

  it('rotates once every colour is taken, rather than returning undefined', () => {
    const all = LAYER_COLOURS.map((c) => ({ colour: c }));
    expect(LAYER_COLOURS).toContain(nextLayerColour(all));
  });

  it('starts from the first colour', () => {
    expect(nextLayerColour([])).toBe(LAYER_COLOURS[0]);
    expect(nextLayerColour(undefined)).toBe(LAYER_COLOURS[0]);
  });
});

suite('describe', () => {
  it('separates "gone from the case" from "not on this canvas"', () => {
    // Different problems with different fixes: one is gone, the other is merely elsewhere.
    expect(describe(3, ['hash-x'])).toEqual([
      { kind: 'orphaned', count: 1 },
      { kind: 'offcanvas', count: 2 },
    ]);
  });

  it('says nothing when everything is drawn', () => {
    expect(describe(0, [])).toEqual([]);
    expect(describe(undefined, undefined)).toEqual([]);
  });

  it('never reports a negative remainder', () => {
    expect(describe(1, ['a', 'b', 'c'])).toEqual([{ kind: 'orphaned', count: 3 }]);
  });
});
