// Motion helpers. Every animation in the app routes its duration through here so that
// "prefers-reduced-motion" is honoured in ONE place — a user who has asked the OS for less
// motion gets an instant state change rather than a shortened animation.
import { MOTION } from './consts/index.js';

/** Whether the user has asked for reduced motion. Safe outside a browser. */
export function prefersReducedMotion() {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  } catch (_) {
    return false;
  }
}

/** A duration in ms, collapsed to 0 when the user prefers reduced motion. */
export function duration(ms = MOTION.MEDIUM) {
  return prefersReducedMotion() ? 0 : ms;
}

/**
 * Transition params for Svelte's built-in transitions, reduced-motion aware.
 * @param {number} ms
 */
export function motion(ms = MOTION.MEDIUM) {
  return { duration: duration(ms), easing: undefined };
}

// --- Perceived performance ---
//
// These exist to make the app FEEL fast, which is a different thing from being fast. The
// idea, borrowed from mobile OSes and games: cover the moment data is being fetched with a
// consistent reveal, so the user perceives "arriving" rather than "waiting". Two rules keep
// it honest rather than deceptive:
//
//   1. The reveal plays for the SAME duration every time, whether the data took 5 ms or
//      500 ms. A Svelte `in:` transition animates the element as it mounts and never holds
//      the data back — so this adds zero real latency, and the animation never becomes a
//      tell that "this time it was slow" (which is exactly what a duration that scaled with
//      load time would do).
//   2. A skeleton only appears once a load has genuinely dragged past SKELETON_DELAY.
//      Flashing a skeleton for 20 ms on an instant load reads as a glitch — worse than the
//      clean reveal alone. Below the threshold the reveal carries the whole experience.

/**
 * SKELETON_DELAY is how long a load may run before a skeleton is worth showing. Set just
 * above the ~100 ms that reads as "instant": faster than this needs no placeholder, and
 * showing one would only flash.
 */
export const SKELETON_DELAY = 140;

/**
 * reveal returns `fly` params for content ARRIVING — a short rise plus fade, played
 * consistently every time a view's data mounts. Reduced motion collapses it to an instant
 * appearance (no movement, no duration), honouring the same one-place rule as motion().
 * @param {number} ms
 */
export function reveal(ms = MOTION.MEDIUM) {
  const d = duration(ms);
  return { y: d === 0 ? 0 : 6, duration: d, easing: undefined };
}

/**
 * afterDelay calls `show` once `ms` has elapsed and returns a canceller. It is how a
 * skeleton is gated: start it when a load begins, cancel it the moment the data arrives, so
 * a fast load never shows one. Under reduced motion the delay still applies — the skeleton
 * is information, not decoration, so a slow load still gets it; only the shimmer is stilled.
 * @param {() => void} show
 * @param {number} ms
 * @returns {() => void} cancel
 */
export function afterDelay(show, ms = SKELETON_DELAY) {
  const t = setTimeout(show, ms);
  return () => clearTimeout(t);
}
