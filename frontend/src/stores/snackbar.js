// Central snackbar service (P5.4). A single queue of transient messages rendered by
// SnackbarHost. Permission warnings and ingestion errors are routed here so there is
// one notification surface for the whole app.
import { writable } from 'svelte/store';

export const SNACK_KIND = Object.freeze({
  INFO: 'info',
  SUCCESS: 'success',
  WARNING: 'warning',
  ERROR: 'error',
});

const DEFAULT_TIMEOUT_MS = 6000;
let seq = 0;

function create() {
  const { subscribe, update } = writable(/** @type {any[]} */ ([]));

  function dismiss(id) {
    update((list) => list.filter((x) => x.id !== id));
  }

  function show(message, opts = {}) {
    const id = ++seq;
    const item = {
      id,
      message,
      kind: opts.kind || SNACK_KIND.INFO,
      timeout: opts.timeout ?? DEFAULT_TIMEOUT_MS,
      action: opts.action || null, // { label, run }
    };
    update((list) => [...list, item]);
    if (item.timeout > 0 && typeof setTimeout !== 'undefined') {
      setTimeout(() => dismiss(id), item.timeout);
    }
    return id;
  }

  return {
    subscribe,
    show,
    dismiss,
    info: (m, o) => show(m, { ...o, kind: SNACK_KIND.INFO }),
    success: (m, o) => show(m, { ...o, kind: SNACK_KIND.SUCCESS }),
    warn: (m, o) => show(m, { ...o, kind: SNACK_KIND.WARNING }),
    error: (m, o) => show(m, { ...o, kind: SNACK_KIND.ERROR }),
  };
}

export const snackbar = create();
