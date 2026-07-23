import { describe, it, expect } from 'vitest';
import {
  worldToScreen,
  screenToWorld,
  zoomAround,
  clampZoom,
  isNodeVisible,
  centerOn,
  worldBounds,
  fitToNodes,
  normalizeRect,
  nodesInRect,
} from './coords.js';
import { GRAPH } from '../../lib/consts/index.js';

describe('viewport coordinate model', () => {
  it('screenToWorld inverts worldToScreen', () => {
    const vp = { x: 120, y: -40, zoom: 1.5 };
    const w = screenToWorld(300, 200, vp);
    const s = worldToScreen(w.x, w.y, vp);
    expect(s.x).toBeCloseTo(300, 6);
    expect(s.y).toBeCloseTo(200, 6);
  });

  it('zoomAround keeps the anchor world point under the cursor', () => {
    const vp = { x: 0, y: 0, zoom: 1 };
    const before = screenToWorld(400, 300, vp);
    const next = zoomAround(vp, 2, 400, 300);
    const after = screenToWorld(400, 300, next);
    expect(after.x).toBeCloseTo(before.x, 6);
    expect(after.y).toBeCloseTo(before.y, 6);
    expect(next.zoom).toBe(2);
  });

  it('clamps zoom to configured bounds', () => {
    expect(clampZoom(999)).toBe(GRAPH.ZOOM_MAX);
    expect(clampZoom(0.0001)).toBe(GRAPH.ZOOM_MIN);
    expect(zoomAround({ x: 0, y: 0, zoom: 1 }, 999, 0, 0).zoom).toBe(GRAPH.ZOOM_MAX);
  });

  it('centerOn puts the node centre at the canvas centre (P14.4 focus)', () => {
    const vp = { x: 999, y: 999, zoom: 1.5 };
    const node = { x: 400, y: 250 };
    const W = 1000;
    const H = 600;
    const next = centerOn(node, vp, W, H);
    // The node's world centre must project to the middle of the canvas.
    const cx = node.x + GRAPH.NODE_WIDTH / 2;
    const cy = node.y + GRAPH.NODE_HEIGHT / 2;
    const s = worldToScreen(cx, cy, next);
    expect(s.x).toBeCloseTo(W / 2, 6);
    expect(s.y).toBeCloseTo(H / 2, 6);
    expect(next.zoom).toBe(vp.zoom); // zoom preserved
  });
});

describe('canvas virtualization (P9.1)', () => {
  const vp = { x: 0, y: 0, zoom: 1 };
  const W = 1000;
  const H = 700;

  it('renders only a bounded window regardless of total node count', () => {
    // A 50×50 grid = 2500 nodes spread across a large world.
    const nodes = [];
    for (let i = 0; i < 50; i++) {
      for (let j = 0; j < 50; j++) {
        nodes.push({ x: i * 300, y: j * 200 });
      }
    }
    const visible = nodes.filter((n) => isNodeVisible(n, vp, W, H));
    // The visible set must be a tiny fraction of the total — proving render cost is
    // bounded by the viewport, not the graph size.
    expect(visible.length).toBeGreaterThan(0);
    expect(visible.length).toBeLessThan(60);
    expect(visible.length).toBeLessThan(nodes.length / 10);
  });

  it('excludes nodes far outside the viewport but keeps the margin ring', () => {
    expect(isNodeVisible({ x: 0, y: 0 }, vp, W, H)).toBe(true);
    expect(isNodeVisible({ x: 100000, y: 100000 }, vp, W, H)).toBe(false);
    // Just beyond the right edge but within VIRTUALIZE_MARGIN → still rendered.
    const justOff = { x: W + GRAPH.VIRTUALIZE_MARGIN - GRAPH.NODE_WIDTH - 1, y: 100 };
    expect(isNodeVisible(justOff, vp, W, H)).toBe(true);
  });
});

// --- Fit-to-content (replaces the old jump-to-origin reset) ---

const node = (id, x, y) => ({ event: { id }, x, y });

