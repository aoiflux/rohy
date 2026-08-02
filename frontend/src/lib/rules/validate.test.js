import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { validate, collisionWarning, scanPositions } from './validate.js';
import { RULE_PROBLEMS } from '../consts/index.js';

// The fixture lives in the BACKEND's testdata and is read by validate_test.go as well.
//
// That crossing is deliberate. The editor runs this validator so it can underline a mistake
// on the keystroke that makes it, while the Go one decides whether a saved rule ever comes
// back. Two validators allowed to drift will eventually disagree about a file the user has
// already saved — and the failure looks like a rule that saved cleanly and then vanished.
// One fixture makes the disagreement a red test instead.
const fixture = (name) =>
  JSON.parse(readFileSync(fileURLToPath(new URL(`../../../../backend/rules/testdata/${name}`, import.meta.url)), 'utf8'));

// The descriptor is read from the BACKEND's schema golden rather than hand-written here.
//
// It used to be a stand-in copy carrying "the same bounds", and that was fine while the format
// was uniform. It stopped being fine the moment fields became algorithm-dependent: a local
// copy would have to be updated by hand every time the format grew, and the version that is
// out of date is the version that makes this validator agree with a backend it no longer
// matches — which is precisely the drift the shared fixture exists to catch.
//
// The golden is generated from rules.Describe(), so this reads the real descriptor. A format
// change now breaks the Go golden test AND lands here in the same edit.
const SCHEMA = fixture('schema-golden.json');

describe('validate — shared fixture parity with the Go validator', () => {
  for (const testCase of fixture('validation-cases.json')) {
    it(testCase.name, () => {
      const source = testCase.source.join('\n');
      const report = validate(source, SCHEMA);

      expect(report.errors.map((e) => e.code)).toEqual(testCase.codes);
      expect(report.valid).toBe(testCase.codes.length === 0);

      for (const code of testCase.warnings || []) {
        expect(report.warnings.map((w) => w.code)).toContain(code);
      }
      expect(report.unknownFields).toEqual(testCase.unknown_fields || []);

      for (const want of testCase.locations || []) {
        const found = report.errors.find(
          (e) => e.code === want.code && e.field === want.field && e.index === want.index,
        );
        expect(found, `no error matching ${want.code} at ${want.field}[${want.index}]`).toBeTruthy();
        expect({ line: found.line, col: found.col }).toEqual({ line: want.line, col: want.col });
      }
    });
  }
});

describe('validate', () => {
  it('refuses input over the size cap without trying to parse it', () => {
    const report = validate(' '.repeat(2000), { ...SCHEMA, max_file_bytes: 1000 });
    expect(report.valid).toBe(false);
    expect(report.errors[0].code).toBe(RULE_PROBLEMS.FILE_TOO_LARGE);
  });

  it('rejects a top-level value that is not an object', () => {
    expect(validate('["4624","1102"]', SCHEMA).errors[0].code).toBe(RULE_PROBLEMS.SYNTAX);
    expect(validate('42', SCHEMA).errors[0].code).toBe(RULE_PROBLEMS.SYNTAX);
  });

  it('takes its bounds from the schema rather than hardcoding them', () => {
    const strict = { ...SCHEMA, fields: SCHEMA.fields.map((f) => (f.name === 'sequence' ? { ...f, min_items: 3 } : f)) };
    const source = '{"name":"X","description":"d","sequence":["4624","1102"]}';
    expect(validate(source, SCHEMA).valid).toBe(true);
    expect(validate(source, strict).errors[0].code).toBe(RULE_PROBLEMS.SEQUENCE_SHORT);
  });
});

describe('collisionWarning', () => {
  const library = [
    { id: 'taken', name: 'Taken', source: 'user' },
    { id: 'a-builtin', name: 'A Builtin', source: 'builtin' },
  ];

  it('warns when another user rule already claims the name', () => {
    expect(collisionWarning('Taken', '', library)?.code).toBe(RULE_PROBLEMS.NAME_COLLISION);
    // Slugging means punctuation and case do not save you from a collision.
    expect(collisionWarning('  taken!  ', '', library)?.code).toBe(RULE_PROBLEMS.NAME_COLLISION);
  });

  it('does not let a rule collide with itself', () => {
    expect(collisionWarning('Taken', 'taken', library)).toBeNull();
  });

  it('allows overriding a built-in, which is the documented way to vary one', () => {
    expect(collisionWarning('A Builtin', '', library)).toBeNull();
  });

  it('ignores a name that slugs to nothing', () => {
    expect(collisionWarning('!!!', '', library)).toBeNull();
  });
});

describe('scanPositions', () => {
  it('addresses top-level fields and array elements', () => {
    const text = '{\n  "name": "x",\n  "sequence": ["4624", "1102"]\n}';
    const positions = scanPositions(text);
    expect(text[positions.name]).toBe('"');
    expect(text.slice(positions['sequence[1]'], positions['sequence[1]'] + 6)).toBe('"1102"');
  });

  it('steps over a nested object without losing track of what follows', () => {
    const text = '{"scope":{"a":[1,2]},"labels":["hop"]}';
    const positions = scanPositions(text);
    expect(text.slice(positions['labels[0]'], positions['labels[0]'] + 5)).toBe('"hop"');
  });

  it('is not fooled by a brace or bracket inside a string', () => {
    const text = '{"description":"a } and a ] and a \\" quote","labels":["hop"]}';
    const positions = scanPositions(text);
    expect(text.slice(positions['labels[0]'], positions['labels[0]'] + 5)).toBe('"hop"');
  });
});
