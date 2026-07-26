// The editor's document model.
//
// Both editor modes are projections of ONE state: the JSON text. The guided form does not
// own a parallel copy of the rule that has to be reconciled with the raw buffer — it reads a
// parse of the text and writes patches back through it. That is what makes switching modes
// mid-edit safe, and what lets a single undo history span both.
//
// The model's one real responsibility is not losing anything. RULES.md §3 says a field this
// build does not interpret is ignored, not rejected, and the rule inspector goes out of its
// way to show a file verbatim rather than re-serializing it. If the guided form rebuilt the
// document from a fixed set of known fields, opening a rule written for a newer rohy and
// touching one control would silently delete the fields that make it work. So a patch
// mutates the key it targets and nothing else — key order and unknown keys included.

import { stringify } from './format.js';

/**
 * @typedef {object} RuleDocument
 * @property {string} text the canonical state
 * @property {any} value the parse of it, or null when the text does not parse
 * @property {{message:string, line:number, col:number}|null} error
 */

/**
 * createDocument builds a document from rule text.
 * @param {string} [text]
 * @returns {RuleDocument}
 */
export function createDocument(text = '') {
  const { value, error } = parseText(text);
  return { text, value, error };
}

/**
 * parseText parses rule text and locates a syntax error in it.
 *
 * The position is best-effort: engines report it differently and some not at all, so this
 * recovers what it can and reports line 0 when it cannot. That is acceptable because the
 * position shown to the user comes from the backend validator, which computes it exactly;
 * this one only has to be good enough to keep the caret useful between debounced round trips.
 *
 * @param {string} text
 * @returns {{value:any, error:{message:string,line:number,col:number}|null}}
 */
export function parseText(text) {
  if (String(text).trim() === '') {
    return { value: null, error: { message: 'the rule is empty', line: 1, col: 1 } };
  }
  try {
    return { value: JSON.parse(text), error: null };
  } catch (err) {
    const message = String(err && err.message ? err.message : err);
    const { line, col } = errorPosition(text, message);
    return { value: null, error: { message, line, col } };
  }
}

/**
 * errorPosition recovers a line and column from a JSON.parse message. Engines emit either
 * "... at position 42" or "... (line 3 column 5)"; anything else yields 0/0 rather than a
 * guess, because an underline in the wrong place is worse than none.
 * @param {string} text
 * @param {string} message
 */
function errorPosition(text, message) {
  const lineCol = message.match(/line (\d+) column (\d+)/i);
  if (lineCol) return { line: Number(lineCol[1]), col: Number(lineCol[2]) };

  const position = message.match(/position (\d+)/i);
  if (position) return offsetToLineCol(text, Number(position[1]));

  return { line: 0, col: 0 };
}

/**
 * offsetToLineCol converts a character offset into a 1-based line and column, matching the
 * backend's offsetToLineCol.
 * @param {string} text
 * @param {number} offset
 */
export function offsetToLineCol(text, offset) {
  const clamped = Math.max(0, Math.min(offset, text.length));
  const before = text.slice(0, clamped);
  const newline = before.lastIndexOf('\n');
  return {
    line: before.split('\n').length,
    col: [...before.slice(newline + 1)].length + 1,
  };
}

/**
 * setText replaces the whole document, re-parsing it.
 * @param {RuleDocument} doc
 * @param {string} text
 * @returns {RuleDocument}
 */
export function setText(doc, text) {
  return createDocument(text);
}

/**
 * patch sets (or, with undefined, removes) one top-level key and re-serializes.
 *
 * Key order is preserved because JavaScript objects keep insertion order for string keys and
 * this assigns into the parsed object rather than rebuilding it — an existing key keeps its
 * position, and a new one lands at the end. Unknown keys are never touched.
 *
 * A document that does not parse cannot be patched: the guided form is not reachable in that
 * state, and guessing at a repair would be worse than refusing.
 *
 * @param {RuleDocument} doc
 * @param {string} key
 * @param {any} value undefined removes the key
 * @returns {RuleDocument} the original document unchanged if it does not parse
 */
export function patch(doc, key, value) {
  if (!doc || doc.value === null || typeof doc.value !== 'object' || Array.isArray(doc.value)) {
    return doc;
  }
  const next = { ...doc.value };
  if (value === undefined) delete next[key];
  else next[key] = value;
  return createDocument(stringify(next));
}

/**
 * patchAll applies several keys in one pass, so a form action that changes two fields
 * together (adding a sequence step and its label) produces one document and one undo entry
 * rather than two.
 * @param {RuleDocument} doc
 * @param {Record<string, any>} changes
 * @returns {RuleDocument}
 */
export function patchAll(doc, changes) {
  if (!doc || doc.value === null || typeof doc.value !== 'object' || Array.isArray(doc.value)) {
    return doc;
  }
  const next = { ...doc.value };
  for (const [key, value] of Object.entries(changes)) {
    if (value === undefined) delete next[key];
    else next[key] = value;
  }
  return createDocument(stringify(next));
}

/**
 * isFormEditable reports whether the guided form can safely open this document.
 *
 * Raw → guided is the one direction that can lose work, so it is gated: the text has to
 * parse, and it has to be a JSON object. Anything else and the switch is refused with the
 * parse error, rather than silently opening a form seeded from a partial read.
 *
 * @param {RuleDocument} doc
 * @returns {boolean}
 */
export function isFormEditable(doc) {
  return !!doc && doc.error === null && doc.value !== null && typeof doc.value === 'object' && !Array.isArray(doc.value);
}

/**
 * projectToForm splits a document into the fields the schema defines and the ones it does
 * not.
 *
 * The unknown half is returned rather than dropped so the guided editor can show it — a form
 * that silently omitted a field would look like it had deleted it.
 *
 * @param {RuleDocument} doc
 * @param {{fields:{name:string}[]}} schema
 * @returns {{known: Record<string, any>, unknown: Record<string, any>}}
 */
export function projectToForm(doc, schema) {
  const known = {};
  const unknown = {};
  if (!isFormEditable(doc)) return { known, unknown };

  const defined = new Set((schema?.fields || []).map((f) => f.name));
  for (const [key, value] of Object.entries(doc.value)) {
    if (defined.has(key)) known[key] = value;
    else unknown[key] = value;
  }
  return { known, unknown };
}

/**
 * fromSchemaDefaults seeds a brand-new rule from the schema: required fields get their
 * example, optional ones their default. The result is a document that already loads, so a
 * new rule opens with a working example rather than an empty buffer and four errors.
 * @param {{fields:{name:string,required:boolean,default?:any,example:any,read_only?:boolean}[]}} schema
 * @returns {RuleDocument}
 */
export function fromSchemaDefaults(schema) {
  const seed = {};
  for (const field of schema?.fields || []) {
    if (field.required) seed[field.name] = field.example;
    else if (field.default !== undefined && field.default !== null && field.default !== '') {
      seed[field.name] = field.default;
    }
  }
  return createDocument(stringify(seed));
}

/**
 * slug derives a rule's id from its name, mirroring the backend's slug(). The editor shows
 * it live under the name field, because the id is not cosmetic — it is the rule's identity,
 * and changing the name replaces the rule rather than editing it.
 * @param {string} name
 * @returns {string}
 */
export function slug(name) {
  return String(name || '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}
