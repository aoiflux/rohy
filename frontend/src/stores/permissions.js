// Permissions store (P5.2, closes P1's frontend items). Holds the latest privilege
// snapshot and the access decision for the protected channels, refreshed on demand.
// A blocked decision is surfaced through the snackbar.
import { writable } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { PROTECTED_CHANNELS } from '../lib/consts/index.js';
import { snackbar } from './snackbar.js';

function create() {
  const { subscribe, set, update } = writable({
    checked: false,
    status: { platform: '', elevated: false, administrator: false },
    decision: { needed: false, blocked_channels: [], message: '' },
    error: null,
  });

  /** Refresh privilege state + protected-channel access. Warns via snackbar if blocked. */
  async function refresh() {
    try {
      const status = await api.checkPermissions();
      const decision = await api.evaluateAccess(PROTECTED_CHANNELS);
      set({ checked: true, status, decision, error: null });
      if (decision && decision.needed && decision.message) {
        snackbar.warn(decision.message);
      }
      return { status, decision };
    } catch (err) {
      update((s) => ({ ...s, checked: true, error: String(err && err.message ? err.message : err) }));
      return null;
    }
  }

  return { subscribe, refresh };
}

export const permissions = create();
