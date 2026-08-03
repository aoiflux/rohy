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

import {
  ALGORITHMS,
  LINEAGE_MAX_DEPTH,
  RULE_FIELDS,
  RULE_PROBLEMS,
  TEMPORAL_MAX_WINDOW_NS,
} from '../consts/index.js';
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
    advisories(value, unknownFields, schema).map(locate),
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
  const current = schema?.format_version ?? 1;
  const declared = typeof value.format_version === 'number' ? value.format_version : 0;
  const algorithm = algorithmOf(value);
  const descriptor = algorithmDescriptor(schema, algorithm);
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

  // The algorithm is resolved first and an unknown one stops the pass, because almost every
  // remaining check depends on which algorithm this is — whether a sequence is required,
  // whether match_fields must be present, whether a window is mandatory. Checking against a
  // guessed algorithm would put confident, wrong complaints beside the real one.
  if (!descriptor) {
    return [
      problem(
        RULE_PROBLEMS.UNKNOWN_ALGORITHM,
        RULE_FIELDS.ALGORITHM,
        -1,
        `unknown correlation algorithm "${value.algorithm}"`,
      ),
    ];
  }

  const out = [];

  if (String(value.name ?? '').trim() === '') {
    out.push(problem(RULE_PROBLEMS.NAME_REQUIRED, 'name', -1, 'rule name is required'));
  }

  out.push(...sequenceProblems(value, schema, descriptor));
  out.push(...scopeProblems(value, schema));
  out.push(...matchFieldProblems(value, schema, descriptor, algorithm));
  out.push(...windowProblems(value, descriptor));
  out.push(...lineageProblems(value, descriptor));
  out.push(...channelProblems(value));
  return out;
}

/** sequenceProblems checks the event-ID sequence, for the algorithms that have one. */
function sequenceProblems(value, schema, descriptor) {
  // Lineage reconstructs ancestry from creation records; it has no sequence to check, and a
  // sequence present anyway is an advisory rather than a rejection.
  if (descriptor.requires_sequence === false) return [];

  const out = [];
  const sequence = Array.isArray(value.sequence) ? value.sequence : [];
  const labels = Array.isArray(value.labels) ? value.labels : [];
  const min = minItems(schema, 'sequence', 2);
  const max = maxItems(schema, 'sequence', 1000);

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
  return out;
}

/**
 * scopeProblems refuses an unrecognized correlation scope.
 *
 * Unlike relation_type — which is cosmetic and silently corrected — a scope decides WHICH
 * events may match each other, so substituting a default would build a graph the author did
 * not ask for and has no way to notice.
 */
function scopeProblems(value, schema) {
  const scope = String(value[RULE_FIELDS.MATCH_SCOPE] ?? '').trim();
  if (scope === '') return [];
  const allowed = enumOf(schema, RULE_FIELDS.MATCH_SCOPE);
  if (allowed.length === 0 || allowed.includes(scope)) return [];
  return [
    problem(
      RULE_PROBLEMS.UNKNOWN_SCOPE,
      RULE_FIELDS.MATCH_SCOPE,
      -1,
      `unknown match_scope "${value[RULE_FIELDS.MATCH_SCOPE]}" (expected one of: ${allowed.join(', ')})`,
    ),
  ];
}

/** matchFieldProblems checks the correlation fields a match must share. */
function matchFieldProblems(value, schema, descriptor, algorithm) {
  const out = [];
  const fields = Array.isArray(value[RULE_FIELDS.MATCH_FIELDS]) ? value[RULE_FIELDS.MATCH_FIELDS] : [];

  // Field correlation with nothing to correlate on is sequence correlation wearing a
  // different name, so it is refused rather than quietly downgraded.
  if (algorithm === ALGORITHMS.FIELD && fields.length === 0) {
    out.push(
      problem(
        RULE_PROBLEMS.MATCH_FIELDS_REQUIRED,
        RULE_FIELDS.MATCH_FIELDS,
        -1,
        `the "${algorithm}" algorithm needs at least one entry in match_fields`,
      ),
    );
  }
  if (!reads(descriptor, RULE_FIELDS.MATCH_FIELDS)) return out;

  const known = enumOf(schema, RULE_FIELDS.MATCH_FIELDS);
  const seen = new Set();
  fields.forEach((raw, i) => {
    const name = String(raw ?? '').trim();
    if (known.length > 0 && !known.includes(name)) {
      out.push(
        problem(
          RULE_PROBLEMS.UNKNOWN_MATCH_FIELD,
          RULE_FIELDS.MATCH_FIELDS,
          i,
          `"${raw}" is not a correlation field (available: ${known.join(', ')})`,
        ),
      );
      return;
    }
    if (seen.has(name)) {
      out.push(
        problem(
          RULE_PROBLEMS.DUPLICATE_MATCH_FIELD,
          RULE_FIELDS.MATCH_FIELDS,
          i,
          `match_fields lists "${name}" more than once`,
        ),
      );
    }
    seen.add(name);
  });
  return out;
}

