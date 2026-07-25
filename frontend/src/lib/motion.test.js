import { describe, it, expect, vi, afterEach } from 'vitest';
import { duration, motion, reveal, afterDelay, SKELETON_DELAY } from './motion.js';
import { MOTION } from './consts/index.js';

// Force the reduced-motion media query to a known answer. motion.js reads it through
// window.matchMedia, so stubbing that stubs the whole module's behaviour.
function setReducedMotion(reduced) {
  // The tests run in the node environment, which has no window. motion.js reads
  // window.matchMedia (guarded by try/catch), so provide a window whose matchMedia answers
  // the reduced-motion query as required.
  vi.stubGlobal('window', {
    matchMedia: (q) => ({
      matches: reduced && q.includes('reduce'),
      media: q,
      addEventListener() {},
      removeEventListener() {},
    }),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('motion durations honour reduced motion in one place', () => {
  it('returns the real duration by default', () => {
    setReducedMotion(false);
    expect(duration(MOTION.MEDIUM)).toBe(MOTION.MEDIUM);
    expect(motion(MOTION.SLOW).duration).toBe(MOTION.SLOW);
  });

  it('collapses every duration to zero under reduced motion', () => {
    setReducedMotion(true);
    expect(duration(MOTION.MEDIUM)).toBe(0);
    expect(motion(MOTION.FAST).duration).toBe(0);
    expect(reveal(MOTION.SLOW).duration).toBe(0);
  });
});

describe('reveal is a consistent arrival', () => {
  it('rises and fades over a fixed duration normally', () => {
    setReducedMotion(false);
    const r = reveal(MOTION.MEDIUM);
    expect(r.duration).toBe(MOTION.MEDIUM);
    expect(r.y).toBeGreaterThan(0); // a slight rise, so content reads as arriving
  });

  it('becomes an instant, motionless appearance under reduced motion', () => {
    setReducedMotion(true);
    const r = reveal(MOTION.MEDIUM);
    expect(r.duration).toBe(0);
    expect(r.y).toBe(0); // no movement at all, not merely a shorter one
  });

  it('uses the same duration regardless of anything external — it is never a "tell"', () => {
    setReducedMotion(false);
    // The reveal duration is a constant of the call, not a function of load time: two calls
    // with the same argument are identical, so a slow load and a fast load reveal the same.
    expect(reveal(MOTION.MEDIUM)).toEqual(reveal(MOTION.MEDIUM));
  });
});

describe('afterDelay gates a skeleton without ever delaying data', () => {
  it('does not fire before the threshold — a fast load cancels it first', () => {
    vi.useFakeTimers();
    const show = vi.fn();
    const cancel = afterDelay(show, SKELETON_DELAY);
    // Data arrived in 20 ms: the caller cancels, and the skeleton never appears.
    vi.advanceTimersByTime(20);
    cancel();
    vi.advanceTimersByTime(SKELETON_DELAY * 2);
    expect(show).not.toHaveBeenCalled();
  });

  it('fires once the load genuinely drags past the threshold', () => {
    vi.useFakeTimers();
    const show = vi.fn();
    afterDelay(show, SKELETON_DELAY);
    vi.advanceTimersByTime(SKELETON_DELAY + 1);
    expect(show).toHaveBeenCalledTimes(1);
  });

  it('keeps the threshold above the ~100ms that reads as instant', () => {
    // A threshold at or below human "instant" perception would flash a skeleton on loads
    // that felt immediate — the exact glitch it exists to avoid.
    expect(SKELETON_DELAY).toBeGreaterThan(100);
  });
});
