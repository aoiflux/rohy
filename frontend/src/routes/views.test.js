import { describe, it, expect } from 'vitest';
import { ROUTE_VIEWS, viewFor } from './views.js';
import { ROUTES } from '../lib/consts/index.js';
import { NAV_KEYS } from '../lib/shortcuts.js';
import Dashboard from './Dashboard.svelte';

// The parity test the routing bug needed.
//
// ROUTES.MAINTENANCE existed, was bound to Alt+7, appeared in the nav menu, and had a view
// component sitting unimported-in-effect in App.svelte — but no branch in the route chain, so
// the catch-all rendered the Dashboard. Every individual piece was correct and the feature was
// unreachable. These assertions run in both directions, because the two failures are
// different: a route with no view shows the wrong screen, and a view with no route is dead
// code that reads as shipped.
describe('routes / views', () => {
  it('every route in the vocabulary has a view', () => {
    const missing = Object.values(ROUTES).filter((id) => !ROUTE_VIEWS[id]);
    expect(missing, 'routes with no component — they would fall back to the Dashboard').toEqual([]);
  });

  it('every view in the table is a route in the vocabulary', () => {
    const known = new Set(Object.values(ROUTES));
    const extra = Object.keys(ROUTE_VIEWS).filter((id) => !known.has(id));
    expect(extra, 'components mapped to a route id that no longer exists').toEqual([]);
  });

  it('routes resolve to distinct views', () => {
    // Two routes sharing one component is how the old chain failed: `maintenance` and
    // `dashboard` both rendered the Dashboard. If two ever legitimately share a view, this
    // assertion is the place to say so deliberately.
    const seen = new Map();
    for (const [id, View] of Object.entries(ROUTE_VIEWS)) {
      const already = seen.get(View);
      expect(already, `${id} renders the same component as ${already}`).toBeUndefined();
      seen.set(View, id);
    }
  });

  it('every navigable route resolves — the nav menu cannot offer a dead destination', () => {
    // NAV_KEYS drives both the Alt+digit handler and the menu, so anything in it is one
    // keypress from the user.
    for (const id of Object.values(NAV_KEYS)) {
      expect(ROUTE_VIEWS[id], `Alt+digit route "${id}" has no view`).toBeTruthy();
    }
  });

  it('falls back to the dashboard only for an id that is not a route at all', () => {
    expect(viewFor('not-a-route')).toBe(Dashboard);
    expect(viewFor('')).toBe(Dashboard);
    expect(viewFor(undefined)).toBe(Dashboard);
    // And the fallback is never how a real route is reached.
    expect(viewFor(ROUTES.MAINTENANCE)).not.toBe(Dashboard);
  });
});
