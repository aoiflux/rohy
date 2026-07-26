// Caret-aware completion for the raw editor.
//
// Everything it offers comes from the schema descriptor the backend serves — field names,
// allowed values, bounds — so the completion list cannot suggest something the loader would
// refuse. The one exception is event IDs, which come from the case itself: suggesting 4624
// to someone whose case has no Security channel wastes their time, so the ranking is by what
// is actually there.
//
// It navigates the token stream from highlight.js rather than re-scanning the text, so the
// editor has one understanding of what the caret is sitting in instead of two.

import { tokenize } from './highlight.js';

/**
 * @typedef {object} Completion
 * @property {string} value the text to insert
 * @property {string} label what the list shows
 * @property {string} [detail] the right-hand hint
 */

/**
 * contextAt reports what the caret is in.
 *
 * kind is 'key' when a field name is being typed, 'value' when a field's scalar value is,
 * 'element' when an array element is, and 'none' when nothing useful can be offered. field
 * names the top-level key the caret sits under, which is what decides the candidates.
 *
 * @param {string} text
 * @param {number} caret
 * @returns {{kind:'key'|'value'|'element'|'none', field:string|null, from:number, to:number, prefix:string}}
 */
export function contextAt(text, caret) {
  const src = String(text ?? '');
  const position = Math.max(0, Math.min(caret ?? 0, src.length));
  const tokens = tokenize(src);

  // Walk the stream tracking depth and, at depth 1, which key we are under. Only top-level
  // fields matter: the rule format has no addressable nesting.
  let depth = 0;
  let field = null;
  let inArray = false;

  for (const token of tokens) {
    const inside = position > token.start && position < token.end;
    const atEdge = position === token.start || position === token.end;

    if (inside || (atEdge && (token.type === 'key' || token.type === 'string'))) {
      if (token.type === 'key') {
        return { kind: 'key', field: null, from: token.start + 1, to: token.end - 1, prefix: inner(src, token) };
      }
      if (token.type === 'string') {
        return {
          kind: inArray ? 'element' : 'value',
          field,
          from: token.start + 1,
          to: token.end - 1,
          prefix: inner(src, token),
        };
      }
    }

    if (token.end > position) break;

    const raw = src.slice(token.start, token.end);
    if (token.type === 'punct') {
      if (raw === '{' || raw === '[') {
        depth++;
        if (raw === '[') inArray = true;
      } else if (raw === '}' || raw === ']') {
        depth--;
        if (raw === ']') inArray = false;
        if (depth <= 1) field = null;
      } else if (raw === ',' && !inArray) {
        field = null;
      }
    } else if (token.type === 'key' && depth === 1) {
      field = JSON.parse(src.slice(token.start, token.end));
    }
  }

  // Outside any token: a caret in the whitespace of the root object is where a new field
  // name goes.
  if (depth === 1 && !inArray && field === null) {
    return { kind: 'key', field: null, from: position, to: position, prefix: '' };
  }
  return { kind: 'none', field: null, from: position, to: position, prefix: '' };
}

const inner = (src, token) => src.slice(token.start + 1, token.end - 1);

/**
 * completionsAt returns what can be inserted at the caret, filtered by what is already typed.
 *
 * @param {string} text
 * @param {number} caret
 * @param {{fields:{name:string,kind:string,required:boolean,enum?:string[],description:string,default?:any}[]}} schema
 * @param {{id:string,count?:number,description?:string}[]} [eventIds] event IDs present in
 *   the case, most frequent first; the built-in library's own IDs are a reasonable stand-in
 * @returns {{from:number, to:number, items:Completion[]}}
 */
export function completionsAt(text, caret, schema, eventIds = []) {
  const src = String(text ?? '');
  const context = contextAt(src, caret);
  const empty = { from: context.from, to: context.to, items: [] };
  if (context.kind === 'none') return empty;

  const fields = schema?.fields || [];
  let items = [];

  if (context.kind === 'key') {
    // A field already written is not offered again — a duplicate key is legal JSON but the
    // decoder silently keeps only the last one, which is a mistake nobody notices.
    const present = new Set(topLevelKeys(src));
    present.delete(context.prefix);
    items = fields
      .filter((f) => !present.has(f.name))
      .map((f) => ({
        value: f.name,
        label: f.name,
        detail: f.required ? 'required' : f.description,
      }));
    // Required fields first: on a new rule they are the only ones that must be filled in.
    items.sort((a, b) => Number(b.detail === 'required') - Number(a.detail === 'required'));
  } else if (context.kind === 'element' && context.field === 'sequence') {
    items = eventIds.map((e) => ({
      value: e.id,
      label: e.id,
      detail: e.description || (e.count ? `${e.count} in this case` : ''),
    }));
  } else if (context.kind === 'value') {
    const field = fields.find((f) => f.name === context.field);
    items = (field?.enum || []).map((value) => ({
      value,
      label: value,
      detail: value === field.default ? 'default' : '',
    }));
  }

  const prefix = context.prefix.toLowerCase();
  return {
    from: context.from,
    to: context.to,
    items: prefix ? items.filter((i) => i.value.toLowerCase().startsWith(prefix)) : items,
  };
}

/**
 * topLevelKeys lists the field names already written, so completion does not offer one twice.
 * @param {string} text
 * @returns {string[]}
 */
export function topLevelKeys(text) {
  const src = String(text ?? '');
  const out = [];
  let depth = 0;
  for (const token of tokenize(src)) {
    const raw = src.slice(token.start, token.end);
    if (token.type === 'punct') {
      if (raw === '{' || raw === '[') depth++;
      else if (raw === '}' || raw === ']') depth--;
      continue;
    }
    if (token.type === 'key' && depth === 1) {
      try {
        out.push(JSON.parse(raw));
      } catch {
        /* a half-typed key is not a key yet */
      }
    }
  }
  return out;
}

/**
 * eventIdsFromRules derives a candidate list from the rule library itself, ranked by how
 * many rules use each id. It is the fallback for when the case has not been queried for its
 * own event IDs — a worse list than the real data, but a far better one than nothing.
 * @param {{sequence?:string[]}[]} rules
 * @returns {{id:string, count:number}[]}
 */
export function eventIdsFromRules(rules) {
  const counts = new Map();
  for (const rule of rules || []) {
    for (const id of new Set(rule.sequence || [])) {
      counts.set(id, (counts.get(id) || 0) + 1);
    }
  }
  return [...counts.entries()]
    .map(([id, count]) => ({ id, count }))
    .sort((a, b) => b.count - a.count || a.id.localeCompare(b.id));
}
