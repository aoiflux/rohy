// Theme store (P5.2/P5.5). Owns the active theme, applies its tokens globally, and
// persists the choice. Applying a theme swaps CSS custom properties on :root so the
// change reaches every component and (later) the canvas uniformly.
import { writable, get } from 'svelte/store';
import { THEMES, DEFAULT_THEME, applyTheme } from '../lib/consts/theme.js';

const STORAGE_KEY = 'rohy:theme';

function initialTheme() {
  if (typeof localStorage !== 'undefined') {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === THEMES.LIGHT || saved === THEMES.DARK) return saved;
  }
  return DEFAULT_THEME;
}

function create() {
  const store = writable(initialTheme());
  const { subscribe, set } = store;

  function apply(name) {
    const next = name === THEMES.LIGHT ? THEMES.LIGHT : THEMES.DARK;
    if (typeof document !== 'undefined') applyTheme(next);
    if (typeof localStorage !== 'undefined') localStorage.setItem(STORAGE_KEY, next);
    set(next);
  }

  return {
    subscribe,
    set: apply,
    /** Apply the current value to the document (call once at startup). */
    init() {
      apply(get(store));
    },
    toggle() {
      apply(get(store) === THEMES.DARK ? THEMES.LIGHT : THEMES.DARK);
    },
  };
}

export const theme = create();
