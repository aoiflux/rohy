// A JSON tokenizer for the raw editor's syntax highlighting.
//
// It is a tokenizer rather than a set of regexes because a key and a string value are the
// same lexical thing and only position tells them apart — and colouring them identically is
// exactly what makes hand-edited JSON hard to read. The token stream is also what the
// completion logic navigates to work out what the caret is sitting in, so the two features
// share one understanding of the text instead of two approximations of it.
//
// It never throws. A half-typed buffer is the normal state of an editor: malformed input
// yields an `error` token from the bad offset onward, so the view still renders instead of
// blanking mid-keystroke.

/**
 * @typedef {'key'|'string'|'number'|'boolean'|'null'|'punct'|'error'} TokenType
 * @typedef {{start:number, end:number, type:TokenType}} Token
 */

/**
 * tokenize returns the non-overlapping, source-ordered spans of a JSON document.
 * Whitespace produces no token; the caller renders the gaps verbatim.
 * @param {string} src
 * @returns {Token[]}
 */
export function tokenize(src) {
  const text = String(src ?? '');
  /** @type {Token[]} */
  const tokens = [];
  // Each open container remembers whether the next string is a member name. Only an object
  // has keys, and only in the position right after '{' or ','.
  const stack = [];
  let i = 0;

  const top = () => stack[stack.length - 1];
  const push = (start, end, type) => tokens.push({ start, end, type });

  while (i < text.length) {
    const ch = text[i];

    if (ch === ' ' || ch === '\t' || ch === '\r' || ch === '\n') {
      i++;
      continue;
    }

    if (ch === '{' || ch === '[') {
      push(i, i + 1, 'punct');
      stack.push({ object: ch === '{', expectKey: ch === '{' });
      i++;
      continue;
    }
    if (ch === '}' || ch === ']') {
      push(i, i + 1, 'punct');
      stack.pop();
      i++;
      continue;
    }
    if (ch === ':') {
      push(i, i + 1, 'punct');
      if (top()) top().expectKey = false;
      i++;
      continue;
    }
    if (ch === ',') {
      push(i, i + 1, 'punct');
      if (top()) top().expectKey = top().object;
      i++;
      continue;
    }

    if (ch === '"') {
      const end = scanString(text, i);
      if (end === -1) {
        // Unterminated: everything from the opening quote is suspect, and calling it an
        // error is more honest than colouring the rest of the file as one long string.
        push(i, text.length, 'error');
        return tokens;
      }
      push(i, end, top()?.object && top().expectKey ? 'key' : 'string');
      i = end;
      continue;
    }

    if (ch === '-' || (ch >= '0' && ch <= '9')) {
      const end = scanNumber(text, i);
      push(i, end, 'number');
      i = end;
      continue;
    }

    if (text.startsWith('true', i) || text.startsWith('false', i)) {
      const end = i + (text[i] === 't' ? 4 : 5);
      push(i, end, 'boolean');
      i = end;
      continue;
    }
    if (text.startsWith('null', i)) {
      push(i, i + 4, 'null');
      i += 4;
      continue;
    }

    // Anything else is not JSON. Mark from here to the end and stop: guessing where the
    // author meant to resume would produce confidently wrong colours further down.
    push(i, text.length, 'error');
    return tokens;
  }

  return tokens;
}

/**
 * scanString returns the offset just past the closing quote of the string starting at
 * `start`, or -1 if it is never closed. Escapes are honoured so a value containing \" does
 * not end it early.
 * @param {string} text @param {number} start
 */
function scanString(text, start) {
  let i = start + 1;
  while (i < text.length) {
    const ch = text[i];
    if (ch === '\\') {
      i += 2;
      continue;
    }
    if (ch === '"') return i + 1;
    // A raw newline inside a string is invalid JSON, and treating it as unterminated stops
    // one missing quote from swallowing the whole rest of the file.
    if (ch === '\n') return -1;
    i++;
  }
  return -1;
}

/**
 * scanNumber returns the offset just past the numeric literal starting at `start`. It is
 * permissive — the validator judges whether the number is well formed; this only has to
 * decide how far to colour.
 * @param {string} text @param {number} start
 */
function scanNumber(text, start) {
  let i = start;
  if (text[i] === '-' || text[i] === '+') i++;
  while (i < text.length && /[0-9eE+\-.]/.test(text[i])) i++;
  return i;
}

/**
 * segments turns the token stream into a gap-free list covering every character, so the
 * highlight underlay can be rendered by walking one array. Whitespace between tokens becomes
 * an untyped segment; the underlay must reproduce it exactly or the text drifts out of
 * alignment with the textarea above it.
 * @param {string} src
 * @returns {{text:string, type:TokenType|''}[]}
 */
export function segments(src) {
  const text = String(src ?? '');
  const out = [];
  let cursor = 0;
  for (const token of tokenize(text)) {
    if (token.start > cursor) out.push({ text: text.slice(cursor, token.start), type: '' });
    out.push({ text: text.slice(token.start, token.end), type: token.type });
    cursor = token.end;
  }
  if (cursor < text.length) out.push({ text: text.slice(cursor), type: '' });
  return out;
}
