// Events store (P5.2 + P1). Holds the currently-loaded event rows, the accurate total of
// matching events, the active forensic filter, and loading flags. Events load
// progressively: an initial page + a total count, then more pages appended as the user
// scrolls (P1 progressive loading), so the loaded count grows toward the true total and
// the user never perceives events as missing. All reads go through the API wrapper.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { EVENTS_LIST } from '../lib/consts/index.js';
import { emptyForm, formToFilter } from '../lib/filter.js';
import { prefs } from './prefs.js';
import { UNDATED } from '../lib/consts/index.js';

export function emptyFilter() {
  return formToFilter(emptyForm());
}

// initialFilter restores the query the user last ran, so a filtered view survives both
// navigation and a restart (P9). It is read here — at store construction — rather than in
// the filter bar, so the very first load already reflects it and there is no flash of
// unfiltered results while a component mounts.
function initialFilter() {
  try {
    return formToFilter(prefs.current().filterForm);
  } catch (_) {
    return emptyFilter();
  }
}

function create() {
  const store = writable({
    list: /** @type {any[]} */ ([]),
    total: 0, // accurate total matching the active filter (P1)
    // How many events match the filter but are hidden for having no timestamp. Tracked so
    // the view can SAY they are excluded rather than quietly dropping them (P22).
    undatedHidden: 0,
    filter: initialFilter(),
    loading: false, // fresh (first-page) load
    loadingMore: false, // appending a subsequent page
    error: null,
  });
  const { subscribe, update } = store;

  function setFilter(patch) {
    update((s) => ({ ...s, filter: { ...s.filter, ...patch } }));
  }

  // load performs a FRESH load: the total count + the first page, replacing the list.
  async function load() {
    update((s) => ({ ...s, loading: true, error: null }));
    const f = { ...get(store).filter, offset: 0 };
    // The third call counts what the current view is hiding for lack of a timestamp. It is
    // only meaningful while undated events are being excluded; when the user has asked to
    // see them there is nothing hidden to report.
    const hiddenQuery =
      f.undated === UNDATED.EXCLUDE ? api.countEvents({ ...f, undated: UNDATED.ONLY }) : Promise.resolve(0);
    try {
      const [list, total, undatedHidden] = await Promise.all([
        api.queryEvents(f),
        api.countEvents(f),
        hiddenQuery.catch(() => 0),
      ]);
      update((s) => ({
        ...s,
        list: list || [],
        total: total || 0,
        undatedHidden: undatedHidden || 0,
        loading: false,
      }));
      return list;
    } catch (err) {
      update((s) => ({ ...s, loading: false, error: String(err && err.message ? err.message : err) }));
      return null;
    }
  }

  // loadMore appends the next page when there is more to load. Guarded so overlapping
  // scroll events don't fire duplicate page fetches.
  async function loadMore() {
    const s = get(store);
    if (s.loading || s.loadingMore || s.list.length >= s.total) return;
    update((x) => ({ ...x, loadingMore: true }));
    try {
      const page = await api.queryEvents({ ...s.filter, offset: s.list.length });
      update((x) => ({ ...x, list: [...x.list, ...(page || [])], loadingMore: false }));
    } catch (_) {
      update((x) => ({ ...x, loadingMore: false }));
    }
  }

  function reset() {
    update((s) => ({ ...s, filter: emptyFilter(), list: [], total: 0 }));
  }

  return { subscribe, setFilter, load, loadMore, reset };
}

export const events = create();