/** windowProblems checks the temporal bounds. */
function windowProblems(value, descriptor) {
  if (!reads(descriptor, RULE_FIELDS.WINDOW_WITHIN)) return [];

  const out = [];
  const rawWithin = String(value[RULE_FIELDS.WINDOW_WITHIN] ?? '').trim();
  const rawTotal = String(value[RULE_FIELDS.WINDOW_TOTAL] ?? '').trim();

  // An unbounded temporal rule is a slower spelling of a sequence rule, so the window is
  // required rather than defaulted to infinity.
  if (rawWithin === '') {
    out.push(
      problem(
        RULE_PROBLEMS.WINDOW_REQUIRED,
        RULE_FIELDS.WINDOW_WITHIN,
        -1,
        `the "${descriptor.name}" algorithm needs a window_within duration (for example "5m")`,
      ),
    );
  }
  const within = parseGoDuration(rawWithin);
  const total = parseGoDuration(rawTotal);
  out.push(...durationProblems(RULE_FIELDS.WINDOW_WITHIN, rawWithin, within));
  out.push(...durationProblems(RULE_FIELDS.WINDOW_TOTAL, rawTotal, total));

  // A total shorter than the per-step bound can never be satisfied, so the rule would
  // silently never fire.
  if (within !== null && total !== null && within > 0 && total > 0 && total < within) {
    out.push(
      problem(
        RULE_PROBLEMS.WINDOW_TOTAL_TOO_SMALL,
        RULE_FIELDS.WINDOW_TOTAL,
        -1,
        `window_total (${rawTotal}) is shorter than window_within (${rawWithin}), so no match could ever complete`,
      ),
    );
  }
  return out;
}

/** durationProblems reports one duration field's syntax and bounds. */
function durationProblems(field, raw, parsed) {
  if (raw === '') return []; // absent is not malformed; whether it is REQUIRED is the caller's call
  if (parsed === null) {
    return [
      problem(RULE_PROBLEMS.BAD_DURATION, field, -1, `${field} is not a duration (expected a value like "90s", "5m" or "2h")`),
    ];
  }
  if (parsed <= 0) {
    return [problem(RULE_PROBLEMS.BAD_DURATION, field, -1, `${field} must be greater than zero`)];
  }
  // A window measured in weeks is almost always a units slip, and it would make the rule pair
  // events that have nothing to do with each other.
  if (parsed > TEMPORAL_MAX_WINDOW_NS) {
    return [problem(RULE_PROBLEMS.WINDOW_TOO_LARGE, field, -1, `${field} of ${raw} exceeds the maximum — check the units`)];
  }
  return [];
}

/** lineageProblems checks the process-ancestry settings. */
function lineageProblems(value, descriptor) {
  if (!reads(descriptor, RULE_FIELDS.LINEAGE_CREATE_IDS)) return [];

  const out = [];
  const ids = Array.isArray(value[RULE_FIELDS.LINEAGE_CREATE_IDS]) ? value[RULE_FIELDS.LINEAGE_CREATE_IDS] : [];
  // An empty list is fine — it defaults to 4688 — but a blank entry is a half-finished edit.
  ids.forEach((id, i) => {
    if (String(id ?? '').trim() === '') {
      out.push(
        problem(
          RULE_PROBLEMS.LINEAGE_IDS_EMPTY,
          RULE_FIELDS.LINEAGE_CREATE_IDS,
          i,
          `lineage_create_ids contains an empty event ID at position ${i}`,
        ),
      );
    }
  });
  const depth = value[RULE_FIELDS.LINEAGE_DEPTH];
  if (typeof depth === 'number' && (depth < 0 || depth > LINEAGE_MAX_DEPTH)) {
    out.push(
      problem(
        RULE_PROBLEMS.LINEAGE_DEPTH_RANGE,
        RULE_FIELDS.LINEAGE_DEPTH,
        -1,
        `lineage_depth must be between 0 and ${LINEAGE_MAX_DEPTH}`,
      ),
    );
  }
  return out;
}

/** channelProblems checks the declared channel list, which every algorithm carries. */
function channelProblems(value) {
  const channels = Array.isArray(value[RULE_FIELDS.CHANNELS]) ? value[RULE_FIELDS.CHANNELS] : [];
  const out = [];
  channels.forEach((ch, i) => {
    if (String(ch ?? '').trim() === '') {
      out.push(
        problem(RULE_PROBLEMS.CHANNEL_EMPTY, RULE_FIELDS.CHANNELS, i, `channels contains an empty entry at position ${i}`),
      );
    }
  });
  return out;
}

