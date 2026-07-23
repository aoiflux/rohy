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
