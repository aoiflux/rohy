// The query console's pure half: turn the builder's rows into a spec, check it against the
// vocabulary the backend described, and format what comes back.
//
// 🔒 The vocabulary comes from QueryAPI.Describe, never from a list kept here, so the page offers
// exactly what Run accepts. checkSpec repeats the backend's rules so a mistake is named before the
// round trip; the backend stays the authority and refuses anything this misses.
import { UI } from '../consts/index.js';

export const KIND = Object.freeze({ TEXT: 'text', ENUM: 'enum', TIME: 'time', NUMBER: 'number' });
export const OP = Object.freeze({ EQ: 'eq', CONTAINS: 'contains', GTE: 'gte', LTE: 'lte' });

const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;
const WHOLE = /^\d+$/;

/** subjectOf is the vocabulary's entry for a subject, or null. */
export function subjectOf(vocab, key) {
  return vocab?.subjects?.find((s) => s.key === key) ?? null;
}

/** fieldOf is a subject's field, or null. */
export function fieldOf(subject, key) {
  return subject?.fields?.find((f) => f.key === key) ?? null;
}

/** filterable is the fields a condition can test: those with an operator. */
export function filterable(subject) {
  return (subject?.fields ?? []).filter((f) => (f.ops ?? []).length > 0);
}

/** groupable is the fields a count can be grouped by. */
export function groupable(subject) {
  return (subject?.fields ?? []).filter((f) => f.groupable);
}

/** newCondition is a row for field, with its first operator and its default or first choice. */
export function newCondition(field) {
  if (!field) return { field: '', op: '', value: '' };
  const value = field.default ?? (field.kind === KIND.ENUM ? field.values?.[0] ?? '' : '');
  return { field: field.key, op: field.ops[0], value };
}

/**
 * buildSpec is what runQuery sends. Every value is a string and the limit an integer, whatever the
 * input produced, so the binding never sees a JSON type it has to guess at.
 */
export function buildSpec({ subject, rows, groupBy, limit }) {
  return {
    subject: String(subject ?? ''),
    where: (rows ?? []).map((r) => ({ field: String(r.field ?? ''), op: String(r.op ?? ''), value: String(r.value ?? '') })),
    group_by: String(groupBy ?? ''),
    limit: Number.isInteger(Number(limit)) ? Number(limit) : NaN,
  };
}

/**
 * checkSpec returns the first thing the backend would refuse in spec, in words, or null.
 * @returns {string|null}
 */
export function checkSpec(vocab, spec) {
  const subject = subjectOf(vocab, spec.subject);
  if (!subject) return UI.QUERY_PROBLEM_SUBJECT;
  if (spec.where.length > vocab.max_conditions) return UI.QUERY_PROBLEM_TOO_MANY(vocab.max_conditions);
  if (!Number.isInteger(spec.limit) || spec.limit < 0 || spec.limit > vocab.max_limit) {
    return UI.QUERY_PROBLEM_LIMIT(vocab.max_limit);
  }
  if (spec.group_by && !fieldOf(subject, spec.group_by)?.groupable) return UI.QUERY_PROBLEM_GROUP;
  const seen = new Set();
  let from = null;
  let to = null;
  for (const c of spec.where) {
    const f = fieldOf(subject, c.field);
    if (!f) return UI.QUERY_PROBLEM_FIELD;
    const label = f.label;
    if (!(f.ops ?? []).includes(c.op)) return UI.QUERY_PROBLEM_OP(label);
    const key = f.kind === KIND.TIME ? `${c.field}/${c.op}` : c.field;
    if (seen.has(key)) return UI.QUERY_PROBLEM_TWICE(label);
    seen.add(key);
    if (c.value === '') return UI.QUERY_PROBLEM_EMPTY(label);
    if (new TextEncoder().encode(c.value).length > vocab.max_value_length) return UI.QUERY_PROBLEM_LONG(label);
    if (f.kind === KIND.ENUM && !(f.values ?? []).includes(c.value)) return UI.QUERY_PROBLEM_ENUM(label);
    if (f.kind === KIND.NUMBER && !WHOLE.test(c.value)) return UI.QUERY_PROBLEM_NUMBER(label);
    if (f.kind === KIND.TIME) {
      const t = Date.parse(c.value);
      if (!RFC3339.test(c.value) || Number.isNaN(t)) return UI.QUERY_PROBLEM_TIME(label);
      if (c.op === OP.GTE) from = t;
      else to = t;
    }
  }
  if (from !== null && to !== null && to < from) return UI.QUERY_PROBLEM_RANGE;
  return null;
}

/** cell is one value of a result row, as text. A redacted event's blanks say so. */
export function cell(row, key) {
  const v = row?.[key];
  if (key === 'timestamp') return v ? v : UI.QUERY_UNDATED;
  if (key === 'basis') return Array.isArray(v) ? v.join('; ') : '';
  if (key === 'confidence') return typeof v === 'number' ? v.toFixed(2) : '';
  if (row?.redacted && (v === '' || v === undefined)) return UI.QUERY_REDACTED;
  return v === undefined || v === null ? '' : String(v);
}

/** groupLabel is a group's value as text: the blank and redacted buckets are named, not empty. */
export function groupLabel(g) {
  if (g.redacted) return UI.QUERY_REDACTED_GROUP;
  return g.value === '' ? UI.QUERY_BLANK : g.value;
}

/** shown is how many rows or groups the result holds. */
export function shown(result) {
  if (!result) return 0;
  return result.group_by ? (result.groups ?? []).length : (result.events ?? []).length + (result.relations ?? []).length;
}

/** statusLines is what a result says about itself: how much is shown, what was left out, and why. */
export function statusLines(result) {
  if (!result) return [];
  const lines = [];
  const n = shown(result);
  if (result.group_by) {
    lines.push(UI.QUERY_STATUS_GROUPS(n, result.distinct_groups, result.total));
  } else {
    lines.push(UI.QUERY_STATUS_ROWS(n, result.total));
  }
  if (result.truncated) lines.push(UI.QUERY_STATUS_TRUNCATED);
  if (result.subject === 'events' && result.undated) lines.push(UI.QUERY_STATUS_UNDATED[result.undated] ?? '');
  if (result.changed) lines.push(UI.QUERY_STATUS_CHANGED);
  return lines.filter(Boolean);
}
