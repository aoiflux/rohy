// A diff of what a rule MEANS, rather than of the text that expresses it.
//
// The line diff beside this one answers "what did I type?". This answers "what did I change
// about the rule?", and the two are genuinely different questions. Reformatting a file, or
// reordering its keys, is a large line diff and no change at all. Inserting one step into a
// sequence is a one-line diff that shifts every label after it — the text says four lines
// changed; the rule says one step was inserted.
//
// It reads the SERVED schema for the field list rather than hardcoding one, so a field added to
// the format appears here the same day it appears in the form.

/** @typedef {'same'|'added'|'removed'|'changed'} FieldOp */

/**
 * fieldDiff compares two parsed rules field by field.
 *
 * Both sides may be null — an unparseable buffer, or a rule being created — and that is not an
 * error: it is reported as `comparable: false` so the view can say why it has nothing to show
 * instead of rendering an empty table that looks like "no changes".
 *
 * @param {any} before parsed rule, or null
 * @param {any} after parsed rule, or null
 * @param {{fields?: {name:string}[]}} schema
 * @returns {{comparable:boolean, rows:Array<{name:string, op:FieldOp, before:any, after:any}>, changed:number}}
 */
export function fieldDiff(before, after, schema) {
  if (!isRule(before) || !isRule(after)) {
    return { comparable: false, rows: [], changed: 0 };
  }

  // Schema order first, then any key either side carries that the schema does not know about.
  // Unknown fields are part of the rule — the format preserves them on save — so a diff that
  // dropped them would under-report a real change.
  const known = (schema?.fields || []).map((f) => f.name);
  const extra = [...new Set([...Object.keys(before), ...Object.keys(after)])]
    .filter((k) => !known.includes(k))
    .sort();

  const rows = [];
  let changed = 0;
  for (const name of [...known, ...extra]) {
    const l = before[name];
    const r = after[name];
    const op = classify(l, r);
    if (op !== 'same') changed++;
    // Fields absent from both sides are not "unchanged", they are not part of either rule.
    // Listing every unset field the format has would bury the handful that matter.
    if (op === 'same' && !present(l) && !present(r)) continue;
    rows.push({ name, op, before: l, after: r });
  }
  return { comparable: true, rows, changed };
}

/**
 * sequenceDiff aligns two sequences as CHAINS, so an inserted step reads as an insertion
 * rather than as every step after it changing.
 *
 * This is the case a field-level comparison most needs to get right. `["4625","4624"]` becoming
 * `["4625","4688","4624"]` is one step added; a positional comparison calls it "position 1
 * changed, position 2 added" and leaves the author to work out which.
 *
 * Each row carries the label on the connection LEAVING that step, because that is how the
 * format defines labels — `labels[i]` sits between `sequence[i]` and `sequence[i+1]` — and a
 * label moving with its step is the whole point of aligning them here.
 *
 * @param {{sequence?:string[], labels?:string[]}} before
 * @param {{sequence?:string[], labels?:string[]}} after
 * @returns {Array<{op:FieldOp, before:string|null, after:string|null, beforeLabel:string, afterLabel:string}>}
 */
export function sequenceDiff(before, after) {
  const a = list(before?.sequence);
  const b = list(after?.sequence);
  const la = list(before?.labels);
  const lb = list(after?.labels);

  // Longest common subsequence over the steps. Sequences are capped at 1000 entries and real
  // ones are a handful, so the exact O(n·m) table is the right trade here for the same reason
  // the line diff makes it.
  const lcs = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }

  const rows = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      // Same step — but its outgoing label may still have changed, which is a real edit to the
      // rule and must not be swallowed by the steps matching.
      rows.push(row(la[i] === lb[j] ? 'same' : 'changed', a[i], b[j], la[i], lb[j]));
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      rows.push(row('removed', a[i], null, la[i], ''));
      i++;
    } else {
      rows.push(row('added', null, b[j], '', lb[j]));
      j++;
    }
  }
  while (i < a.length) rows.push(row('removed', a[i], null, la[i++], ''));
  while (j < b.length) rows.push(row('added', null, b[j], '', lb[j++]));
  return rows;
}

function row(op, beforeStep, afterStep, beforeLabel, afterLabel) {
  return {
    op,
    before: beforeStep ?? null,
    after: afterStep ?? null,
    beforeLabel: beforeLabel ?? '',
    afterLabel: afterLabel ?? '',
  };
}

/** classify decides how one field changed. */
function classify(before, after) {
  const l = present(before);
  const r = present(after);
  if (!l && !r) return 'same';
  if (!l) return 'added';
  if (!r) return 'removed';
  return same(before, after) ? 'same' : 'changed';
}

/**
 * present reports whether a rule actually carries a value.
 *
 * An empty string and an empty array are ABSENT, matching the loader: it trims strings and
 * treats a missing tail of labels as untagged. Reporting `labels: []` → `labels` removed would
 * be a change the rule does not have.
 */
function present(v) {
  if (v === undefined || v === null) return false;
  if (Array.isArray(v)) return v.length > 0;
  if (typeof v === 'string') return v.trim() !== '';
  return true;
}

/** same compares two field values structurally, so array order counts but identity does not. */
function same(a, b) {
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((v, i) => same(v, b[i]));
  }
  if (typeof a === 'string' && typeof b === 'string') return a.trim() === b.trim();
  return a === b;
}

function isRule(v) {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

function list(v) {
  return Array.isArray(v) ? v.map((x) => (typeof x === 'string' ? x : String(x ?? ''))) : [];
}

/**
 * render turns a field value into the short string the diff shows. Long arrays are elided
 * rather than wrapped: a diff row is a summary, and the raw view is one tab away.
 */
export function render(value) {
  if (value === undefined || value === null) return '';
  if (Array.isArray(value)) {
    const shown = value.slice(0, 6).map((v) => String(v));
    return `[${shown.join(', ')}${value.length > shown.length ? `, …${value.length - shown.length} more` : ''}]`;
  }
  return String(value);
}
