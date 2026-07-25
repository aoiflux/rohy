// Pure interaction logic for the timeline canvas, kept out of the component so the rules
// that decide "what does this drag do" and "what should the cursor say" can be tested
// directly rather than through pointer-event plumbing.
import { TIMELINE_MODE } from './consts/index.js';

/**
 * timelineGesture resolves what a pointer press begins, from where it landed and the
 * current mode. The axis strip always scrubs — it is the playhead's track. Elsewhere the
 * mode decides, and Shift inverts it, so whichever gesture is not the default is always
 * exactly one modifier away rather than hidden.
 *
 * @param {{ onAxis: boolean, shiftKey: boolean, mode: string }} p
 * @returns {'scrub'|'pan'|'select'}
 */
export function timelineGesture({ onAxis, shiftKey, mode }) {
  if (onAxis) return 'scrub';
  const base = mode === TIMELINE_MODE.SELECT ? 'select' : 'pan';
  if (!shiftKey) return base;
  return base === 'pan' ? 'select' : 'pan';
}

/**
 * timelineCursor is the cursor the canvas should show, so a drag's effect is legible before
 * it starts rather than only felt after. It mirrors timelineGesture: the axis reads as a
 * horizontal scrubber, a pending select as a crosshair, a pending pan as a grab (or
 * grabbing while a drag is live).
 *
 * @param {{ onAxis: boolean, shiftKey: boolean, mode: string, active?: boolean }} p
 * @returns {string} a CSS cursor value
 */
export function timelineCursor({ onAxis, shiftKey, mode, active = false }) {
  const gesture = timelineGesture({ onAxis, shiftKey, mode });
  switch (gesture) {
    case 'scrub':
      return 'ew-resize';
    case 'select':
      return 'crosshair';
    default:
      return active ? 'grabbing' : 'grab';
  }
}

/**
 * zoomAround returns a new [start,end] view that scales the visible span by `factor` while
 * keeping `anchor` (a fraction of the full extent) fixed under the cursor — so zooming with
 * the wheel or the buttons keeps the point of interest in place instead of drifting.
 * Clamped to the full extent and to a floor span so the window can never invert or vanish.
 *
 * @param {{start:number,end:number}} view
 * @param {number} factor  >1 zooms out, <1 zooms in
 * @param {number} anchor  fraction [0..1] to hold fixed
 * @param {number} minSpan smallest allowed visible span
 */
export function zoomAround(view, factor, anchor, minSpan) {
  const span = Math.max(view.end - view.start, minSpan);
  const nextSpan = Math.min(Math.max(span * factor, minSpan), 1);
  let start = anchor - ((anchor - view.start) / span) * nextSpan;
  start = Math.min(Math.max(start, 0), 1 - nextSpan);
  return { start, end: start + nextSpan };
}
