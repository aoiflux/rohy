// Keyboard shortcut map (P11).
//
// This is the SINGLE source of truth: the global handler and the help dialog both read it,
// so a shortcut can never work while the documentation claims otherwise (or vice versa).
//
// Binding choices avoid OS/browser clashes (R-QL1): navigation uses Alt+digit, which
// Windows and WebView2 leave alone, rather than Ctrl+digit (browser tabs) or bare letters
// (which would fire while typing). Bare letters are used only inside the canvas, and only
// when focus is not in a field.
import { ROUTES, UI } from './consts/index.js';

export const SHORTCUT_SCOPE = Object.freeze({
  GLOBAL: 'global',
  EVENTS: 'events',
  GRAPH: 'graph',
  TIMELINE: 'timeline',
});

// Navigation: Alt+<digit> → route.
export const NAV_KEYS = Object.freeze({
  1: ROUTES.DASHBOARD,
  2: ROUTES.EVENTS,
  3: ROUTES.GRAPH,
  4: ROUTES.RULES,
  5: ROUTES.TIMELINE,
});

export const SHORTCUTS = Object.freeze([
  { keys: '?', label: UI.SHORTCUTS_TITLE, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+1', label: UI.NAV_DASHBOARD, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+2', label: UI.NAV_EVENTS, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+3', label: UI.NAV_GRAPH, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+4', label: UI.NAV_RULES, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+5', label: UI.NAV_TIMELINE, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Ctrl+F', label: UI.SEARCH_EXPAND, scope: SHORTCUT_SCOPE.EVENTS },
  { keys: 'Enter', label: UI.ACTION_APPLY_FILTERS, scope: SHORTCUT_SCOPE.EVENTS },
  { keys: 'C', label: UI.ACTION_CONNECT_MODE, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'F', label: UI.ACTION_FIT_VIEW, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'Ctrl+A', label: UI.ACTION_SELECT_ALL, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'Shift+drag', label: UI.MARQUEE_HINT, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'Enter', label: UI.SHORTCUT_CONNECT_SELECTED, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: '← →', label: UI.SHORTCUT_SCRUB, scope: SHORTCUT_SCOPE.TIMELINE },
  { keys: 'Home / End', label: UI.SHORTCUT_SCRUB_ENDS, scope: SHORTCUT_SCOPE.TIMELINE },
  { keys: 'Esc', label: UI.SHORTCUT_ESC, scope: SHORTCUT_SCOPE.GLOBAL },
]);

/**
 * isTypingTarget reports whether the event originated in a text-entry control, where a
 * bare-letter shortcut must never fire.
 */
export function isTypingTarget(target) {
  if (!target || !target.tagName) return false;
  return (
    target.tagName === 'INPUT' ||
    target.tagName === 'TEXTAREA' ||
    target.tagName === 'SELECT' ||
    target.isContentEditable === true
  );
}

/** matchNavRoute returns the route an Alt+digit press selects, or null. */
export function matchNavRoute(e) {
  if (!e.altKey || e.ctrlKey || e.metaKey) return null;
  return NAV_KEYS[e.key] || null;
}

/** isHelpKey reports whether the press should open the shortcut help. */
export function isHelpKey(e) {
  return e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey;
}
