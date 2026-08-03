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
  EDITOR: 'editor',
});

// Navigation: Alt+<digit> → route.
export const NAV_KEYS = Object.freeze({
  1: ROUTES.DASHBOARD,
  2: ROUTES.EVENTS,
  3: ROUTES.GRAPH,
  4: ROUTES.RULES,
  5: ROUTES.TIMELINE,
  6: ROUTES.ALGORITHMS,
});

const NAV_LABELS = Object.freeze({
  [ROUTES.DASHBOARD]: UI.NAV_DASHBOARD,
  [ROUTES.EVENTS]: UI.NAV_EVENTS,
  [ROUTES.GRAPH]: UI.NAV_GRAPH,
  [ROUTES.RULES]: UI.NAV_RULES,
  [ROUTES.TIMELINE]: UI.NAV_TIMELINE,
  [ROUTES.ALGORITHMS]: UI.NAV_ALGORITHMS,
});

/**
 * NAV_ITEMS is the navigation menu, derived from NAV_KEYS so the menu and the keys cannot
 * disagree: a route reachable by Alt+<digit> is in the menu, with that key shown beside it,
 * and one that is not reachable cannot appear.
 *
 * Every view used to repeat this list as four text buttons in its app bar, which is what
 * made the bars crowded before their own actions were even counted.
 *
 * @type {ReadonlyArray<{id:string, label:string, keys:string}>}
 */
export const NAV_ITEMS = Object.freeze(
  Object.entries(NAV_KEYS).map(([digit, id]) => ({ id, label: NAV_LABELS[id], keys: `Alt+${digit}` })),
);

/** navLabel returns a route's display name, for the app bar's current-route trigger. */
export function navLabel(routeId) {
  return NAV_LABELS[routeId] || '';
}

export const SHORTCUTS = Object.freeze([
  { keys: '?', label: UI.SHORTCUTS_TITLE, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+1', label: UI.NAV_DASHBOARD, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+2', label: UI.NAV_EVENTS, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+3', label: UI.NAV_GRAPH, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+4', label: UI.NAV_RULES, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+5', label: UI.NAV_TIMELINE, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Alt+6', label: UI.NAV_ALGORITHMS, scope: SHORTCUT_SCOPE.GLOBAL },
  { keys: 'Ctrl+F', label: UI.SEARCH_EXPAND, scope: SHORTCUT_SCOPE.EVENTS },
  { keys: 'Enter', label: UI.ACTION_APPLY_FILTERS, scope: SHORTCUT_SCOPE.EVENTS },
  { keys: 'C', label: UI.ACTION_CONNECT_MODE, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'F', label: UI.ACTION_FIT_VIEW, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'R / Shift+R', label: UI.SHORTCUT_STEP_RELATIONS, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'Ctrl+A', label: UI.ACTION_SELECT_ALL, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'Shift+drag', label: UI.MARQUEE_HINT, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: 'Enter', label: UI.SHORTCUT_CONNECT_SELECTED, scope: SHORTCUT_SCOPE.GRAPH },
  { keys: '← →', label: UI.SHORTCUT_SCRUB, scope: SHORTCUT_SCOPE.TIMELINE },
  { keys: 'Home / End', label: UI.SHORTCUT_SCRUB_ENDS, scope: SHORTCUT_SCOPE.TIMELINE },
  // Rule editor (P26). All modified keys: the editor is full of text fields, so a bare
  // letter here would fire while somebody is typing an event ID or a description.
  { keys: 'Ctrl+S', label: UI.RULE_EDITOR_SAVE, scope: SHORTCUT_SCOPE.EDITOR },
  { keys: 'Ctrl+Z', label: UI.SHORTCUT_UNDO, scope: SHORTCUT_SCOPE.EDITOR },
  { keys: 'Ctrl+Shift+Z', label: UI.SHORTCUT_REDO, scope: SHORTCUT_SCOPE.EDITOR },
  { keys: 'Ctrl+Shift+F', label: UI.RULE_EDITOR_PRETTY, scope: SHORTCUT_SCOPE.EDITOR },
  { keys: 'Ctrl+E', label: UI.SHORTCUT_TOGGLE_MODE, scope: SHORTCUT_SCOPE.EDITOR },
  { keys: 'Ctrl+Space', label: UI.RULE_EDITOR_COMPLETIONS, scope: SHORTCUT_SCOPE.EDITOR },
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
