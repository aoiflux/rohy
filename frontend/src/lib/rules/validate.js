// A fast, local mirror of the backend rule validator.
//
// This is NOT the authority. backend/rules/validate.go decides whether a rule loads, and the
// editor asks it before every save. What this exists for is latency: the raw editor
// underlines a mistake on the keystroke that makes it, and the guided form disables Save the
// instant a required field is emptied, neither of which survives a round trip through the
// backend on every character.
//
// The two are kept in step by backend/rules/testdata/validation-cases.json, which drives
// both this module's tests and validate_test.go. A rule this mirror gets wrong is a failing
// test on both sides rather than an editor that quietly accepts something the loader will
// refuse.

import { RULE_PROBLEMS } from '../consts/index.js';
import { parseText, offsetToLineCol, slug } from './document.js';

/**
 * @typedef {object} Problem
 * @property {string} code
 * @property {string} [field]
 * @property {number} index 0-based element within field, or -1 for the field as a whole
 * @property {number} line 1-based, or 0 when unlocated
 * @property {number} col
 * @property {string} message
 */

/**
 * validate reports every way rule text violates the format, in the order the format states
 * its rules — the same order as the backend, so the first problem shown is the one the
 * loader would have reported.
 *
 * @param {string} source
 * @param {{format_version:number, max_file_bytes:number, fields:{name:string}[]}} schema
 * @returns {{valid:boolean, errors:Problem[], warnings:Problem[], unknownFields:string[], value:any}}
 */
export function validate(source, schema) {
  const text = String(source ?? '');
  const maxBytes = schema?.max_file_bytes;
  if (maxBytes && byteLength(text) > maxBytes) {
    return result([problem(RULE_PROBLEMS.FILE_TOO_LARGE, '', -1, 'the rule is too large')], [], [], null);
  }

  const { value, error } = parseText(text);
  if (error) {
    return result([{ ...problem(RULE_PROBLEMS.SYNTAX, '', -1, error.message), line: error.line, col: error.col }], [], [], null);
  }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return result([problem(RULE_PROBLEMS.SYNTAX, '', -1, 'a rule file must be a JSON object')], [], [], null);
  }

  const positions = scanPositions(text);
  const locate = (p) => {
    const key = p.index >= 0 ? `${p.field}[${p.index}]` : p.field;
    const offset = positions[key];
    if (offset === undefined) return p;
    const { line, col } = offsetToLineCol(text, offset);
    return { ...p, line, col };
  };

  const known = new Set((schema?.fields || []).map((f) => f.name));
  const unknownFields = Object.keys(value).filter((k) => !known.has(k));

  // A field of the wrong JSON type is a decode failure, not a contract violation: the
  // backend never gets as far as building a Spec, so it reports a syntax problem and stops.
  // Coercing instead — reading a string "4624" as an empty sequence — would have this mirror
  // report "needs at least 2 event IDs" for a file the loader refuses to parse at all.
  const mismatch = typeMismatch(value, schema);
  if (mismatch) return result([locate(mismatch)], [], unknownFields, null);

  return result(
    problems(value, schema).map(locate),
    advisories(value, unknownFields).map(locate),
    unknownFields,
    value,
  );
}

/**
 * typeMismatch returns the first known field whose value has the wrong JSON type, in file
 * order — matching where encoding/json would stop.
 *
 * null is not a mismatch: Go's decoder accepts it for any type and leaves the zero value,
 * so a rule with "labels": null loads with no labels rather than failing.
 *
 * @param {any} value
 * @param {{fields:{name:string,kind:string}[]}} schema
 * @returns {Problem|null}
 */
function typeMismatch(value, schema) {
  const byName = new Map((schema?.fields || []).map((f) => [f.name, f]));
  for (const [key, actual] of Object.entries(value)) {
    const field = byName.get(key);
    if (!field || !field.kind || actual === null) continue;
    if (!matchesKind(actual, field.kind)) {
      return problem(
        RULE_PROBLEMS.SYNTAX,
        key,
        -1,
        `not a valid rule file: "${key}" must be ${describeKind(field.kind)}`,
      );
    }
  }
  return null;
}

