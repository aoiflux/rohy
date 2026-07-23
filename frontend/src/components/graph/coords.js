// Viewport / coordinate model for the graph canvas (P7.1). Node positions are stored
// in WORLD space (stable, layout-independent). The viewport maps world→screen with a
// pan offset (screen px) and a uniform zoom:
//
//   screen = world * zoom + pan
//   world  = (screen - pan) / zoom
//
// Keeping world coordinates canonical means pan/zoom never mutate node data, only how
// it is projected — which is also what makes virtualization a pure function of the
// viewport rectangle.

import { GRAPH } from '../../lib/consts/index.js';

export function worldToScreen(wx, wy, viewport) {
  return { x: wx * viewport.zoom + viewport.x, y: wy * viewport.zoom + viewport.y };
}

export function screenToWorld(sx, sy, viewport) {
  return { x: (sx - viewport.x) / viewport.zoom, y: (sy - viewport.y) / viewport.zoom };
}

export function clampZoom(z) {
  return Math.max(GRAPH.ZOOM_MIN, Math.min(GRAPH.ZOOM_MAX, z));
}

// zoomAround returns a viewport zoomed to `nextZoom` while keeping the world point
// under the screen anchor (sx,sy) fixed — i.e. zoom toward the cursor.
export function zoomAround(viewport, nextZoom, sx, sy) {
  const z = clampZoom(nextZoom);
  const world = screenToWorld(sx, sy, viewport);
  return { zoom: z, x: sx - world.x * z, y: sy - world.y * z };
}

// isNodeVisible tests whether a node's screen rect intersects the viewport expanded by
// VIRTUALIZE_MARGIN. Used to render only the on-screen window at large node counts.
export function isNodeVisible(node, viewport, width, height) {
  const s = worldToScreen(node.x, node.y, viewport);
  const w = GRAPH.NODE_WIDTH * viewport.zoom;
  const h = GRAPH.NODE_HEIGHT * viewport.zoom;
  const m = GRAPH.VIRTUALIZE_MARGIN;
  return s.x + w >= -m && s.x <= width + m && s.y + h >= -m && s.y <= height + m;
}

// nodeCenterScreen returns the screen-space centre of a node (edge endpoints).
export function nodeCenterScreen(node, viewport) {
  const s = worldToScreen(node.x, node.y, viewport);
  return { x: s.x + (GRAPH.NODE_WIDTH * viewport.zoom) / 2, y: s.y + (GRAPH.NODE_HEIGHT * viewport.zoom) / 2 };
}

// centerOn returns a viewport (keeping the current zoom) that places a node's centre at
// the middle of a width×height canvas — used to auto-focus the canvas on an event
// selected elsewhere (P14.4 cross-view focus).
export function centerOn(node, viewport, width, height) {
  const cx = node.x + GRAPH.NODE_WIDTH / 2;
  const cy = node.y + GRAPH.NODE_HEIGHT / 2;
  return { zoom: viewport.zoom, x: width / 2 - cx * viewport.zoom, y: height / 2 - cy * viewport.zoom };
}

// worldBounds returns the bounding box (world space) enclosing every node, or null when
// there are none.
export function worldBounds(nodes) {
  const list = Array.isArray(nodes) ? nodes : Object.values(nodes || {});
  if (list.length === 0) return null;
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const n of list) {
    minX = Math.min(minX, n.x);
    minY = Math.min(minY, n.y);
    maxX = Math.max(maxX, n.x + GRAPH.NODE_WIDTH);
    maxY = Math.max(maxY, n.y + GRAPH.NODE_HEIGHT);
  }
  return { minX, minY, maxX, maxY, width: maxX - minX, height: maxY - minY };
}

// fitToNodes returns a viewport that frames EVERY node with a comfortable margin.
//
// This is what the toolbar's fit control does, replacing a literal "jump to world origin":
// the origin is only useful when nodes happen to sit near it, so resetting there could
// leave the user staring at empty canvas — precisely when they reached for the button.
// Fitting can never do that. Zoom is clamped to the configured range, so a single distant
// node cannot zoom the canvas into uselessness; an empty canvas returns the identity view.
export function fitToNodes(nodes, width, height, padding = GRAPH.FIT_PADDING) {
  const b = worldBounds(nodes);
  if (!b || !width || !height) return { x: 0, y: 0, zoom: 1 };

  const availW = Math.max(width - padding * 2, 1);
  const availH = Math.max(height - padding * 2, 1);
  // A zero-sized box (one node is never zero, but be defensive) must not divide by zero.
  const zoom = clampZoom(Math.min(availW / Math.max(b.width, 1), availH / Math.max(b.height, 1)));

  const cx = b.minX + b.width / 2;
  const cy = b.minY + b.height / 2;
  return { zoom, x: width / 2 - cx * zoom, y: height / 2 - cy * zoom };
}

// normalizeRect orders two corners into a positive-area rect, so a marquee dragged in any
// direction (including up-left) still describes the same region.
export function normalizeRect(a, b) {
  return {
    x: Math.min(a.x, b.x),
    y: Math.min(a.y, b.y),
    width: Math.abs(a.x - b.x),
    height: Math.abs(a.y - b.y),
  };
}

// nodesInRect returns the ids of every node whose rect INTERSECTS the given world-space
// rect. Intersection (rather than full containment) is deliberate: a marquee that clips a
// node is read by users as "I selected that one".
export function nodesInRect(nodes, rect) {
  const list = Array.isArray(nodes) ? nodes : Object.values(nodes || {});
  const right = rect.x + rect.width;
  const bottom = rect.y + rect.height;
  const out = [];
  for (const n of list) {
    const nr = n.x + GRAPH.NODE_WIDTH;
    const nb = n.y + GRAPH.NODE_HEIGHT;
    if (n.x <= right && nr >= rect.x && n.y <= bottom && nb >= rect.y) {
      out.push(n.event.id);
    }
  }
  return out;
}
