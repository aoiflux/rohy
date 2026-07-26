import { describe, it, expect } from 'vitest';
import {
  SHORTCUTS,
  NAV_KEYS,
  NAV_ITEMS,
  navLabel,
  isTypingTarget,
  matchNavRoute,
  isHelpKey,
} from './shortcuts.js';
import { ROUTES } from './consts/index.js';

const key = (over = {}) => ({ key: '1', altKey: false, ctrlKey: false, metaKey: false, ...over });

describe('navigation shortcuts', () => {
  it('maps Alt+digit to each main route', () => {
    expect(matchNavRoute(key({ key: '1', altKey: true }))).toBe(ROUTES.DASHBOARD);
    expect(matchNavRoute(key({ key: '2', altKey: true }))).toBe(ROUTES.EVENTS);
    expect(matchNavRoute(key({ key: '3', altKey: true }))).toBe(ROUTES.GRAPH);
    expect(matchNavRoute(key({ key: '4', altKey: true }))).toBe(ROUTES.RULES);
  });

  it('ignores a bare digit, so typing a number never navigates', () => {
    expect(matchNavRoute(key({ key: '2' }))).toBeNull();
  });

  it('ignores Ctrl/Cmd+digit, which belong to the browser/OS (R-QL1)', () => {
    expect(matchNavRoute(key({ key: '2', ctrlKey: true }))).toBeNull();
    expect(matchNavRoute(key({ key: '2', metaKey: true }))).toBeNull();
    // Even with Alt also held, a Ctrl/Cmd combo is not ours to take.
    expect(matchNavRoute(key({ key: '2', altKey: true, ctrlKey: true }))).toBeNull();
  });

  it('ignores digits outside the mapped range', () => {
    expect(matchNavRoute(key({ key: '9', altKey: true }))).toBeNull();
  });
});

describe('help key', () => {
  it('opens on a plain ?', () => {
    expect(isHelpKey(key({ key: '?' }))).toBe(true);
  });

  it('ignores ? with a modifier', () => {
    expect(isHelpKey(key({ key: '?', ctrlKey: true }))).toBe(false);
    expect(isHelpKey(key({ key: '?', altKey: true }))).toBe(false);
  });
});

describe('isTypingTarget', () => {
  it('protects text-entry controls from bare-key shortcuts', () => {
    expect(isTypingTarget({ tagName: 'INPUT' })).toBe(true);
    expect(isTypingTarget({ tagName: 'TEXTAREA' })).toBe(true);
    expect(isTypingTarget({ tagName: 'SELECT' })).toBe(true);
    expect(isTypingTarget({ tagName: 'DIV', isContentEditable: true })).toBe(true);
  });

  it('allows shortcuts elsewhere', () => {
    expect(isTypingTarget({ tagName: 'DIV' })).toBe(false);
    expect(isTypingTarget({ tagName: 'BUTTON' })).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });
});

describe('documented map', () => {
  it('documents every navigation binding that is implemented', () => {
    // The dialog is generated from SHORTCUTS, so a binding missing here is a binding the
    // user can never discover.
    for (const digit of Object.keys(NAV_KEYS)) {
      expect(SHORTCUTS.some((s) => s.keys === `Alt+${digit}`)).toBe(true);
    }
  });

  it('has a label and scope for every entry', () => {
    for (const s of SHORTCUTS) {
      expect(s.keys, `keys missing on ${JSON.stringify(s)}`).toBeTruthy();
      expect(s.label, `label missing for ${s.keys}`).toBeTruthy();
      expect(s.scope, `scope missing for ${s.keys}`).toBeTruthy();
    }
  });

  it('lists no duplicate bindings within a scope', () => {
    const seen = new Set();
    for (const s of SHORTCUTS) {
      const id = `${s.scope}:${s.keys}`;
      expect(seen.has(id), `duplicate binding ${id}`).toBe(false);
      seen.add(id);
    }
  });
});

describe('navigation menu', () => {
  // The app bars used to spell this list out as text buttons, four per view. Now one control
  // renders NAV_ITEMS — so the menu and the keys must describe the same set, or a route
  // becomes reachable by one and not the other.
  it('offers exactly the routes the Alt+digit keys reach', () => {
    expect(NAV_ITEMS.map((i) => i.id).sort()).toEqual(Object.values(NAV_KEYS).sort());
  });

  it('shows each entry its own key', () => {
    for (const item of NAV_ITEMS) {
      const digit = Object.keys(NAV_KEYS).find((d) => NAV_KEYS[d] === item.id);
      expect(item.keys).toBe(`Alt+${digit}`);
    }
  });

  it('labels every entry', () => {
    for (const item of NAV_ITEMS) {
      expect(item.label, `no label for ${item.id}`).toBeTruthy();
      expect(navLabel(item.id)).toBe(item.label);
    }
  });

  // Splash and the permission check are deliberately absent: they are states the app puts
  // you in, not places to navigate to.
  it('does not offer routes that are not destinations', () => {
    expect(NAV_ITEMS.map((i) => i.id)).not.toContain(ROUTES.SPLASH);
    expect(NAV_ITEMS.map((i) => i.id)).not.toContain(ROUTES.PERMISSION);
  });

  it('returns nothing for a route with no menu entry', () => {
    expect(navLabel(ROUTES.SPLASH)).toBe('');
    expect(navLabel('nope')).toBe('');
  });
});
