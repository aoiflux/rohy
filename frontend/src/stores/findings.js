// Findings store (P25). Holds the analyst's own marks on events — a flag, tags, and a note
// — plus the tag vocabulary already in use in the case.
//
// Findings are keyed by an event's hash_normalized, its content identity, NOT by node id.
// Node ids are assignment-order, so keying on one would move a note onto a different event
// after a re-ingest. Every event the UI renders carries the hash, so the key is always to
// hand.
//
// The store caches findings for the events currently on screen and refreshes them from the
// backend's answer after every write, so what is displayed is always what was persisted
// rather than what the UI hoped it wrote.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';

function create() {
  const store = writable({
    // byKey maps hash_normalized → finding, for the events currently loaded.
    byKey: /** @type {Record<string, any>} */ ({}),
    // tags is the case's tag vocabulary with usage counts, most used first.
    tags: /** @type {{tag:string,count:number}[]} */ ([]),
    stats: { total: 0, flagged: 0, noted: 0, tagged: 0 },
    // Reconciliation against the events actually present (P25). null until audited.
    audit: /** @type {{total:number,live:number,orphans:any[],stale:boolean}|null} */ (null),
    saving: false,
    error: /** @type {string|null} */ (null),
  });
  const { subscribe, update } = store;

  function fail(err) {
    update((s) => ({ ...s, saving: false, error: String(err && err.message ? err.message : err) }));
  }

  // loadFor fetches the findings for a page of events in one call. Rows without a finding
  // are simply absent from the answer, so the map is only as large as what is annotated.
  async function loadFor(keys) {
    const wanted = (keys || []).filter(Boolean);
    if (wanted.length === 0) return;
    try {
      const found = (await api.getFindings(wanted)) || {};
      update((s) => {
        // Replace the entries for the keys just asked about — including clearing any that
        // came back empty — while keeping findings for events loaded earlier.
        const byKey = { ...s.byKey };
        for (const k of wanted) delete byKey[k];
        return { ...s, byKey: { ...byKey, ...found } };
      });
    } catch (err) {
      fail(err);
    }
  }

  // save writes one annotation and folds the persisted result back in. A request that clears
  // the last of an event's content resolves to null, and the entry is dropped rather than
  // left behind as an empty finding that would still read as annotated.
  async function save(key, patch, descriptor) {
    if (!key) return null;
    const current = get(store).byKey[key] || {};
    const req = {
      key,
      flagged: patch.flagged !== undefined ? patch.flagged : !!current.flagged,
      tags: patch.tags !== undefined ? patch.tags : current.tags || [],
      note: patch.note !== undefined ? patch.note : current.note || '',
      descriptor: descriptor || current.descriptor || '',
    };

    update((s) => ({ ...s, saving: true, error: null }));
    try {
      const saved = await api.setFinding(req);
      update((s) => {
        const byKey = { ...s.byKey };
        if (saved) byKey[key] = saved;
        else delete byKey[key];
        return { ...s, byKey, saving: false };
      });
      // The tag vocabulary and the case counts both change on any write.
      refreshMeta();
      return saved;
    } catch (err) {
      fail(err);
      return null;
    }
  }

  const toggleFlag = (key, descriptor) => {
    const current = get(store).byKey[key];
    return save(key, { flagged: !(current && current.flagged) }, descriptor);
  };

  const setNote = (key, note, descriptor) => save(key, { note }, descriptor);
  const setTags = (key, tags, descriptor) => save(key, { tags }, descriptor);

  // refreshMeta reloads the tag vocabulary and case counts. Failures are ignored: these
  // drive suggestions and a summary, and neither is worth surfacing an error over.
  async function refreshMeta() {
    try {
      const [tags, stats] = await Promise.all([api.listTags(), api.findingStats()]);
      update((s) => ({ ...s, tags: tags || [], stats: stats || s.stats }));
    } catch (_) {
      /* suggestions and counts are best-effort */
    }
  }

  // audit reconciles the sidecar against the events in the case. It is a separate call from
  // refreshMeta because it costs one indexed lookup per finding: cheap, but not something to
  // repeat after every keystroke-triggered save. Callers run it when the CASE changes —
  // on mount and after an ingestion — not when a finding does.
  async function audit() {
    try {
      const res = await api.auditFindings();
      update((s) => ({ ...s, audit: res || null }));
      return res;
    } catch (err) {
      // A failed audit must not present as "no orphans": leave the previous answer in place
      // rather than replacing it with a reassuring one that was never computed.
      return null;
    }
  }

  const get_ = (key) => get(store).byKey[key] || null;

  return { subscribe, loadFor, save, toggleFlag, setNote, setTags, refreshMeta, audit, get: get_ };
}

export const findings = create();
