// The route table: every ROUTES value → the component that renders it.
//
// WHY THIS IS A TABLE AND NOT AN {#if} CHAIN. It used to be a chain in App.svelte ending in
// `{:else} <Dashboard />`, which made the catch-all do two jobs at once — "this is the
// dashboard route" and "this route is unknown". `ROUTES.MAINTENANCE` was added to the
// vocabulary, wired to Alt+7 and to the nav menu, and never given a branch; the chain fell
// through to the catch-all, so choosing Maintenance opened the Dashboard. Nothing failed:
// the import of MaintenanceView was simply unused, which is legal.
//
// A table cannot fail that way silently. `views.test.js` asserts the keys are EXACTLY
// ROUTES' values in both directions, so a route added without a view — or a view left
// behind after its route is removed — fails the suite instead of quietly showing the wrong
// screen. This is the same discipline as the autograph registry ↔ consts.Algorithms parity
// test: a name enters the vocabulary when its implementation does.
import { ROUTES } from '../lib/consts/index.js';

import Splash from './Splash.svelte';
import PermissionCheck from './PermissionCheck.svelte';
import Dashboard from './Dashboard.svelte';
import EventsView from './EventsView.svelte';
import GraphView from './GraphView.svelte';
import RulesView from './RulesView.svelte';
import TimelineView from './TimelineView.svelte';
import AlgorithmsView from './AlgorithmsView.svelte';
import MaintenanceView from './MaintenanceView.svelte';

/** @type {Readonly<Record<string, any>>} */
export const ROUTE_VIEWS = Object.freeze({
  [ROUTES.SPLASH]: Splash,
  [ROUTES.PERMISSION]: PermissionCheck,
  [ROUTES.DASHBOARD]: Dashboard,
  [ROUTES.EVENTS]: EventsView,
  [ROUTES.GRAPH]: GraphView,
  [ROUTES.RULES]: RulesView,
  [ROUTES.TIMELINE]: TimelineView,
  [ROUTES.ALGORITHMS]: AlgorithmsView,
  [ROUTES.MAINTENANCE]: MaintenanceView,
});

/**
 * viewFor resolves a route to its component.
 *
 * An unrecognised route falls back to the Dashboard, because a blank window is the worst
 * possible outcome — but note that this is now reachable ONLY by a value that is not in
 * ROUTES at all. A real route with no view is a build failure, not a fallback.
 *
 * @param {string} routeId
 */
export function viewFor(routeId) {
  return ROUTE_VIEWS[routeId] ?? Dashboard;
}
