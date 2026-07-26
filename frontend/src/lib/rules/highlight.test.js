import { describe, it, expect } from 'vitest';
import { tokenize, segments } from './highlight.js';

const typesOf = (src) => tokenize(src).map((t) => t.type);
const textOf = (src) => tokenize(src).map((t) => src.slice(t.start, t.end));

describe('tokenize', () => {
  // The whole reason this is a tokenizer and not a regex sweep: a key and a string value are
  // lexically identical, and colouring them the same is what makes hand-edited JSON hard to
  // read.
  it('distinguishes a key from a string value', () => {
    expect(typesOf('{"name":"value"}')).toEqual(['punct', 'key', 'punct', 'string', 'punct']);
  });

  it('treats array elements as values, never as keys', () => {
    expect(typesOf('{"a":["x","y"]}')).toEqual([
      'punct', 'key', 'punct', 'punct', 'string', 'punct', 'string', 'punct', 'punct',
    ]);
  });

  it('returns to key position after a comma inside an object', () => {
    expect(typesOf('{"a":"1","b":"2"}')).toEqual([
      'punct', 'key', 'punct', 'string', 'punct', 'key', 'punct', 'string', 'punct',
    ]);
  });

  it('does not treat a comma inside an array as starting a key', () => {
    expect(typesOf('{"a":["x","y"],"b":"z"}').filter((t) => t === 'key')).toEqual(['key', 'key']);
  });

  it('recognizes numbers, booleans and null', () => {
    expect(typesOf('{"a":1,"b":-2.5e3,"c":true,"d":false,"e":null}').filter((t) => t !== 'punct' && t !== 'key'))
      .toEqual(['number', 'number', 'boolean', 'boolean', 'null']);
  });

  it('handles escaped quotes inside a string', () => {
    expect(textOf('{"a":"he said \\"hi\\""}')[3]).toBe('"he said \\"hi\\""');
  });

  it('marks an unterminated string as an error instead of swallowing the file', () => {
    const tokens = tokenize('{"a":"unclosed\n  "b":"x"}');
    expect(tokens[tokens.length - 1].type).toBe('error');
  });

  it('marks unexpected input as an error and stops', () => {
    const tokens = tokenize('{"a": @@@ }');
    expect(tokens[tokens.length - 1].type).toBe('error');
  });

  // An editor's buffer is half-typed most of the time; the highlighter must never be the
  // reason the view goes blank.
  it('never throws, whatever it is given', () => {
    for (const input of ['', '{', '}', '[[[', '"', '\\', '{"a":', 'null', '@', '{"a":"b"']) {
      expect(() => tokenize(input)).not.toThrow();
    }
  });

  it('produces ordered, non-overlapping spans', () => {
    const src = '{\n  "name": "x",\n  "sequence": ["4624", "1102"]\n}';
    let previousEnd = 0;
    for (const token of tokenize(src)) {
      expect(token.start).toBeGreaterThanOrEqual(previousEnd);
      expect(token.end).toBeGreaterThan(token.start);
      previousEnd = token.end;
    }
  });
});

describe('segments', () => {
  // The underlay sits behind the textarea and must reproduce every character, whitespace
  // included — a dropped space shifts the highlighting out of alignment with the text the
  // user is actually looking at.
  it('covers the input exactly', () => {
    for (const src of ['{\n  "a": 1\n}', '  {"a":"b"}  ', '', '{"a":"unclosed']) {
      expect(segments(src).map((s) => s.text).join('')).toBe(src);
    }
  });

  it('leaves whitespace untyped', () => {
    const gaps = segments('{ "a": 1 }').filter((s) => s.type === '');
    expect(gaps.every((s) => s.text.trim() === '')).toBe(true);
  });
});
