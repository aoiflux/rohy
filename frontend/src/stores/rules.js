// Rules store (P5). Holds the merged rule library (built-in + imported), the per-file
// load errors the backend reports, and loading state. Every mutation goes through the API
// wrapper and refreshes from the backend's answer, so the registry on disk is always the
// source of truth — the store never guesses what a toggle or delete did.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { RULE_SOURCES, CHANNELS } from '../lib/consts/index.js';

function create() {
  const store = writable({
    list: /** @type {any[]} */ ([]),
    errors: /** @type {{path:string,message:string}[]} */ ([]),
    dir: '',
    loading: false,
    running: false, // a rule-driven graph build is in flight (P6)
    // Per-rule progress while a build runs: { rule, rule_index, rule_total, relations }.
    progress: null,
    error: null,
  });
  const { subscribe, update } = store;

  function apply(res) {
    update((s) => ({
      ...s,
      list: (res && res.rules) || [],
      errors: (res && res.errors) || [],
      loading: false,
    }));
  }

  function fail(err) {
    update((s) => ({ ...s, loading: false, error: String(err && err.message ? err.message : err) }));
  }

  async function load() {
    update((s) => ({ ...s, loading: true, error: null }));
    try {
      const [res, dir] = await Promise.all([api.listRules(), api.rulesDir()]);
      apply(res);
      update((s) => ({ ...s, dir: dir || '' }));
    } catch (err) {
      fail(err);
    }
  }

  // reload rescans the rules folder on disk — for when the user drops a file in manually.
  async function reload() {
    update((s) => ({ ...s, loading: true, error: null }));
    try {
      apply(await api.reloadRules());
    } catch (err) {
      fail(err);
    }
  }

  async function setEnabled(id, enabled) {
    try {
      await api.setRuleEnabled(id, enabled);
      apply(await api.listRules());
      return true;
    } catch (err) {
      fail(err);
      return false;
    }
  }

  async function remove(id) {
    try {
      await api.deleteRule(id);
      apply(await api.listRules());
      return true;
    } catch (err) {
      fail(err);
      return false;
    }
  }

  // importFiles/importFolder open a native dialog in the backend and return the import
  // result ({imported, errors}) so the caller can report exactly what happened; the list
  // is refreshed either way, because a partial import still changes the library.
  async function runImport(fn) {
    update((s) => ({ ...s, loading: true, error: null }));
    try {
      const res = await fn();
      apply(await api.listRules());
      return res || { imported: [], errors: [] };
    } catch (err) {
      fail(err);
      return null;
    }
  }

  const importFiles = () => runImport(api.importRuleFiles);
  const importFolder = () => runImport(api.importRuleFolder);

  // wireRun subscribes once to rule-run progress. The call itself is synchronous (the
  // promise resolves with the full result), but a build over many rules publishes progress
  // as it goes so the UI shows movement instead of freezing on "running".
  let wired = false;
  function wireRun() {
    if (wired) return;
    wired = true;
    api.on(CHANNELS.RULES_PROGRESS, (p) => update((s) => ({ ...s, progress: p || null })));
    const done = () => update((s) => ({ ...s, running: false, progress: null }));
    api.on(CHANNELS.RULES_COMPLETE, done);
    api.on(CHANNELS.RULES_CANCELLED, done);
  }

  // run applies rules and returns the per-rule summary ({outcomes, events}). An empty
  // ruleIds runs every enabled rule; filter scopes the dataset. Returns null on failure
  // (the error is already on the store), so callers can bail without a second try/catch.
  async function run(ruleIds, filter) {
    wireRun();
    update((s) => ({ ...s, running: true, progress: null, error: null }));
    try {
      const res = await api.runRules({ rule_ids: ruleIds || [], filter: filter || {} });
      update((s) => ({ ...s, running: false, progress: null }));
      return res || { outcomes: [], events: 0 };
    } catch (err) {
      update((s) => ({
        ...s,
        running: false,
        progress: null,
        error: String(err && err.message ? err.message : err),
      }));
      return null;
    }
  }

  // cancelRun stops an in-flight build. The partial result still returns to the caller, so
  // graphs already rebuilt are kept rather than silently discarded.
  async function cancelRun() {
    try {
      await api.cancelRuleRun();
    } catch (_) {
      /* nothing running */
    }
  }

  // source fetches a rule's file as authored, for the inspector. Returns null on failure
  // (the message is already on the store), so the caller can just bail.
  async function source(id) {
    try {
      return await api.ruleSource(id);
    } catch (err) {
      fail(err);
      return null;
    }
  }

  // enabledCount powers the "N of M enabled" summary without a second backend call.
  function enabledCount() {
    return get(store).list.filter((r) => r.enabled).length;
  }

  function isDeletable(rule) {
    return rule && rule.source === RULE_SOURCES.USER;
  }

  return {
    subscribe,
    load,
    reload,
    setEnabled,
    remove,
    importFiles,
    importFolder,
    run,
    cancelRun,
    source,
    enabledCount,
    isDeletable,
  };
}

export const rules = create();
