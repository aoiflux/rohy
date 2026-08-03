// Rule editor session state (P26).
//
// One editor is open at a time, holding one document that both modes project. This store
// owns that document, its undo history, and the validation results — and it is the only
// caller of the editor's backend bindings, matching the discipline the rest of the app
// follows: stores talk to lib/api, components talk to stores.
//
// Validation runs at two tiers. The local mirror in lib/rules/validate.js answers on every
// keystroke so a mistake is underlined as it is made; the backend answers on a debounce and
// before every save, and it is the authority. Where they disagree the backend wins, because
// it is the code that will actually decide whether the saved file loads.

import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { rules } from './rules.js';
import { EDITOR_MODES, RULE_SOURCES, UI } from '../lib/consts/index.js';
import {
  createDocument,
  fromSchemaDefaults,
  isFormEditable,
  patch,
  patchAll,
  slug,
} from '../lib/rules/document.js';
import { validate, collisionWarning } from '../lib/rules/validate.js';
import { createHistory } from '../lib/rules/history.js';
import { hasChanges } from '../lib/rules/diff.js';
import { stringify } from '../lib/rules/format.js';

/** How long to wait after the last keystroke before asking the backend. Long enough that
 *  typing does not produce a call per character, short enough that the authoritative answer
 *  arrives before the user reaches for Save. */
const VALIDATE_DEBOUNCE_MS = 250;

function emptyState() {
  return {
    open: false,
    mode: EDITOR_MODES.RAW,
    /** the rule being edited; '' when creating one */
    editingId: '',
    /** the rule being duplicated, for the header — a builtin cannot be edited in place */
    duplicatingFrom: '',
    /** a broken file being repaired; retired on save, since the fix rarely keeps its name */
    replacePath: '',
    doc: createDocument(''),
    /** the text as loaded, for the diff and the unsaved-changes guard */
    original: '',
    schema: null,
    /** the authoritative report from the backend; null until the first round trip */
    report: null,
    /** the local mirror's result, shown between round trips */
    local: null,
    saving: false,
    loading: false,
    /** the last testbench result, or null when the buffer has changed since one was run */
    test: null,
    testing: false,
    error: null,
  };
}

