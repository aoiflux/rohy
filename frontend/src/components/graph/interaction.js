// Canvas interaction rules that are pure enough to test on their own.
//
// The canvas owns a single pointer-gesture state machine on its viewport element. Overlay
// UI (the zoom/connect toolbar, the node context menu) is positioned INSIDE that viewport
// for layout reasons, so its pointer events bubble into the same handler — and if the
// canvas treats them as canvas gestures, two things break:
//
//   1. `setPointerCapture` on the viewport redirects the follow-up `click` to the viewport,
//      so a toolbar button's onclick never fires.
//   2. Closing the context menu on pointerdown unmounts the menu item before its click can
//      land, so the action never runs.
//
// Both presented as "the button does nothing". The rule below is what keeps overlay
// pointers out of the gesture machine.

export const OVERLAY_ATTR = 'data-overlay';
export const OVERLAY_SELECTOR = `[${OVERLAY_ATTR}]`;

/**
 * isOverlayTarget reports whether a pointer landed on overlay UI rather than the canvas.
 * The marker sits on the overlay CONTAINER, so this must match descendants too — the click
 * lands on the button, not on the toolbar that carries the attribute.
 * @param {{closest?: (sel: string) => any} | null | undefined} target
 */
export function isOverlayTarget(target) {
  if (!target || typeof target.closest !== 'function') return false;
  return target.closest(OVERLAY_SELECTOR) != null;
}