/** advisories mirrors Spec.advisories: legal, but worth saying. */
function advisories(value, unknownFields, schema) {
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
  // A rule that does not say which logs it needs cannot be checked against a case, and silence
  // from that check would read as "fine".
  const channels = Array.isArray(value[RULE_FIELDS.CHANNELS]) ? value[RULE_FIELDS.CHANNELS] : [];
  if (channels.length === 0) {
    out.push(
      problem(
        RULE_PROBLEMS.NO_CHANNELS,
        RULE_FIELDS.CHANNELS,
        -1,
        'this rule does not declare the channels it needs, so rohy cannot tell you when a case is missing the log it depends on',
      ),
    );
  }
  out.push(...inertFieldAdvisories(value, schema));
  for (const key of unknownFields) {
    out.push(
      problem(RULE_PROBLEMS.UNKNOWN_FIELD, key, -1, `field "${key}" is not used by this build — it is preserved on save but has no effect`),
    );
  }
  return out;
}

/**
 * inertFieldAdvisories reports fields the selected algorithm does not read.
 *
 * Warnings, never errors — the same rule the format applies to a field it does not recognize
 * at all: preserved on save, ignored on load. Rejecting a window_within on a sequence rule
 * would make the editor stricter than the loader, which is the one direction it must never be.
 *
 * Saying nothing would be worse than either, though. An author who sets match_fields and
 * leaves the algorithm at its default gets a rule that looks precise and correlates on
 * ordering alone, and nothing in the result would tell them.
 */
function inertFieldAdvisories(value, schema) {
  const algorithm = algorithmOf(value);
  const descriptor = algorithmDescriptor(schema, algorithm);
  if (!descriptor) return []; // an unknown algorithm is already a hard error; do not pile on

  const out = [];
  const warn = (field) =>
    out.push(
      problem(
        RULE_PROBLEMS.FIELD_NOT_FOR_ALGORITHM,
        field,
        -1,
        `field "${field}" has no effect for the "${algorithm}" algorithm — it is preserved on save but is not read`,
      ),
    );

  const set = (field) => {
    const v = value[field];
    if (Array.isArray(v)) return v.length > 0;
    if (typeof v === 'number') return v !== 0;
    return String(v ?? '').trim() !== '';
  };

  for (const field of [
    RULE_FIELDS.MATCH_FIELDS,
    RULE_FIELDS.WINDOW_WITHIN,
    RULE_FIELDS.WINDOW_TOTAL,
    RULE_FIELDS.LINEAGE_CREATE_IDS,
    RULE_FIELDS.LINEAGE_DEPTH,
  ]) {
    if (set(field) && !reads(descriptor, field)) warn(field);
  }

  // A sequence on an algorithm that does not match one gets its own code: it is the likeliest
  // of these to be a genuine misunderstanding of what the algorithm does.
  const sequence = Array.isArray(value.sequence) ? value.sequence : [];
  if (sequence.length > 0 && descriptor.requires_sequence === false) {
    out.push(
      problem(
        RULE_PROBLEMS.SEQUENCE_IGNORED,
        'sequence',
        -1,
        `the "${algorithm}" algorithm does not match an event ID sequence, so this rule's sequence is preserved but not read`,
      ),
    );
  }
  return out;
}

/** algorithmOf resolves the algorithm a rule selects, defaulting to sequence correlation. */
function algorithmOf(value) {
  const a = String(value.algorithm ?? '').trim();
  return a === '' ? ALGORITHMS.SEQUENCE : a;
}

/**
 * algorithmDescriptor looks an algorithm up in the SERVED schema, so the frontend never keeps
 * its own copy of which fields a matcher reads or what version it needs.
 */
function algorithmDescriptor(schema, name) {
  return (schema?.algorithms || []).find((a) => a.name === name) || null;
}

/** reads reports whether an algorithm reads a given rule field. */
function reads(descriptor, field) {
  return (descriptor?.fields || []).includes(field);
}

/**
 * parseGoDuration parses the subset of Go's duration syntax the rule format uses, returning
 * nanoseconds or null when the text is not a duration.
 *
 * It mirrors time.ParseDuration rather than inventing a format, because the backend is what
 * ultimately reads these values — accepting something here that the loader refuses is exactly
 * the drift the shared fixture exists to catch.
 */
export function parseGoDuration(text) {
  const s = String(text ?? '').trim();
  if (s === '') return null;

  const units = { ns: 1, us: 1e3, µs: 1e3, μs: 1e3, ms: 1e6, s: 1e9, m: 6e10, h: 3.6e12 };
  const re = /(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/gy;

  let i = 0;
  let sign = 1;
  if (s[i] === '+' || s[i] === '-') {
    sign = s[i] === '-' ? -1 : 1;
    i++;
  }
  if (s.slice(i) === '0') return 0;

  re.lastIndex = i;
  let total = 0;
  let matched = false;
  let m;
  while ((m = re.exec(s)) !== null) {
    total += parseFloat(m[1]) * units[m[2]];
    matched = true;
    if (re.lastIndex >= s.length) break;
  }
  if (!matched || re.lastIndex !== s.length) return null;
  return sign * total;
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
