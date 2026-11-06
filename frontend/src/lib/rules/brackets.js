// Bracket pairs and the keystrokes that keep them balanced, for the raw editor (plan §7.6).
//
// Its own scanner rather than the highlighter's tokenizer: the tokenizer stops at the first
// thing that is not JSON, and a half-typed buffer is the normal state of an editor, so every
// bracket below a typo would go unmatched. This one knows only strings and brackets, which
// is all that pairing needs, and it reads to the end whatever it meets.
//
// Everything here is pure: text and a caret in, an edit or a pair out. CodeArea applies the
// result, which keeps the behaviour testable without a DOM.

import { INDENT } from './format.js';

const OPENERS = Object.freeze({ '{': '}', '[': ']' });
const CLOSERS = Object.freeze({ '}': '{', ']': '[' });

/**
 * A pair is only auto-closed when what follows the caret cannot be the start of a value:
 * typing `{` in front of `"name"` means to wrap it, and a `}` appearing after the caret
 * would be in the way.
 */
const CLOSES_BEFORE = /[\s}\],:]/;

/** closesBefore reports whether a pair may be closed in front of `next`, the character after the caret. */
function closesBefore(next) {
  return next === undefined || CLOSES_BEFORE.test(next);
}

// Every scan here runs on each caret move, over rules up to the loader's 1 MiB cap, so none of
// them builds a structure the size of the text: an int stack at most, and they stop as soon as
// they have their answer.

/**
 * stringAt reads the string whose opening quote is at `start`. A closed string runs to just
 * past its closing quote; an unclosed one stops at the end of its line, because a raw newline
 * cannot be inside a JSON string and one missing quote must not swallow the file.
 * @param {string} text @param {number} start
 * @returns {{start:number, end:number, closed:boolean}}
 */
function stringAt(text, start) {
  let i = start + 1;
  while (i < text.length && text[i] !== '\n') {
    if (text[i] === '\\' && i + 1 < text.length && text[i + 1] !== '\n') {
      i += 2;
      continue;
    }
    if (text[i] === '"') return { start, end: i + 1, closed: true };
    i++;
  }
  return { start, end: i, closed: false };
}

/** spanAt returns the string the caret is inside, between its quotes, or null. */
function spanAt(text, caret) {
  let i = 0;
  while (i < caret && i < text.length) {
    if (text[i] !== '"') {
      i++;
      continue;
    }
    const span = stringAt(text, i);
    if (caret <= (span.closed ? span.end - 1 : span.end)) return span;
    i = span.end;
  }
  return null;
}

/**
 * eachBracket calls visit with every bracket outside a string, in order, until it returns true.
 * @param {string} text @param {(at:number, ch:string) => boolean|void} visit
 */
function eachBracket(text, visit) {
  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (ch === '"') {
      i = stringAt(text, i).end;
      continue;
    }
    if ((OPENERS[ch] || CLOSERS[ch]) && visit(i, ch)) return;
    i++;
  }
}

/**
 * insideString reports whether a caret at `offset` sits inside a string, between its quotes.
 * @param {string} text @param {number} offset
 */
export function insideString(text, offset) {
  return spanAt(String(text ?? ''), offset) !== null;
}

/**
 * bracketPairs maps every bracket outside a string to its partner's offset, or to -1 when it
 * has none. A closer of the wrong kind is unmatched and leaves the open bracket waiting, so
 * one typo marks one bracket rather than every bracket after it.
 * @param {string} src
 * @returns {Map<number, number>}
 */
export function bracketPairs(src) {
  const text = String(src ?? '');
  const pairs = new Map();
  const open = [];
  eachBracket(text, (i, ch) => {
    if (OPENERS[ch]) {
      open.push(i);
      return;
    }
    const top = open[open.length - 1];
    if (top !== undefined && text[top] === CLOSERS[ch]) {
      open.pop();
      pairs.set(top, i);
      pairs.set(i, top);
    } else {
      pairs.set(i, -1);
    }
  });
  for (const at of open) pairs.set(at, -1);
  return pairs;
}

/**
 * partnerOf is bracketPairs(text).get(at) without the map: the same pass, stopped at the
 * answer. Undefined when `at` is not a bracket outside a string.
 * @param {string} text @param {number} at
 * @returns {number|undefined}
 */
function partnerOf(text, at) {
  const open = [];
  let found;
  eachBracket(text, (i, ch) => {
    if (OPENERS[ch]) {
      open.push(i);
      return false;
    }
    const top = open[open.length - 1];
    const pairs = top !== undefined && text[top] === CLOSERS[ch];
    if (i === at) {
      found = pairs ? top : -1;
      return true;
    }
    if (!pairs) return false;
    open.pop();
    if (top !== at) return false;
    found = i;
    return true;
  });
  if (found === undefined && open.includes(at)) found = -1;
  return found;
}

/**
 * matchAt returns the bracket beside the caret and its partner (-1 when it has none), or
 * null. The bracket before the caret wins, since that is the one just typed or stepped over.
 * The caret is clamped: it can be one edit behind the text for the frame in between.
 * @param {string} src @param {number} caret
 * @returns {{at:number, match:number}|null}
 */