describe('fitToNodes', () => {
  it('frames every node, including ones far from the world origin', () => {
    // The failure the old reset had: nodes nowhere near (0,0). Fitting must still show them.
    const nodes = [node(1, 5000, 4000), node(2, 5600, 4400)];
    const W = 800;
    const H = 600;
    const vp = fitToNodes(nodes, W, H);

    for (const n of nodes) {
      for (const [cx, cy] of [
        [n.x, n.y],
        [n.x + GRAPH.NODE_WIDTH, n.y + GRAPH.NODE_HEIGHT],
      ]) {
        const s = worldToScreen(cx, cy, vp);
        expect(s.x).toBeGreaterThanOrEqual(0);
        expect(s.y).toBeGreaterThanOrEqual(0);
        expect(s.x).toBeLessThanOrEqual(W);
        expect(s.y).toBeLessThanOrEqual(H);
      }
    }
  });

  it('centres the content in the canvas', () => {
    const nodes = [node(1, 100, 100), node(2, 900, 700)];
    const W = 1000;
    const H = 800;
    const vp = fitToNodes(nodes, W, H);
    const b = worldBounds(nodes);
    const centre = worldToScreen(b.minX + b.width / 2, b.minY + b.height / 2, vp);
    expect(centre.x).toBeCloseTo(W / 2, 6);
    expect(centre.y).toBeCloseTo(H / 2, 6);
  });

  it('never zooms outside the configured bounds', () => {
    const tiny = fitToNodes([node(1, 0, 0)], 4000, 4000);
    expect(tiny.zoom).toBeLessThanOrEqual(GRAPH.ZOOM_MAX);
    const huge = fitToNodes([node(1, 0, 0), node(2, 500000, 500000)], 400, 300);
    expect(huge.zoom).toBeGreaterThanOrEqual(GRAPH.ZOOM_MIN);
  });

  it('returns the identity view for an empty canvas', () => {
    expect(fitToNodes([], 800, 600)).toEqual({ x: 0, y: 0, zoom: 1 });
    expect(fitToNodes({}, 800, 600)).toEqual({ x: 0, y: 0, zoom: 1 });
  });

  it('tolerates an unmeasured canvas', () => {
    expect(fitToNodes([node(1, 0, 0)], 0, 0)).toEqual({ x: 0, y: 0, zoom: 1 });
  });
});

// --- Marquee box-select ---

describe('normalizeRect', () => {
  it('normalizes a box dragged in any direction', () => {
    const down_right = normalizeRect({ x: 10, y: 20 }, { x: 110, y: 220 });
    const up_left = normalizeRect({ x: 110, y: 220 }, { x: 10, y: 20 });
    expect(down_right).toEqual({ x: 10, y: 20, width: 100, height: 200 });
    expect(up_left).toEqual(down_right);
  });
});

describe('nodesInRect', () => {
  const nodes = [node(1, 0, 0), node(2, 1000, 0), node(3, 0, 1000)];

  it('selects nodes inside the box and ignores the rest', () => {
    const ids = nodesInRect(nodes, { x: -10, y: -10, width: 400, height: 400 });
    expect(ids).toEqual([1]);
  });

  it('counts a node the box merely clips, not just fully contained ones', () => {
    // The box overlaps only the last few px of node 1 — users read that as "selected".
    const ids = nodesInRect(nodes, {
      x: GRAPH.NODE_WIDTH - 5,
      y: GRAPH.NODE_HEIGHT - 5,
      width: 20,
      height: 20,
    });
    expect(ids).toContain(1);
  });

  it('selects several nodes at once', () => {
    const ids = nodesInRect(nodes, { x: -50, y: -50, width: 2000, height: 2000 });
    expect(ids.sort()).toEqual([1, 2, 3]);
  });

  it('returns nothing for a box over empty space', () => {
    expect(nodesInRect(nodes, { x: 5000, y: 5000, width: 100, height: 100 })).toEqual([]);
  });
});
