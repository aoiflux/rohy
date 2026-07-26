import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { stringify, inline, minify, WIDTH } from './format.js';

// Shared with backend/rules/format_test.go. The guided editor re-serializes on every
// keystroke and cannot round-trip to Go for each one, so it carries this mirror of the
// backend formatter; one fixture is what stops the mirror from quietly writing rule files in
// a different shape from the rest of the library.
const cases = JSON.parse(
  readFileSync(fileURLToPath(new URL('../../../../backend/rules/testdata/format-cases.json', import.meta.url)), 'utf8'),
);

describe('stringify — shared fixture parity with the Go formatter', () => {
  for (const testCase of cases) {
    it(testCase.name, () => {
      expect(stringify(JSON.parse(testCase.input))).toBe(`${testCase.expected.join('\n')}\n`);
    });
  }
});

describe('stringify', () => {
  it('is idempotent through a parse', () => {
    const once = stringify({ name: 'X', sequence: ['1', '2'], labels: ['hop'] });
    expect(stringify(JSON.parse(once))).toBe(once);
  });

  it('keeps field order, so an unknown field stays where its author put it', () => {
    const text = stringify({ max_gap_seconds: 300, name: 'X', sequence: ['1', '2'] });
    expect(text.indexOf('max_gap_seconds')).toBeLessThan(text.indexOf('"name"'));
  });

  it('never emits a line wider than the budget for content it can break', () => {
    const text = stringify({ name: 'X', sequence: Array.from({ length: 40 }, () => '4624') });
    for (const line of text.split('\n')) {
      expect(line.length).toBeLessThanOrEqual(WIDTH);
    }
  });

  it('measures the key prefix, not just the value', () => {
    // One value, two keys. Inline it is 90 columns: at two spaces of indent it fits the
    // budget on its own, and behind a short key it still does. Behind a long one it does
    // not — so a formatter that measured only the value would emit an overlong line.
    const value = { host: 'x'.repeat(78) };
    expect(inline(value)).toHaveLength(90);

    const shortKey = stringify({ a: value });
    expect(shortKey.split('\n')[1]).toContain(inline(value)); // stayed inline

    const longKey = stringify({ a_rather_long_field_name: value });
    expect(longKey.split('\n')[1]).not.toContain(inline(value)); // expanded instead
    for (const line of longKey.split('\n')) {
      expect(line.length).toBeLessThanOrEqual(WIDTH);
    }
  });

  it('ends with a newline', () => {
    expect(stringify({ a: 1 }).endsWith('}\n')).toBe(true);
  });
});

describe('inline', () => {
  it('renders scalars, arrays and objects on one line', () => {
    expect(inline('x')).toBe('"x"');
    expect(inline(['a', 'b'])).toBe('["a", "b"]');
    expect(inline({ a: 1, b: [2] })).toBe('{"a": 1, "b": [2]}');
    expect(inline(null)).toBe('null');
  });
});

describe('minify', () => {
  it('strips every insignificant byte', () => {
    expect(minify({ name: 'X', sequence: ['1', '2'] })).toBe('{"name":"X","sequence":["1","2"]}');
  });
});
