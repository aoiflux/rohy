// The query console's state: the vocabulary the backend described, the query being
// built, and the last answer.
//
// 🔒 Nothing runs on its own except reading the vocabulary, which reads nothing from the case. A
// query runs when Run is pressed.
//
// A failure keeps its code (lib/api/errors.js), because the page says whose fault it was: a
// query_error is the query's, anything else the case's.
import { writable, get } from 'svelte/store';
import * as api from '../lib/api/index.js';
import { errorCode, errorMessage } from '../lib/api/errors.js';
import { buildSpec, checkSpec, fieldOf, filterable, newCondition, subjectOf } from '../lib/query/spec.js';

const empty = () => ({
  /** @type {any|null} QueryAPI.Describe's vocabulary; null until read. */
  vocab: null,
  subject: 'events',
  /** @type {{field:string, op:string, value:string}[]} */
  rows: [],
  groupBy: '',
  /** 0 means the backend's default. */
  limit: 0,
  /** @type {any|null} the last result. */
  result: null,
  running: false,
  /** @type {string|null} what the builder would be refused for, found before sending. */
  problem: null,
  /** @type {{code:string, message:string}|null} */
  error: null,
  /** The last result was dropped because a redaction ran: what it showed may be gone. */
  dropped: false,
});

function createQueryStore() {
  const store = writable(empty());
  const { subscribe, update, set } = store;

  const fail = (e) => ({ code: errorCode(e), message: errorMessage(e) });
  // Changing the question invalidates the answer and any complaint about the old question.
  const edit = (fn) => update((s) => ({ ...fn(s), problem: null }));

  return {
    subscribe,

    /** load reads the vocabulary once. */
    async load() {
      if (get(store).vocab) return;
      try {
        const vocab = await api.describeQuery();
        update((s) => ({ ...s, vocab, error: null }));
      } catch (e) {
        update((s) => ({ ...s, error: fail(e) }));
      }
    },

    /** setSubject switches between events and relations, clearing what only made sense for one. */
    setSubject(subject) {
      edit((s) => ({ ...s, subject, rows: [], groupBy: '', result: null, error: null, dropped: false }));
    },

    /** addCondition adds a row on the first field not already used. */
    addCondition() {
      edit((s) => {
        const fields = filterable(subjectOf(s.vocab, s.subject));
        const used = new Set(s.rows.map((r) => r.field));
        const field = fields.find((f) => !used.has(f.key)) ?? fields[0];
        return { ...s, rows: [...s.rows, newCondition(field)] };
      });
    },

    removeCondition(i) {
      edit((s) => ({ ...s, rows: s.rows.filter((_, j) => j !== i) }));
    },

    /** setField changes a row's field; its operator and value start again, since they were the old field's. */
    setField(i, key) {
      edit((s) => {
        const field = fieldOf(subjectOf(s.vocab, s.subject), key);
        return { ...s, rows: s.rows.map((r, j) => (j === i ? newCondition(field) : r)) };
      });
    },

    setOp(i, op) {
      edit((s) => ({ ...s, rows: s.rows.map((r, j) => (j === i ? { ...r, op } : r)) }));
    },

    setValue(i, value) {
      edit((s) => ({ ...s, rows: s.rows.map((r, j) => (j === i ? { ...r, value: String(value) } : r)) }));
    },

    setGroupBy(groupBy) {
      edit((s) => ({ ...s, groupBy }));
    },

    setLimit(limit) {
      edit((s) => ({ ...s, limit }));
    },

    /** clear removes every condition and the count, keeping the subject. */
    clear() {
      edit((s) => ({ ...s, rows: [], groupBy: '', result: null, error: null, dropped: false }));
    },

    /**
     * run checks the query, then sends it. It resolves true when an answer arrived.
     * @returns {Promise<boolean>}
     */
    async run() {
      const s = get(store);
      if (s.running || !s.vocab) return false;
      const spec = buildSpec(s);
      const problem = checkSpec(s.vocab, spec);
      if (problem) {
        update((st) => ({ ...st, problem }));
        return false;
      }
      update((st) => ({ ...st, running: true, error: null, problem: null, dropped: false }));
      try {
        const result = await api.runQuery(spec);
        update((st) => ({ ...st, running: false, result }));
        return true;
      } catch (e) {
        update((st) => ({ ...st, running: false, result: null, error: fail(e) }));
        return false;
      }
    },

    /**
     * dropResult forgets the last answer because a redaction ran: the rows may hold content that is
     * gone now. The query stays, so running it again is one press.
     */
    dropResult() {
      update((s) => (s.result ? { ...s, result: null, dropped: true } : s));
    },

    clearError() {
      update((s) => ({ ...s, error: null }));
    },

    reset() {
      set(empty());
    },
  };
}

export const query = createQueryStore();
