// Application initialization state (P21).
//
// The window now opens before the case store is ready, so the UI has to know when the
// backend can actually answer. State arrives two ways on purpose: an event stream for
// progress, and a one-shot poll on mount — a view that starts after initialization already
// finished would otherwise wait forever for an event that has already fired.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { CHANNELS, INIT_PHASE, UI } from '../lib/consts/index.js';

function create() {
  const store = writable({
    phase: INIT_PHASE.STARTING,
    stage: UI.INIT_STARTING,
    error: '',
  });
  const { subscribe, set, update } = store;
  let wired = false;

  function apply(s) {
    if (!s || !s.phase) return;
    update((prev) => ({ ...prev, phase: s.phase, stage: s.stage || prev.stage, error: s.error || '' }));
  }

  // wire subscribes once and immediately reconciles with the current backend state.
  function wire() {
    if (wired) return;
    wired = true;
    api.on(CHANNELS.INIT_STATE, apply);
    refresh();
  }

  async function refresh() {
    try {
      apply(await api.initStatus());
    } catch (_) {
      // Outside the Wails runtime (browser dev) there is nothing to initialize; treat the
      // app as ready rather than blocking the UI behind a splash forever.
      set({ phase: INIT_PHASE.READY, stage: UI.INIT_READY, error: '' });
    }
  }

  return {
    subscribe,
    wire,
    refresh,
    isReady: () => get(store).phase === INIT_PHASE.READY,
  };
}

export const init = create();
