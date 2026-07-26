// Rule-file serialization, in rohy's house style.
//
// This is a deliberate mirror of backend/rules/format.go. The backend formatter is the
// authority — the raw editor's "Pretty" button calls it — but the guided editor has to
// re-serialize on every keystroke, and a round trip to Go for each one would make the live
// JSON preview lag behind the form that produces it.
//
// The two are kept honest by a shared fixture: backend/rules/testdata/format-cases.json is
// read by both format_test.go and format.test.js, so a change to one formatter that is not
// made to the other is a failing test rather than a guided editor that quietly writes files
// in a different shape from the rest of the library.
//
// House style: one field per line at the root, short arrays kept inline. A sequence reads as
// a sequence when its steps sit side by side, and stops reading as one when each step is on
// its own line.

// Mirrors backend consts.RuleFormatIndent / RuleFormatWidth.
export const INDENT = '  ';
export const WIDTH = 100;

/**
 * inline renders a value on one line. It is both the compact form and the measurement used
 * to decide whether the expanded form is needed.
 * @param {any} value
 * @returns {string}
 */
export function inline(value) {
  if (Array.isArray(value)) return `[${value.map(inline).join(', ')}]`;
  if (value !== null && typeof value === 'object') {
    return `{${Object.keys(value)
      .map((k) => `${JSON.stringify(k)}: ${inline(value[k])}`)
      .join(', ')}}`;
  }
  return JSON.stringify(value) ?? 'null';
}

/** columns counts display columns rather than UTF-16 units, matching the Go side's rune
 *  count — an em-dash is one column wide. */
const columns = (s) => [...s].length;

/**
 * render writes a value at the given indent depth, keeping containers on one line when they
 * fit the width budget and expanding them when they do not.
 *
 * used is how many columns the current line has already consumed, INCLUDING the "key": that
 * precedes an object value. Measuring only the value would let a short value hanging off a
 * long key overflow the budget, which is precisely the case the width limit exists for.
 *
 * @param {any} value
 * @param {number} depth
 * @param {boolean} forceExpand
 * @param {number} used
 * @returns {string}
 */
function render(value, depth, forceExpand, used) {
  const pad = INDENT.repeat(depth);
  const inner = pad + INDENT;
  const isArray = Array.isArray(value);
  const isObject = !isArray && value !== null && typeof value === 'object';

  if (!isArray && !isObject) return JSON.stringify(value) ?? 'null';
  if (!forceExpand && used + columns(inline(value)) <= WIDTH) return inline(value);

  if (isArray) {
    if (value.length === 0) return '[]';
    const items = value.map((item) => inner + render(item, depth + 1, false, columns(inner)));
    return `[\n${items.join(',\n')}\n${pad}]`;
  }

  const keys = Object.keys(value);
  if (keys.length === 0) return '{}';
  const fields = keys.map((k) => {
    const prefix = `${inner}${JSON.stringify(k)}: `;
    return prefix + render(value[k], depth + 1, false, columns(prefix));
  });
  return `{\n${fields.join(',\n')}\n${pad}}`;
}

/**
 * stringify renders a rule object as a rule file.
 *
 * Key order is the object's own insertion order, which JSON.parse preserves for string keys
 * — so a rule read in, edited in the guided form, and written back keeps the author's field
 * order and any field this build does not interpret.
 *
 * One honest limitation: values pass through JavaScript numbers, so an integer beyond 2^53
 * in a field this build does not read would lose precision here (the Go formatter preserves
 * the literal exactly). No field in the rule format is such a number, and the backend
 * formatter is what runs when the user presses Pretty.
 *
 * @param {any} value
 * @returns {string} the file text, newline-terminated
 */
export function stringify(value) {
  // The root is always expanded, even when it would fit on one line: a rule file is read
  // field by field, and collapsing a two-field rule onto a single line would make the
  // shortest rules the hardest ones to scan.
  return `${render(value, 0, true, 0)}\n`;
}

/**
 * minify strips insignificant whitespace, matching the backend's Minify.
 * @param {any} value
 * @returns {string}
 */
export function minify(value) {
  return JSON.stringify(value);
}
