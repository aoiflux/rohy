import { describe, it, expect } from 'vitest';
import {
  createDocument,
  parseText,
  patch,
  patchAll,
  isFormEditable,
  projectToForm,
  fromSchemaDefaults,
  offsetToLineCol,
  slug,
} from './document.js';

const SCHEMA = {
  fields: [
    { name: 'name', kind: 'string', required: true, example: 'Failed Logons Then Successful Logon' },
    { name: 'sequence', kind: 'string[]', required: true, example: ['4625', '4624'] },
    { name: 'description', kind: 'string', default: '' },
    { name: 'relation_type', kind: 'string', default: 'correlation' },
    { name: 'algorithm', kind: 'string', default: 'sequence' },
    { name: 'format_version', kind: 'integer', default: 1, read_only: true },
  ],
};

describe('createDocument', () => {
  it('parses valid text', () => {
    const doc = createDocument('{"name":"X"}');
    expect(doc.error).toBeNull();
    expect(doc.value).toEqual({ name: 'X' });
  });

  it('keeps the text and reports the error when it does not parse', () => {
    const doc = createDocument('{"name":');
    expect(doc.value).toBeNull();
    expect(doc.error).not.toBeNull();
    // The buffer is never discarded — losing what someone typed because it is momentarily
    // unparseable would be the worst possible behaviour for an editor.
    expect(doc.text).toBe('{"name":');
  });

  it('treats an empty buffer as a problem rather than as null', () => {
    expect(createDocument('   ').error).not.toBeNull();
  });
});

describe('parseText', () => {
  it('locates a syntax error when the engine reports a position', () => {
    const { error } = parseText('{\n  "name": "x",\n  bad\n}');
    // Engines differ on the message; what must hold is that a position was recovered rather
    // than guessed at.
    expect(error).not.toBeNull();
    expect(error.line).toBeGreaterThanOrEqual(0);
  });
});

describe('patch', () => {
  it('sets a key and re-serializes', () => {
    const doc = patch(createDocument('{"name":"X","sequence":["1","2"]}'), 'name', 'Y');
    expect(doc.value.name).toBe('Y');
    expect(doc.text).toContain('"name": "Y"');
  });

  it('removes a key when the value is undefined', () => {
    const doc = patch(createDocument('{"name":"X","labels":["a"]}'), 'labels', undefined);
    expect(doc.value).toEqual({ name: 'X' });
  });

  it('appends a new key at the end rather than reordering', () => {
    const doc = patch(createDocument('{"sequence":["1","2"],"name":"X"}'), 'description', 'd');
    expect(Object.keys(doc.value)).toEqual(['sequence', 'name', 'description']);
  });

  // The promise that makes the guided editor safe on a rule from a newer rohy: RULES.md §3
  // says an unrecognized field is ignored rather than rejected, so editing one control must
  // not delete the fields that make somebody else's rule work.
  it('preserves fields this build does not interpret', () => {
    const doc = patch(
      createDocument('{"max_gap_seconds":300,"name":"X","scope":{"host":"any"}}'),
      'name',
      'Renamed',
    );
    expect(doc.value.max_gap_seconds).toBe(300);
    expect(doc.value.scope).toEqual({ host: 'any' });
    expect(Object.keys(doc.value)).toEqual(['max_gap_seconds', 'name', 'scope']);
  });

  it('refuses to patch a document that does not parse', () => {
    const broken = createDocument('{"name":');
    expect(patch(broken, 'name', 'X')).toBe(broken);
  });

  it('refuses to patch a document whose root is not an object', () => {
    const array = createDocument('["a"]');
    expect(patch(array, 'name', 'X')).toBe(array);
  });
});

describe('patchAll', () => {
  it('applies several changes as one document', () => {
    // A form action that changes two fields together — adding a step and its label — must
    // produce one document and therefore one undo entry, not two.
    const doc = patchAll(createDocument('{"sequence":["1","2"],"labels":["a"]}'), {
      sequence: ['1', '2', '3'],
      labels: ['a', 'b'],
    });
    expect(doc.value.sequence).toHaveLength(3);
    expect(doc.value.labels).toEqual(['a', 'b']);
  });
});

describe('isFormEditable', () => {
  it('is true only for a parsed JSON object', () => {
    expect(isFormEditable(createDocument('{"name":"X"}'))).toBe(true);
    expect(isFormEditable(createDocument('{"name":'))).toBe(false);
    expect(isFormEditable(createDocument('["a"]'))).toBe(false);
    expect(isFormEditable(createDocument('42'))).toBe(false);
    expect(isFormEditable(null)).toBe(false);
  });
});

describe('projectToForm', () => {
  it('splits known fields from ones the schema does not define', () => {
    const { known, unknown } = projectToForm(
      createDocument('{"name":"X","max_gap_seconds":300,"sequence":["1","2"]}'),
      SCHEMA,
    );
    expect(known).toEqual({ name: 'X', sequence: ['1', '2'] });
    // Returned rather than dropped: a form that silently omitted a field would look like it
    // had deleted it.
    expect(unknown).toEqual({ max_gap_seconds: 300 });
  });

  it('returns empty halves for a document the form cannot open', () => {
    expect(projectToForm(createDocument('{"a":'), SCHEMA)).toEqual({ known: {}, unknown: {} });
  });
});

describe('fromSchemaDefaults', () => {
  it('seeds a new rule that already loads', () => {
    const doc = fromSchemaDefaults(SCHEMA);
    expect(doc.error).toBeNull();
    expect(doc.value.name).toBe('Failed Logons Then Successful Logon');
    expect(doc.value.sequence).toEqual(['4625', '4624']);
    expect(doc.value.relation_type).toBe('correlation');
  });

  it('omits defaults that are empty, so a new rule is not littered with blank fields', () => {
    expect(fromSchemaDefaults(SCHEMA).value).not.toHaveProperty('description');
  });
});

describe('offsetToLineCol', () => {
  it('is 1-based and counts characters, not UTF-16 units', () => {
    const text = '{\n  "name": "café",\n}';
    expect(offsetToLineCol(text, 0)).toEqual({ line: 1, col: 1 });
    expect(offsetToLineCol(text, 2)).toEqual({ line: 2, col: 1 });
    expect(offsetToLineCol(text, text.indexOf(','), 10)).toEqual({ line: 2, col: 17 });
  });

  it('clamps out-of-range offsets', () => {
    expect(offsetToLineCol('{}', -5)).toEqual({ line: 1, col: 1 });
    expect(offsetToLineCol('{}', 9999)).toEqual({ line: 1, col: 3 });
  });
});

describe('slug', () => {
  it('mirrors the backend id derivation', () => {
    expect(slug('Failed Logons Then Successful Logon')).toBe('failed-logons-then-successful-logon');
    expect(slug('  Mixed   CASE!! ')).toBe('mixed-case');
    expect(slug('---')).toBe('');
    expect(slug('')).toBe('');
  });
});