export function matchAt(src, caret) {
  const text = String(src ?? '');
  const position = Math.max(0, Math.min(caret ?? 0, text.length));
  for (const at of [position - 1, position]) {
    if (!OPENERS[text[at]] && !CLOSERS[text[at]]) continue;
    const match = partnerOf(text, at);
    if (match !== undefined) return { at, match };
  }
  return null;
}

/**
 * bracketMarks turns a match into the offsets to decorate.
 * @param {{at:number, match:number}|null} pair
 * @returns {Map<number, 'match'|'unmatched'>}
 */
export function bracketMarks(pair) {
  const marks = new Map();
  if (!pair) return marks;
  if (pair.match < 0) {
    marks.set(pair.at, 'unmatched');
  } else {
    marks.set(pair.at, 'match');
    marks.set(pair.match, 'match');
  }
  return marks;
}

/**
 * markSegments splits highlight segments so each marked offset is a one-character segment
 * of its own carrying `mark`. The text is never changed: the underlay must stay character
 * for character the same as the textarea above it.
 * @param {{text:string, type:string}[]} parts
 * @param {Map<number, string>} marks
 */
export function markSegments(parts, marks) {
  if (marks.size === 0) return parts;
  const out = [];
  let offset = 0;
  for (const part of parts) {
    const end = offset + part.text.length;
    let cursor = offset;
    for (let i = offset; i < end; i++) {
      const mark = marks.get(i);
      if (!mark) continue;
      if (i > cursor) out.push({ text: part.text.slice(cursor - offset, i - offset), type: part.type });
      out.push({ text: part.text[i - offset], type: part.type, mark });
      cursor = i + 1;
    }
    if (cursor < end) out.push(cursor === offset ? part : { text: part.text.slice(cursor - offset), type: part.type });
    offset = end;
  }
  return out;
}

/**
 * keystroke returns the edit a key makes in the raw editor, or null to leave the key to the
 * browser. The rules are the ones a JSON editor is expected to follow:
 *
 * - `{` `[` and `"` insert their pair when nothing that could be a value follows;
 * - a closer, or a string's closing quote, is typed over rather than doubled — but a closer
 *   with no partner is typed, so a missing one can always be added;
 * - Backspace inside an empty pair deletes both halves;
 * - Enter keeps the line's indent, one level deeper after an opener, and opens an empty
 *   pair onto its own lines;
 * - Tab inserts one indent.
 *
 * None of it applies inside a string, where a bracket is text.
 * @param {string} src
 * @param {number} start selectionStart
 * @param {number} end selectionEnd
 * @param {string} key KeyboardEvent.key
 * @returns {{text:string, caret:number}|null}
 */
export function keystroke(src, start, end, key) {
  const text = String(src ?? '');
  const selected = start !== end;
  const insert = (value, caretAt = value.length) => ({
    text: text.slice(0, start) + value + text.slice(end),
    caret: start + caretAt,
  });

  if (key === 'Tab') return insert(INDENT);
  if (key === 'Enter') return insideString(text, start) ? null : enter(text, start, end, insert);
  if (selected) return null;

  const before = text[start - 1];
  const after = text[start];

  if (OPENERS[key]) {
    if (insideString(text, start) || !closesBefore(after)) return null;
    return insert(key + OPENERS[key], 1);
  }

  if (CLOSERS[key]) {
    if (after !== key || insideString(text, start)) return null;
    return partnerOf(text, start) === -1 ? null : { text, caret: start + 1 };
  }

  if (key === '"') {
    const span = spanAt(text, start);
    if (span) return span.closed && span.end - 1 === start ? { text, caret: start + 1 } : null;
    // Straight after a word or another string, a quote is more likely a slip than a new value.
    if (!closesBefore(after) || /[\w"]/.test(before ?? '')) return null;
    return insert('""', 1);
  }

  if (key === 'Backspace') {
    if (start === 0) return null;
    const empty =
      before === '"'
        ? isEmptyString(spanAt(text, start), start - 1)
        : OPENERS[before] === after && !insideString(text, start - 1);
    if (!empty) return null;
    return { text: text.slice(0, start - 1) + text.slice(start + 1), caret: start - 1 };
  }

  return null;
}

/** isEmptyString reports whether span is the closed, empty string opening at `at`. */
function isEmptyString(span, at) {
  return span !== null && span.start === at && span.closed && span.end === at + 2;
}

/** enter builds the newline: the line's indent, deeper after an opener, a pair opened up. */
function enter(text, start, end, insert) {
  const lineStart = text.lastIndexOf('\n', start - 1) + 1;
  const indent = /^[ \t]*/.exec(text.slice(lineStart, start))[0];
  const before = text[start - 1];
  if (!OPENERS[before]) return insert(`\n${indent}`);
  const inner = `\n${indent}${INDENT}`;
  return OPENERS[before] === text[end] ? insert(`${inner}\n${indent}`, inner.length) : insert(inner);
}
