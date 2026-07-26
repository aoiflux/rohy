// A line diff, for showing what an edit actually changed.
//
// The editor uses it two ways: against the file as loaded, so an author can see their own
// changes before saving, and between the raw buffer and what the guided form would write,
// so switching modes is not a leap of faith.
//
// Rule files are small — the format caps them at 1 MiB and real ones are a few hundred bytes
// — so a plain O(n·m) LCS is the right choice: exact, easy to read, and fast enough that
// nothing else is worth the complexity. A cap keeps a pathological input from freezing the
// window anyway.

/** Beyond this many lines on either side the diff is reported as unavailable rather than
 *  computed; an O(n·m) table over a file that large would block the UI thread. */
export const MAX_LINES = 4000;

/**
 * @typedef {{op:'same'|'add'|'remove', text:string, left:number|null, right:number|null}} DiffRow
 */

/**
 * diffLines compares two texts line by line.
 *
 * left and right are 1-based line numbers in the respective sides, or null where that side
 * has no line — which is what lets the view render gutters that line up with the real files.
 *
 * @param {string} before
 * @param {string} after
 * @returns {{rows:DiffRow[], added:number, removed:number, truncated:boolean}}
 */
export function diffLines(before, after) {
  const a = splitLines(before);
  const b = splitLines(after);

  if (a.length > MAX_LINES || b.length > MAX_LINES) {
    return { rows: [], added: 0, removed: 0, truncated: true };
  }

  // lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:].
  const lcs = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }

  /** @type {DiffRow[]} */
  const rows = [];
  let added = 0;
  let removed = 0;
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      rows.push({ op: 'same', text: a[i], left: i + 1, right: j + 1 });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      rows.push({ op: 'remove', text: a[i], left: i + 1, right: null });
      removed++;
      i++;
    } else {
      rows.push({ op: 'add', text: b[j], left: null, right: j + 1 });
      added++;
      j++;
    }
  }
  while (i < a.length) {
    rows.push({ op: 'remove', text: a[i], left: i + 1, right: null });
    removed++;
    i++;
  }
  while (j < b.length) {
    rows.push({ op: 'add', text: b[j], left: null, right: j + 1 });
    added++;
    j++;
  }

  return { rows, added, removed, truncated: false };
}

/**
 * hasChanges is the cheap question the UI actually asks most often — whether to enable Save
 * and warn about discarding work — and does not need the diff computed to answer it.
 * @param {string} before @param {string} after
 */
export function hasChanges(before, after) {
  return normalize(before) !== normalize(after);
}

/**
 * splitLines drops a single trailing newline so a file that ends with one does not diff
 * against an otherwise identical file as having an extra blank line.
 * @param {string} text
 */
function splitLines(text) {
  const normalized = normalize(text);
  const lines = normalized.split('\n');
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop();
  return lines;
}

/** normalize folds CRLF to LF: a rule authored on Windows and one on Linux are the same
 *  rule, and showing every line as changed because of line endings would be noise. */
function normalize(text) {
  return String(text ?? '').replace(/\r\n/g, '\n');
}