function matchesKind(actual, kind) {
  switch (kind) {
    case 'string':
      return typeof actual === 'string';
    case 'integer':
      return typeof actual === 'number' && Number.isInteger(actual);
    case 'string[]':
      return Array.isArray(actual) && actual.every((v) => typeof v === 'string');
    default:
      return true; // a kind this build does not know is not something to refuse on
  }
}

function describeKind(kind) {
  return { string: 'a string', integer: 'a whole number', 'string[]': 'a list of strings' }[kind] || kind;
}

/**
 * problems mirrors Spec.problems in the backend. The checks and their order are the format's
 * contract; changing one here without changing it there breaks the shared fixture.
 * @param {any} value
 * @param {{format_version:number}} schema
 * @returns {Problem[]}
 */
function problems(value, schema) {
  const out = [];
  const current = schema?.format_version ?? 1;
  const declared = typeof value.format_version === 'number' ? value.format_version : 0;
  const version = declared === 0 ? current : declared;

  if (version > current) {
    // Nothing else is reported for a file from the future: every further complaint would be
    // this build's rules applied to a format it has just said it cannot read.
    return [
      problem(
        RULE_PROBLEMS.UNSUPPORTED_FORMAT,
        'format_version',
        -1,
        `unsupported rule format version ${version} (this build supports up to ${current})`,
      ),
    ];
  }

  const sequence = Array.isArray(value.sequence) ? value.sequence : [];
  const labels = Array.isArray(value.labels) ? value.labels : [];
  const min = minItems(schema, 'sequence', 2);
  const max = maxItems(schema, 'sequence', 1000);

  if (String(value.name ?? '').trim() === '') {
    out.push(problem(RULE_PROBLEMS.NAME_REQUIRED, 'name', -1, 'rule name is required'));
  }
  if (sequence.length < min) {
    out.push(problem(RULE_PROBLEMS.SEQUENCE_SHORT, 'sequence', -1, `rule sequence needs at least ${min} event IDs`));
  }
  if (sequence.length > max) {
    out.push(problem(RULE_PROBLEMS.SEQUENCE_LONG, 'sequence', -1, `rule sequence exceeds the maximum of ${max} event IDs`));
  }
  // Every blank id is reported, not just the first: fixing them one round-trip at a time is
  // exactly the tedium an editor is supposed to remove.
  sequence.forEach((id, i) => {
    if (String(id ?? '').trim() === '') {
      out.push(
        problem(RULE_PROBLEMS.SEQUENCE_EMPTY_ID, 'sequence', i, `rule sequence contains an empty event ID at position ${i}`),
      );
    }
  });
  // Only meaningful once the sequence is long enough to have connections at all — otherwise
  // an absent sequence reports "more labels (0) than connections (-1)" on top of the real
  // problem.
  if (sequence.length >= min && labels.length > sequence.length - 1) {
    out.push(
      problem(
        RULE_PROBLEMS.LABELS_TOO_MANY,
        'labels',
        -1,
        `rule has more connection labels (${labels.length}) than connections (${sequence.length - 1})`,
      ),
    );
  }
  const algorithm = String(value.algorithm ?? '').trim();
  const allowed = enumOf(schema, 'algorithm');
  if (algorithm !== '' && allowed.length > 0 && !allowed.includes(algorithm)) {
    out.push(
      problem(RULE_PROBLEMS.UNKNOWN_ALGORITHM, 'algorithm', -1, `unknown correlation algorithm "${value.algorithm}"`),
    );
  }
  return out;
}

/** advisories mirrors Spec.advisories: legal, but worth saying. */
function advisories(value, unknownFields) {
  const out = [];
  if (String(value.description ?? '').trim() === '') {
    out.push(
      problem(
        RULE_PROBLEMS.NO_DESCRIPTION,
        'description',
        -1,
        'this rule has no description — the rules list and inspector will say nothing about what it matches',
      ),
    );
  }
  for (const key of unknownFields) {
    out.push(
      problem(RULE_PROBLEMS.UNKNOWN_FIELD, key, -1, `field "${key}" is not used by this build — it is preserved on save but has no effect`),
    );
  }
  return out;
}

/**
 * collisionWarning reports that another rule already claims this name. It is advice while
 * the author is still typing, and an error only when they try to save — Save asks the
 * backend, which refuses.
 * @param {string} name the name being typed
 * @param {string} editingId the rule being edited, so it does not collide with itself
 * @param {{id:string,name:string,source:string}[]} rules the current library
 * @returns {Problem|null}
 */
