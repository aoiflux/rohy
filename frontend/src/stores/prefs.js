// Persisted UI preferences (P9). Small, non-forensic view state — panel open/closed, the
// last filter the user typed — kept in localStorage so it survives navigation AND a restart.
//
// Nothing here is case data: losing it costs a reopened panel, never evidence. Every access
// is therefore best-effort — a browser with storage disabled, a quota error, or a corrupt
// value degrades to defaults instead of breaking the view.
import { writable, get } from 'svelte/store';
import { PREFS } from '../lib/consts/index.js';
import { emptyForm } from '../lib/filter.js';

function defaults() {
  return {
    searchOpen: PREFS.SEARCH_OPEN_DEFAULT, // the search panel starts collapsed (P9)
    filterForm: emptyForm(),
    // Timeline window as fractions of the full extent, so a zoomed-in investigation
    // survives navigation and restart (P24).
    timelineView: { start: 0, end: 1 },
    timelineGroupBy: '', // lane grouping; '' = no grouping
  };
}

function read() {
  const base = defaults();
  try {
    const raw = localStorage.getItem(PREFS.KEY);
    if (!raw) return base;
    const saved = JSON.parse(raw);
    return {
      searchOpen: typeof saved.searchOpen === 'boolean' ? saved.searchOpen : base.searchOpen,
      // Merge over the blank form so a preference saved by an older build (missing a field
      // added since) still restores cleanly instead of yielding undefined inputs.
      filterForm: { ...base.filterForm, ...(saved.filterForm || {}) },
      timelineView: { ...base.timelineView, ...(saved.timelineView || {}) },
      timelineGroupBy: saved.timelineGroupBy ?? base.timelineGroupBy,
    };
  } catch (_) {
    return base;
  }
}

function create() {
  const store = writable(read());
  const { subscribe, update } = store;

  function persist(state) {
    try {
      localStorage.setItem(PREFS.KEY, JSON.stringify(state));
    } catch (_) {
      /* storage unavailable or full — the preference simply won't outlive the session */
    }
  }

  function set(patch) {
    update((s) => {
      const next = { ...s, ...patch };
      persist(next);
      return next;
    });
  }

  return {
    subscribe,
    set,
    setSearchOpen: (open) => set({ searchOpen: !!open }),
    setFilterForm: (form) => set({ filterForm: { ...emptyForm(), ...(form || {}) } }),
    /** Synchronous read for callers that need the value before subscribing. */
    current: () => get(store),
    reset: () => set(defaults()),
  };
}

export const prefs = create();