function create() {
  const store = writable(emptyState());
  const { subscribe, update, set } = store;

  let history = createHistory({ text: '', mode: EDITOR_MODES.RAW });
  let debounce = null;
  /** Guards against a slow validation response overwriting a newer one. */
  let validateSeq = 0;

  /** schema is fetched once and kept: it only changes when the build does. */
  let cachedSchema = null;
  async function loadSchema() {
    if (cachedSchema) return cachedSchema;
    cachedSchema = await api.ruleSchema();
    return cachedSchema;
  }

  // --- validation ---

  function runLocal(state) {
    const local = validate(state.doc.text, state.schema);
    const clash = local.value ? collisionWarning(local.value.name, state.editingId, get(rules).list) : null;
    if (clash) local.warnings = [...local.warnings, clash];
    return local;
  }

  function scheduleBackendValidation() {
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(async () => {
      debounce = null;
      const seq = ++validateSeq;
      const state = get(store);
      if (!state.open) return;
      try {
        const report = await api.validateRule(state.doc.text, state.editingId);
        // A response for text the user has already moved on from is stale; dropping it stops
        // errors flickering back after they have been fixed.
        if (seq !== validateSeq) return;
        update((s) => (s.open ? { ...s, report } : s));
      } catch (err) {
        update((s) => ({ ...s, error: message(err) }));
      }
    }, VALIDATE_DEBOUNCE_MS);
  }

  /** applyDoc is the single path every edit takes: recompute the local report, record
   *  history, and ask the backend on a debounce. */
  function applyDoc(doc, { coalesce = false } = {}) {
    update((s) => {
      const next = { ...s, doc };
      next.local = runLocal(next);
      // The backend's answer is about the previous text until the new one comes back.
      // Keeping the stale report would let Save stay enabled on text that has since broken.
      next.report = null;
      // Same reasoning for the testbench, and it matters more: a match count computed against
      // text the author has since edited looks current and is not, and "12 matches" is exactly
      // the kind of number somebody acts on.
      next.test = null;
      return next;
    });
    history.push({ text: doc.text, mode: get(store).mode }, { coalesce });
    scheduleBackendValidation();
  }

  // --- opening ---

  async function begin({
    editingId = '',
    duplicatingFrom = '',
    replacePath = '',
    text = null,
    mode = EDITOR_MODES.RAW,
  }) {
    set({ ...emptyState(), open: true, loading: true, mode });
    try {
      const schema = await loadSchema();
      const doc = text === null ? fromSchemaDefaults(schema) : createDocument(text);
      history = createHistory({ text: doc.text, mode });
      update((s) => ({
        ...s,
        loading: false,
        schema,
        editingId,
        duplicatingFrom,
        replacePath,
        doc,
        original: doc.text,
      }));
      update((s) => ({ ...s, local: runLocal(s) }));
      scheduleBackendValidation();
    } catch (err) {
      update((s) => ({ ...s, loading: false, error: message(err) }));
    }
  }

  /** create opens a new rule seeded from the schema, in guided mode — someone who has not
   *  written a rule before should meet the form, not a JSON buffer. */
  const createNew = () => begin({ mode: EDITOR_MODES.GUIDED });

  /**
   * edit opens an existing user rule from its file on disk, verbatim. It reads the source
   * rather than re-serializing the parsed rule so the author sees what they wrote —
   * formatting, field order, and any field this build does not interpret.
   */
  async function edit(rule) {
    const src = await api.ruleSource(rule.id);
    return begin({ editingId: rule.id, text: src?.source ?? '', mode: EDITOR_MODES.RAW });
  }

  /**
   * duplicate opens a copy of a rule as a NEW rule. It is the only way to vary a built-in:
   * built-ins live in the binary and cannot be written to, so the copy takes a distinct name
   * and saves as a create.
   */
  async function duplicate(rule) {
    const src = await api.ruleSource(rule.id);
    const doc = createDocument(src?.source ?? '');
    let text = doc.text;
    if (isFormEditable(doc)) {
      text = stringify({ ...doc.value, name: `${doc.value.name} ${UI.RULE_EDITOR_COPY_SUFFIX}` });
    }
    return begin({ duplicatingFrom: rule.name, text, mode: EDITOR_MODES.RAW });
  }

  /**
   * fix opens a rule file that failed to load, so a broken file can be repaired in the app
   * rather than by leaving it. The path travels with the session: the repaired rule is
   * written under the id its name produces, which is rarely the filename it had, so the
   * original has to be retired or the directory would keep reporting it as broken beside the
   * copy that replaced it.
   */
  const fix = (text, path) => begin({ text, replacePath: path || '', mode: EDITOR_MODES.RAW });

  function close() {
    if (debounce) clearTimeout(debounce);
    debounce = null;
    validateSeq++;
    set(emptyState());
  }

  // --- editing ---

  /** setText is the raw editor's channel. coalesce marks ordinary typing, so undo steps by
   *  edit rather than by character. */
  function setText(text, { coalesce = true } = {}) {
    applyDoc(createDocument(text), { coalesce });
  }

  /** setField is the guided editor's channel: it patches one key and leaves everything else,
   *  including fields this build does not interpret, exactly as it found them. */
  function setField(key, value) {
    const state = get(store);
    applyDoc(patch(state.doc, key, value), { coalesce: true });
  }

  /** setFields applies several keys as one edit, so a form action that changes a step and
   *  its label is one undo entry rather than two. */
  function setFields(changes) {
    const state = get(store);
    applyDoc(patchAll(state.doc, changes), { coalesce: false });
  }

  /**
   * setMode switches projections.
   *
   * Guided → raw always works. Raw → guided is refused when the text does not parse: the
   * form would otherwise open seeded from a partial read of a broken document, and saving it
   * would silently discard whatever it could not understand.
   *
   * @returns {boolean} whether the switch happened
   */
  function setMode(mode) {
    const state = get(store);
    if (mode === state.mode) return true;
    if (mode === EDITOR_MODES.GUIDED && !isFormEditable(state.doc)) return false;
    update((s) => ({ ...s, mode }));
    // A structural action, so it is its own history entry: undo lands on the state before
    // the switch rather than somewhere inside it.
    history.push({ text: state.doc.text, mode }, { coalesce: false });
    return true;
  }

  /**
   * testbench runs the rule against the real case and reports what it WOULD produce, without
   * writing anything.
   *
   * It asks the BACKEND rather than approximating a match locally, for the same reason
   * validation does: an approximation would eventually disagree with the engine, and it would
   * disagree in the direction that matters — telling an author a rule fires when it does not.
   *
   * The buffer is sent as-is, so a rule that has never been saved can be tried. Unparseable
   * text is not an error here; the result carries the located problems the editor already
   * shows, and no evaluation is attempted.
   */
  async function testbench(filter = {}) {
    const state = get(store);
    update((s) => ({ ...s, testing: true, error: null }));
    try {
      const result = await api.dryRunRule(state.doc.text, filter, UI.RULE_TESTBENCH_SAMPLES);
      update((s) => (s.open ? { ...s, testing: false, test: result } : s));
      return result;
    } catch (err) {
      update((s) => ({ ...s, testing: false, error: message(err) }));
      return null;
    }
  }

  /** clearTest drops a stale result. Called on every edit: a result computed against text the
   *  author has since changed is worse than no result, because it looks current. */
  function clearTest() {
    update((s) => (s.test === null ? s : { ...s, test: null }));
  }

  async function format(minify = false) {
    const state = get(store);
    try {
      const text = await api.formatRule(state.doc.text, minify);
      applyDoc(createDocument(text), { coalesce: false });
      return true;
    } catch (err) {
      update((s) => ({ ...s, error: message(err) }));
      return false;
    }
  }

  function undo() {
    const entry = history.undo();
    if (!entry) return false;
    restore(entry);
    return true;
  }

  function redo() {
    const entry = history.redo();
    if (!entry) return false;
    restore(entry);
    return true;
  }

  /** restore moves to a history entry without recording a new one. */
  function restore(entry) {
    update((s) => {
      const doc = createDocument(entry.text);
      const next = { ...s, doc, mode: entry.mode ?? s.mode, report: null };
      next.local = runLocal(next);
      return next;
    });
    scheduleBackendValidation();
  }

  // --- saving ---

  /**
   * save writes the rule and refreshes the library.
   *
   * It validates through the backend first even though the local mirror has already run:
   * the mirror exists for latency, and the file about to be written has to be judged by the
   * code that will later decide whether it loads.
   *
   * @returns {Promise<{rule:object, created:boolean, renamed:boolean, previous_id?:string}|null>}
   */
  async function save() {
    const state = get(store);
    update((s) => ({ ...s, saving: true, error: null }));
    try {
      const report = await api.validateRule(state.doc.text, state.editingId);
      if (!report.valid) {
        update((s) => ({ ...s, saving: false, report }));
        return null;
      }
      const result = await api.saveRule({
        id: state.editingId,
        source: state.doc.text,
        replace_path: state.replacePath,
      });
      await rules.load();
      update((s) => ({
        ...s,
        saving: false,
        original: s.doc.text,
        editingId: result.rule.id,
        replacePath: '', // retired by the write; a second save must not try again
      }));
      return result;
    } catch (err) {
      update((s) => ({ ...s, saving: false, error: message(err) }));
      return null;
    }
  }

  // --- derived questions the UI asks ---

  /** dirty reports unsaved work, for the close guard. */
  function dirty() {
    const s = get(store);
    return s.open && hasChanges(s.original, s.doc.text);
  }

  /** renameTarget reports the id this rule would take, when that differs from the one it
   *  has. The id is a slug of the name, so a rename replaces the rule's identity — and the
   *  graph the old id built stops resolving to it. The UI must say so before saving. */
  function renameTarget() {
    const s = get(store);
    if (!s.editingId || !s.doc.value) return null;
    const next = slug(s.doc.value.name);
    return next && next !== s.editingId ? { from: s.editingId, to: next } : null;
  }

  /** problems returns the errors to show: the backend's when it has answered, the local
   *  mirror's while it has not. */
  function problemsOf(state) {
    if (state.report) return { errors: state.report.errors || [], warnings: state.report.warnings || [] };
    if (state.local) return { errors: state.local.errors || [], warnings: state.local.warnings || [] };
    return { errors: [], warnings: [] };
  }

  function canSave(state) {
    if (!state.open || state.saving || state.loading) return false;
    return problemsOf(state).errors.length === 0;
  }

  function isEditable(rule) {
    return rule && rule.source === RULE_SOURCES.USER;
  }

  return {
    subscribe,
    createNew,
    edit,
    duplicate,
    fix,
    close,
    setText,
    setField,
    setFields,
    setMode,
    format,
    undo,
    redo,
    save,
    dirty,
    renameTarget,
    problemsOf,
    canSave,
    isEditable,
    testbench,
    clearTest,
    canUndo: () => history.canUndo(),
    canRedo: () => history.canRedo(),
  };
}

function message(err) {
  return String(err && err.message ? err.message : err);
}

export const ruleEditor = create();