export function collisionWarning(name, editingId, rules) {
  const id = slug(name);
  if (!id) return null;
  const clash = (rules || []).find((r) => r.source === 'user' && r.id === id && r.id !== editingId);
  if (!clash) return null;
  return problem(
    RULE_PROBLEMS.NAME_COLLISION,
    'name',
    -1,
    `another rule is already named "${clash.name}" (id ${clash.id}); saving under this name is refused`,
  );
}

function problem(code, field, index, message) {
  return { code, field, index, line: 0, col: 0, message };
}

function result(errors, warnings, unknownFields, value) {
  return { valid: errors.length === 0, errors, warnings, unknownFields, value };
}

function fieldOf(schema, name) {
  return (schema?.fields || []).find((f) => f.name === name);
}
function enumOf(schema, name) {
  return fieldOf(schema, name)?.enum || [];
}
function minItems(schema, name, fallback) {
  return fieldOf(schema, name)?.min_items ?? fallback;
}
function maxItems(schema, name, fallback) {
  return fieldOf(schema, name)?.max_items ?? fallback;
}

/** byteLength measures the UTF-8 size the backend's cap is expressed in, not the string length. */
function byteLength(text) {
  if (typeof TextEncoder !== 'undefined') return new TextEncoder().encode(text).length;
  return unescape(encodeURIComponent(text)).length;
}

/**
 * scanPositions records where each addressable part of the source begins, keyed the same way
 * the backend keys them: "sequence" for a whole value, "sequence[2]" for an element. Only
 * top-level fields and their array elements are addressed, which is everything a problem can
 * point at.
 *
 * It is a small hand-rolled scan rather than a full parser because it runs only after
 * JSON.parse has already succeeded — it needs to find things, not to validate them.
 *
 * @param {string} text
 * @returns {Record<string, number>}
 */
export function scanPositions(text) {
  const positions = {};
  let i = 0;

  const skipWhitespace = () => {
    while (i < text.length && /\s/.test(text[i])) i++;
  };
  // readString consumes a JSON string starting at i (which must be a quote), honouring
  // escapes so a value containing \" does not end the scan early.
  const readString = () => {
    i++; // opening quote
    let out = '';
    while (i < text.length) {
      const ch = text[i];
      if (ch === '\\') {
        out += text[i] + text[i + 1];
        i += 2;
        continue;
      }
      i++;
      if (ch === '"') break;
      out += ch;
    }
    try {
      return JSON.parse(`"${out}"`);
    } catch {
      return out;
    }
  };
  // skipValue consumes any value, tracking container depth so a nested object cannot
  // misalign every position after it.
  const skipValue = () => {
    skipWhitespace();
    if (text[i] === '"') return readString();
    if (text[i] === '{' || text[i] === '[') {
      let depth = 0;
      while (i < text.length) {
        const ch = text[i];
        if (ch === '"') {
          readString();
          continue;
        }
        if (ch === '{' || ch === '[') depth++;
        if (ch === '}' || ch === ']') depth--;
        i++;
        if (depth === 0) return;
      }
      return;
    }
    while (i < text.length && !/[,}\]\s]/.test(text[i])) i++;
  };

  skipWhitespace();
  if (text[i] !== '{') return positions;
  i++;

  while (i < text.length) {
    skipWhitespace();
    if (text[i] === ',') {
      i++;
      continue;
    }
    if (text[i] === '}' || i >= text.length) break;
    if (text[i] !== '"') break;

    const keyStart = i;
    const key = readString();
    positions[`#${key}`] = keyStart;
    skipWhitespace();
    if (text[i] !== ':') break;
    i++;
    skipWhitespace();

    positions[key] = i;
    if (text[i] === '[') {
      i++; // into the array
      let index = 0;
      while (i < text.length) {
        skipWhitespace();
        if (text[i] === ',') {
          i++;
          continue;
        }
        if (text[i] === ']') {
          i++;
          break;
        }
        positions[`${key}[${index}]`] = i;
        skipValue();
        index++;
      }
      continue;
    }
    skipValue();
  }
  return positions;
}
