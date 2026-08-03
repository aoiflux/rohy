// Which controls the guided form should show, and what each one is worth showing as.
//
// The rule format is not uniform across algorithms: selecting `lineage` removes the sequence
// entirely, `field` makes match_fields mandatory, `temporal` adds a window. A form that showed
// every field the format has would invite an author to fill in controls that do nothing, and
// the result would be a rule that looks precise and matches on something else.
//
// This lives outside the component for the usual reason: it is a decision, and decisions in
// markup cannot be asserted. The schema descriptor is served by the backend, so nothing here
// hardcodes which field belongs to which algorithm — it reads `applies_to`.

/**
 * @typedef {object} Field
 * @property {string} name
 * @property {string} kind
 * @property {string[]} [applies_to]
 */

/**
 * algorithmOf resolves the algorithm a rule value selects, defaulting the way the loader does.
 * @param {any} value the parsed rule
 * @param {{algorithms?: {name:string}[]}} schema
 */
export function algorithmOf(value, schema) {
  const named = String(value?.algorithm ?? '').trim();
  if (named) return named;
  return schema?.algorithms?.[0]?.name ?? 'sequence';
}

/**
 * appliesTo reports whether a field is read by the given algorithm. A field with no
 * `applies_to` applies to all of them.
 */
export function appliesTo(field, algorithm) {
  const list = field?.applies_to;
  return !Array.isArray(list) || list.length === 0 || list.includes(algorithm);
}

/** isSet reports whether a rule actually carries a value for a field. */
export function isSet(value, field) {
  const v = value?.[field.name];
  if (v === undefined || v === null) return false;
  if (Array.isArray(v)) return v.length > 0;
  if (typeof v === 'number') return true;
  return String(v).trim() !== '';
}

/**
 * visibleFields returns the controls to render, each marked with whether the current algorithm
 * actually reads it.
 *
 * The rule, and the reason for each half:
 *
 *   - A field the algorithm reads is shown.
 *   - A field it does not read is HIDDEN — not disabled — because a disabled control still
 *     asks the author to reason about a setting that cannot matter.
 *   - UNLESS the file already sets it, in which case it stays visible and is marked inert. A
 *     control vanishing along with its value is the one thing worse than showing it: the value
 *     is still in the file, still preserved on save, and an author who cannot see it cannot
 *     remove it.
 *
 * @param {Field[]} fields
 * @param {string} algorithm
 * @param {any} value
 * @returns {Array<Field & {inert: boolean}>}
 */
export function visibleFields(fields, algorithm, value) {
  const out = [];
  for (const field of fields || []) {
    const reads = appliesTo(field, algorithm);
    if (reads) {
      out.push({ ...field, inert: false });
      continue;
    }
    if (isSet(value, field)) out.push({ ...field, inert: true });
  }
  return out;
}

/**
 * listValue coerces a field's value to the string array a list control edits, so a rule
 * carrying a malformed value still renders something the author can fix rather than crashing
 * the form.
 */
export function listValue(value, name) {
  const v = value?.[name];
  if (Array.isArray(v)) return v.map((x) => (typeof x === 'string' ? x : String(x ?? '')));
  return [];
}
