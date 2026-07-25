import { describe, it, expect } from 'vitest';
import { timelineGesture, timelineCursor, zoomAround } from './timeline.js';
import { TIMELINE_MODE, TIMELINE } from './consts/index.js';

const PAN = TIMELINE_MODE.PAN;
const SELECT = TIMELINE_MODE.SELECT;

describe('timelineGesture resolves what a drag does', () => {
  it('scrubs on the axis regardless of mode or Shift', () => {
    expect(timelineGesture({ onAxis: true, shiftKey: false, mode: PAN })).toBe('scrub');
    expect(timelineGesture({ onAxis: true, shiftKey: true, mode: SELECT })).toBe('scrub');
  });

  it('follows the mode off the axis', () => {
    expect(timelineGesture({ onAxis: false, shiftKey: false, mode: PAN })).toBe('pan');
    expect(timelineGesture({ onAxis: false, shiftKey: false, mode: SELECT })).toBe('select');
  });

  it('Shift inverts whichever mode is active — the other gesture is always one modifier away', () => {
    expect(timelineGesture({ onAxis: false, shiftKey: true, mode: PAN })).toBe('select');
    expect(timelineGesture({ onAxis: false, shiftKey: true, mode: SELECT })).toBe('pan');
  });
});

describe('timelineCursor makes the pending gesture legible', () => {
  it('shows a horizontal resize on the axis (scrub)', () => {
    expect(timelineCursor({ onAxis: true, shiftKey: false, mode: PAN })).toBe('ew-resize');
  });
  it('shows a crosshair when a drag would select', () => {
    expect(timelineCursor({ onAxis: false, shiftKey: false, mode: SELECT })).toBe('crosshair');
    expect(timelineCursor({ onAxis: false, shiftKey: true, mode: PAN })).toBe('crosshair');
  });
  it('shows grab for pan, grabbing while the pan is live', () => {
    expect(timelineCursor({ onAxis: false, shiftKey: false, mode: PAN })).toBe('grab');
    expect(timelineCursor({ onAxis: false, shiftKey: false, mode: PAN, active: true })).toBe('grabbing');
  });
});

describe('zoomAround keeps the anchor fixed and stays in bounds', () => {
  const min = TIMELINE.MIN_VIEW_SPAN;

  it('zooms in around the anchor, holding it in place', () => {
    const v = zoomAround({ start: 0, end: 1 }, 0.5, 0.5, min);
    expect(v.end - v.start).toBeCloseTo(0.5); // half the span
    expect((v.start + v.end) / 2).toBeCloseTo(0.5); // centre held
  });

  it('holds an off-centre anchor', () => {
    const v = zoomAround({ start: 0, end: 1 }, 0.5, 0.25, min);
    // 0.25 was at fraction 0.25 of the old window; it must still map to 0.25 absolute.
    expect(v.start).toBeCloseTo(0.125);
    expect(v.end).toBeCloseTo(0.625);
  });

  it('never exceeds the full extent when zooming out', () => {
    const v = zoomAround({ start: 0.4, end: 0.6 }, 100, 0.5, min);
    expect(v.start).toBe(0);
    expect(v.end).toBe(1);
  });

  it('never collapses below the minimum span', () => {
    const v = zoomAround({ start: 0, end: 1 }, 0.0000001, 0.5, min);
    // Clamped to exactly the floor (float subtraction of the two edges can round a hair
    // under, so compare with tolerance rather than a hard >=).
    expect(v.end - v.start).toBeCloseTo(min, 10);
  });

  it('clamps the window to the right edge rather than overshooting', () => {
    const v = zoomAround({ start: 0.9, end: 1 }, 2, 1, min);
    expect(v.end).toBe(1);
    expect(v.start).toBeGreaterThanOrEqual(0);
    expect(v.end - v.start).toBeCloseTo(0.2);
  });
});
