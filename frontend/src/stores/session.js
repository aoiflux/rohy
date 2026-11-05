// The session as the shell shows it (item 41): whether this session can write, and why not.
//
// Refreshed once the case is open and after anything that can change it (unlocking the key), so
// the status bar's read-only mark and the Maintenance page agree.
import { writable } from 'svelte/store';
import * as api from '../lib/api/index.js';

function create() {
  const { subscribe, set } = writable({ loaded: false, mode: '', readOnly: '' });

  async function refresh() {
    try {
      const v = await api.signingStatus();
      set({ loaded: true, mode: v.mode || '', readOnly: v.read_only || '' });
    } catch (_) {
      // Outside the Wails runtime there is no session; nothing is marked.
      set({ loaded: false, mode: '', readOnly: '' });
    }
  }

  return { subscribe, refresh };
}

export const session = create();
