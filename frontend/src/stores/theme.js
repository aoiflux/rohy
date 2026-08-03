// Theme store (P5.2/P5.5). Owns the active theme, applies its tokens globally, and
// persists the choice. Applying a theme swaps CSS custom properties on :root so the
// change reaches every component and (later) the canvas uniformly.
import { writable, get } from 'svelte/store';
import { THEMES, DEFAULT_THEME, applyTheme } from '../lib/consts/theme.js';

const STORAGE_KEY = 'rohy:theme';

// storage returns a usable localStorage, or null.
//
// It feature-detects the METHOD rather than the object, because `typeof localStorage !==
// 'undefined'` is not the same question. Node defines a localStorage global whose methods throw
// unless the runtime was started with a backing file, so the object test passes and the call
// still fails — which is exactly how a component smoke test found this.
//
// Reading and writing are also wrapped: a browser in private mode, or with storage disabled by
// policy, throws on setItem. A theme preference is not worth taking the app down for.
function storage() {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return null;
    return localStorage;
  } catch (_) {
    return null;
  }
}

function initialTheme() {
  try {
    const saved = storage()?.getItem(STORAGE_KEY);
    if (saved === THEMES.LIGHT || saved === THEMES.DARK) return saved;
  } catch (_) {
    // Fall through to the default.
  }
  return DEFAULT_THEME;
}

function create() {
  const store = writable(initialTheme());
  const { subscribe, set } = store;

  function apply(name) {
    const next = name === THEMES.LIGHT ? THEMES.LIGHT : THEMES.DARK;
    if (typeof document !== 'undefined') applyTheme(next);
    try {
      storage()?.setItem(STORAGE_KEY, next);
    } catch (_) {
      // Storage full, disabled, or read-only. The theme still applies for this session.
    }
    set(next);
  }

  return {
    subscribe,
    set: apply,
    /** Apply the current value to the document (call once at startup). */
    init() {
      apply(get(store));
      // Re-apply when the OS reduced-motion preference changes, so the motion tokens are
      // re-zeroed (or restored) live rather than only at the next theme switch. applyTheme
      // reads the current preference, so re-running it is all that is needed.
      if (typeof window !== 'undefined' && window.matchMedia) {
        try {
          window
            .matchMedia('(prefers-reduced-motion: reduce)')
            .addEventListener('change', () => apply(get(store)));
        } catch (_) {
          // Older engines without addEventListener on MediaQueryList: the startup value
          // still applies; only live toggling is unavailable.
        }
      }
    },
    toggle() {
      apply(get(store) === THEMES.DARK ? THEMES.LIGHT : THEMES.DARK);
    },
  };
}

export const theme = create();
