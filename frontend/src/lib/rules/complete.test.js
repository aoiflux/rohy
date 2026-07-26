import { describe, it, expect } from 'vitest';
import { contextAt, completionsAt, topLevelKeys, eventIdsFromRules } from './complete.js';

const SCHEMA = {
  fields: [
    { name: 'name', kind: 'string', required: true, description: 'The rule name.' },
    { name: 'sequence', kind: 'string[]', required: true, description: 'Ordered event IDs.' },
    { name: 'labels', kind: 'string[]', description: 'Per-connection labels.' },
    { name: 'description', kind: 'string', description: 'Free text.' },
    {
      name: 'relation_type',
      kind: 'string',
      enum: ['correlation', 'temporal', 'default'],
      default: 'correlation',
      description: 'Edge type.',
    },
    { name: 'algorithm', kind: 'string', enum: ['sequence'], default: 'sequence', description: 'Matcher.' },
  ],
};

const EVENT_IDS = [
  { id: '4624', count: 120 },
  { id: '4625', count: 40 },
  { id: '1102', count: 2 },
];

/** at returns the caret offset just after `marker` in `src` — how each case says where the
 *  cursor is without counting characters by hand. */
const at = (src, marker) => src.indexOf(marker) + marker.length;

describe('contextAt', () => {
  it('knows the caret is in a field name', () => {
    const src = '{"nam":"x"}';
    expect(contextAt(src, at(src, '"nam'))).toMatchObject({ kind: 'key', prefix: 'nam' });
  });

  it('knows the caret is in a field value, and which field', () => {
    const src = '{"relation_type":"corr"}';
    expect(contextAt(src, at(src, '"corr'))).toMatchObject({
      kind: 'value',
      field: 'relation_type',
      prefix: 'corr',
    });
  });

  it('knows the caret is in an array element, and which field owns the array', () => {
    const src = '{"sequence":["46"]}';
    expect(contextAt(src, at(src, '"46'))).toMatchObject({ kind: 'element', field: 'sequence', prefix: '46' });
  });

  it('offers a key position in the whitespace of the root object', () => {
    const src = '{\n  "name": "x",\n  \n}';
    expect(contextAt(src, at(src, '"x",\n  ')).kind).toBe('key');
  });

  it('reports the range that a completion should replace, inside the quotes', () => {
    const src = '{"seq":"x"}';
    const context = contextAt(src, at(src, '"seq'));
    expect(src.slice(context.from, context.to)).toBe('seq');
  });

  it('offers nothing outside the root object', () => {
    expect(contextAt('{"a":"b"} ', 10).kind).toBe('none');
  });
});

describe('completionsAt', () => {
  it('suggests field names, required ones first', () => {
    const src = '{"":""}';
    const { items } = completionsAt(src, 2, SCHEMA);
    expect(items[0].detail).toBe('required');
    expect(items.map((i) => i.value)).toContain('sequence');
  });

  // A duplicate key is legal JSON but the decoder silently keeps only the last one, which is
  // a mistake nobody notices.
  it('does not offer a field that is already written', () => {
    const src = '{"name":"x","":""}';
    const { items } = completionsAt(src, src.indexOf('"":""') + 1, SCHEMA);
    expect(items.map((i) => i.value)).not.toContain('name');
    expect(items.map((i) => i.value)).toContain('sequence');
  });

  it('still offers the field the caret is currently inside', () => {
    // Re-typing over an existing key must not filter that key out from under the caret.
    const src = '{"nam":"x"}';
    const { items } = completionsAt(src, at(src, '"nam'), SCHEMA);
    expect(items.map((i) => i.value)).toContain('name');
  });

  it('suggests an enum field only its allowed values, marking the default', () => {
    const src = '{"relation_type":""}';
    const { items } = completionsAt(src, src.indexOf('""') + 1, SCHEMA);
    expect(items.map((i) => i.value)).toEqual(['correlation', 'temporal', 'default']);
    expect(items[0].detail).toBe('default');
  });

  it('suggests event IDs inside a sequence, ranked by what the case actually holds', () => {
    const src = '{"sequence":[""]}';
    const { items } = completionsAt(src, src.indexOf('""') + 1, SCHEMA, EVENT_IDS);
    expect(items.map((i) => i.value)).toEqual(['4624', '4625', '1102']);
    expect(items[0].detail).toBe('120 in this case');
  });

  it('filters by what has been typed so far', () => {
    const src = '{"sequence":["46"]}';
    const { items } = completionsAt(src, at(src, '"46'), SCHEMA, EVENT_IDS);
    expect(items.map((i) => i.value)).toEqual(['4624', '4625']);
  });

  it('offers nothing for a field with no allowed values', () => {
    const src = '{"description":"any text"}';
    expect(completionsAt(src, at(src, '"any'), SCHEMA).items).toEqual([]);
  });
});

describe('topLevelKeys', () => {
  it('lists root fields only, ignoring keys nested inside them', () => {
    expect(topLevelKeys('{"a":1,"scope":{"host":"x"},"b":2}')).toEqual(['a', 'scope', 'b']);
  });

  it('survives a half-typed document', () => {
    expect(() => topLevelKeys('{"a":')).not.toThrow();
  });
});

describe('eventIdsFromRules', () => {
  it('ranks by how many rules use each id', () => {
    const ranked = eventIdsFromRules([
      { sequence: ['4624', '1102'] },
      { sequence: ['4624', '4625'] },
      { sequence: ['4624'] },
    ]);
    expect(ranked[0]).toEqual({ id: '4624', count: 3 });
    expect(ranked.map((r) => r.id)).toEqual(['4624', '1102', '4625']);
  });

  it('counts a rule once even when it repeats an id', () => {
    expect(eventIdsFromRules([{ sequence: ['4625', '4625', '4625', '4624'] }])).toEqual([
      { id: '4624', count: 1 },
      { id: '4625', count: 1 },
    ]);
  });
});
