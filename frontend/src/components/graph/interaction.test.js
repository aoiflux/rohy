import { describe, it, expect } from 'vitest';
import { isOverlayTarget, OVERLAY_ATTR, OVERLAY_SELECTOR } from './interaction.js';

// A minimal element stand-in with a real `closest` that walks ancestors, so the test
// exercises the ancestor search rather than a stub that always agrees.
function el(attrs = [], parent = null) {
  const node = {
    attrs: new Set(attrs),
    parent,
    closest(selector) {
      const attr = selector.replace(/^\[|\]$/g, '');
      for (let n = node; n; n = n.parent) {
        if (n.attrs.has(attr)) return n;
      }
      return null;
    },
  };
  return node;
}

describe('isOverlayTarget', () => {
  it('treats a click on the overlay container itself as overlay', () => {
    expect(isOverlayTarget(el([OVERLAY_ATTR]))).toBe(true);
  });

  it('treats a click on a control INSIDE an overlay as overlay', () => {
    // This is the actual failure mode: the pointer lands on the toolbar button, while the
    // marker lives on the toolbar wrapping it.
    const toolbar = el([OVERLAY_ATTR]);
    const button = el([], toolbar);
    expect(isOverlayTarget(button)).toBe(true);
  });

  it('finds an overlay ancestor several levels up (menu item → menu → anchor)', () => {
    const anchor = el([OVERLAY_ATTR]);
    const menu = el([], anchor);
    const item = el([], menu);
    expect(isOverlayTarget(item)).toBe(true);
  });

  it('treats canvas content as NOT overlay, so gestures still work', () => {
    const world = el([]);
    const node = el(['data-node-id'], world);
    expect(isOverlayTarget(node)).toBe(false);
    expect(isOverlayTarget(world)).toBe(false);
  });

  it('is safe for missing or non-element targets', () => {
    expect(isOverlayTarget(null)).toBe(false);
    expect(isOverlayTarget(undefined)).toBe(false);
    expect(isOverlayTarget({})).toBe(false);
  });

  it('exposes a selector that matches the attribute it documents', () => {
    expect(OVERLAY_SELECTOR).toBe(`[${OVERLAY_ATTR}]`);
  });
});
