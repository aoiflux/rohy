import { describe, it, expect } from 'vitest';
import { bracketPairs, bracketMarks, insideString, keystroke, markSegments, matchAt } from './brackets.js';
import { segments } from './highlight.js';

// Cases are written with the caret as `|`, and a selection as two of them.
function parse(src) {
  const start = src.indexOf('|');
  const second = src.indexOf('|', start + 1);
  if (second === -1) return { text: src.slice(0, start) + src.slice(start + 1), start, end: start };
  return { text: src.slice(0, start) + src.slice(start + 1, second) + src.slice(second + 1), start, end: second - 1 };
}

function press(src, key) {
  const { text, start, end } = parse(src);
  const next = keystroke(text, start, end, key);
  return next === null ? null : next.text.slice(0, next.caret) + '|' + next.text.slice(next.caret);
}

describe('insideString', () => {
  it('knows the inside of a string from its edges', () => {
    const text = '{"ab": 1}';
    expect(insideString(text, 1)).toBe(false); // before the opening quote
    expect(insideString(text, 2)).toBe(true);
    expect(insideString(text, 4)).toBe(true); // before the closing quote
    expect(insideString(text, 5)).toBe(false); // after it
  });

  it('is not ended by an escaped quote', () => {
    expect(insideString('"a\\"b"', 5)).toBe(true);
  });

  it('ends an unclosed string at the end of its line', () => {
    const text = '{"ab\n}';
    expect(insideString(text, 4)).toBe(true);
    expect(insideString(text, 5)).toBe(false);
  });
});

describe('bracketPairs and matchAt', () => {
  it('pairs each bracket with its partner', () => {
    const pairs = bracketPairs('{"a": [1, 2]}');
    expect(pairs.get(0)).toBe(12);
    expect(pairs.get(12)).toBe(0);
    expect(pairs.get(6)).toBe(11);
  });

  it('ignores brackets inside strings, escaped quotes included', () => {
    const pairs = bracketPairs('{"a": "]}\\"}"}');
    expect([...pairs.keys()].sort((a, b) => a - b)).toEqual([0, 13]);
    expect(pairs.get(0)).toBe(13);
  });

  it('marks a bracket with no partner, and the wrong kind of closer', () => {
    const pairs = bracketPairs('{"a": [}');
    expect(pairs.get(0)).toBe(-1);
    expect(pairs.get(6)).toBe(-1);
    expect(pairs.get(7)).toBe(-1);
  });

  it('looks left of the caret first, then right', () => {
    const text = '{"a": []}';
    expect(matchAt(text, 0)).toEqual({ at: 0, match: 8 });
    expect(matchAt(text, 1)).toEqual({ at: 0, match: 8 });
    expect(matchAt(text, 7)).toEqual({ at: 6, match: 7 });
    expect(matchAt(text, 9)).toEqual({ at: 8, match: 0 });
    expect(matchAt(text, 3)).toBeNull();
  });

  it('matches nothing at a bracket inside a string', () => {
    expect(matchAt('"[a]"', 2)).toBeNull();
  });

  it('tolerates a caret from before the last edit', () => {
    expect(() => matchAt('{}', 99)).not.toThrow();
    expect(matchAt('{}', 99)).toEqual({ at: 1, match: 0 });
    expect(matchAt('', 5)).toBeNull();
  });

  it('marks both ends of a pair, or only the bracket with no partner', () => {
    expect([...bracketMarks({ at: 0, match: 4 })]).toEqual([
      [0, 'match'],
      [4, 'match'],
    ]);
    expect([...bracketMarks({ at: 3, match: -1 })]).toEqual([[3, 'unmatched']]);
    expect(bracketMarks(null).size).toBe(0);
  });
});

describe('markSegments', () => {
  it('marks single characters without changing the text', () => {
    const text = '{"a": [1]}';
    const parts = markSegments(segments(text), new Map([[0, 'match'], [9, 'match']]));
    expect(parts.map((p) => p.text).join('')).toBe(text);
    expect(parts.filter((p) => p.mark).map((p) => p.text)).toEqual(['{', '}']);
  });

  it('splits a longer segment around a mark and keeps its type', () => {
    // Everything after the stray x is one error segment; the closer inside it can still be marked.
    const text = '{ x }';
    const parts = markSegments(segments(text), new Map([[4, 'unmatched']]));
    expect(parts.map((p) => p.text).join('')).toBe(text);
    const marked = parts.find((p) => p.mark);
    expect(marked).toEqual({ text: '}', type: 'error', mark: 'unmatched' });
  });

  it('returns the segments untouched when nothing is marked', () => {
    const parts = segments('{"a": 1}');
    expect(markSegments(parts, new Map())).toBe(parts);
  });
});

describe('keystroke: auto-close', () => {
  it('closes a brace or a bracket typed before a space, a closer or the end', () => {
    expect(press('|', '{')).toBe('{|}');
    expect(press('{"sequence": |}', '[')).toBe('{"sequence": [|]}');
    expect(press('[|]', '{')).toBe('[{|}]');
    expect(press('[1|, 2]', '[')).toBe('[1[|], 2]');
  });

  it('does not close before other text', () => {
    expect(press('|"name"', '{')).toBeNull();
    expect(press('|1', '[')).toBeNull();
  });

  it('does not close inside a string', () => {
    expect(press('"a|b"', '{')).toBeNull();
    expect(press('"a|"', '[')).toBeNull();
  });

  it('leaves a selection to the browser', () => {
    expect(press('{|ab|}', '{')).toBeNull();
    expect(press('{|ab|}', '"')).toBeNull();
  });

  it('closes a quote opened outside a string', () => {
    expect(press('{|}', '"')).toBe('{"|"}');
    expect(press('{"a": |}', '"')).toBe('{"a": "|"}');
  });

  it('does not close a quote typed straight after a word', () => {
    expect(press('true|', '"')).toBeNull();
  });

  it('steps over the closing quote of the string it is in', () => {
    expect(press('{"abc|"}', '"')).toBe('{"abc"|}');
  });

  it('types a plain quote anywhere else inside a string', () => {
    expect(press('"a|bc"', '"')).toBeNull();
    expect(press('"a\\|"', '"')).toBeNull(); // completes an escape
    expect(press('"abc|', '"')).toBeNull(); // closes an unclosed string
  });
});

describe('keystroke: typing over a closer', () => {
  it('steps over a closer that has a partner', () => {
    expect(press('{|}', '}')).toBe('{}|');
    expect(press('[1|]', ']')).toBe('[1]|');
  });

  it('types a closer that has no partner, so a missing one can still be added', () => {
    expect(press('|}', '}')).toBeNull();
    expect(press('{"a": [1|}', ']')).toBeNull();
  });

  it('does not step over a different closer, or one inside a string', () => {
    expect(press('[|]', '}')).toBeNull();
    expect(press('"|}"', '}')).toBeNull();
  });
});

describe('keystroke: Backspace', () => {
  it('deletes an empty pair together', () => {
    expect(press('{|}', 'Backspace')).toBe('|');
    expect(press('[[|]]', 'Backspace')).toBe('[|]');
    expect(press('{"|"}', 'Backspace')).toBe('{|}');
  });

  it('deletes one character anywhere else', () => {
    expect(press('{a|}', 'Backspace')).toBeNull();
    expect(press('"a|"', 'Backspace')).toBeNull();
    expect(press('"{|}"', 'Backspace')).toBeNull(); // braces inside a string are text
    expect(press('"\\"|"', 'Backspace')).toBeNull(); // the quote before the caret is escaped
    expect(press('|{}', 'Backspace')).toBeNull();
    expect(press('{|x|}', 'Backspace')).toBeNull();
  });
});

describe('keystroke: Enter and Tab', () => {
  it('keeps the line indent', () => {
    expect(press('{\n  "a": 1,|\n}', 'Enter')).toBe('{\n  "a": 1,\n  |\n}');
  });

  it('indents one level after an opener', () => {
    expect(press('{|', 'Enter')).toBe('{\n  |');
  });

  it('opens an empty pair onto its own lines', () => {
    expect(press('{|}', 'Enter')).toBe('{\n  |\n}');
    expect(press('{\n  "a": [|]\n}', 'Enter')).toBe('{\n  "a": [\n    |\n  ]\n}');
  });

  it('takes no more indent than the caret sits in', () => {
    expect(press('  |  "a"', 'Enter')).toBe('  \n  |  "a"');
  });

  it('replaces a selection', () => {
    expect(press('  ab|cd|ef', 'Enter')).toBe('  ab\n  |ef');
  });

  it('leaves Enter inside a string to the browser', () => {
    expect(press('"a|b"', 'Enter')).toBeNull();
  });

  it('inserts two spaces for Tab, over any selection', () => {
    expect(press('{|}', 'Tab')).toBe('{  |}');
    expect(press('a|bc|d', 'Tab')).toBe('a  |d');
  });

  it('ignores every other key', () => {
    expect(press('{|}', 'a')).toBeNull();
    expect(press('{|}', 'ArrowLeft')).toBeNull();
    expect(press('{|}', 'Delete')).toBeNull();
  });
});

// A seeded generator, so a failure names its case and reruns the same way.
function rng(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 2 ** 32;
  };
}

const ALPHABET = ['{', '}', '[', ']', '"', '\\', 'a', ' ', '\n', ',', ':'];
const KEYS = ['{', '[', '"', '}', ']', 'Backspace', 'Enter', 'Tab'];

function balanced(text) {
  for (const partner of bracketPairs(text).values()) if (partner === -1) return false;
  return true;
}

describe('brackets on arbitrary text', () => {
  it('holds its invariants on 20 000 random buffers', () => {
    const next = rng(0xb2ac);
    for (let n = 0; n < 20000; n++) {
      const length = Math.floor(next() * 24);
      let text = '';
      for (let i = 0; i < length; i++) text += ALPHABET[Math.floor(next() * ALPHABET.length)];
      const a = Math.floor(next() * (text.length + 1));
      const b = next() < 0.8 ? a : Math.floor(next() * (text.length + 1));
      const [start, end] = a <= b ? [a, b] : [b, a];
      const key = KEYS[Math.floor(next() * KEYS.length)];
      const label = `${JSON.stringify(text)} ${start}-${end} ${key}`;

      // Pairs are symmetric and only ever join an opener to its own kind of closer.
      const pairs = bracketPairs(text);
      for (const [at, partner] of pairs) {
        expect('{}[]', label).toContain(text[at]);
        if (partner === -1) continue;
        expect(pairs.get(partner), label).toBe(at);
        const [open, close] = at < partner ? [at, partner] : [partner, at];
        expect({ '{': '}', '[': ']' }[text[open]], label).toBe(text[close]);
      }
      // matchAt's own early-stopping pass agrees with the full map at every offset.
      for (let c = 0; c <= text.length; c++) {
        const at = [c - 1, c].find((o) => pairs.has(o));
        const want = at === undefined ? null : { at, match: pairs.get(at) };
        const got = matchAt(text, c);
        if (got?.at !== want?.at || got?.match !== want?.match) expect(got, `${label} @${c}`).toEqual(want);
      }

      const result = keystroke(text, start, end, key);
      if (result === null) continue;
      expect(result.caret, label).toBeGreaterThanOrEqual(0);
      expect(result.caret, label).toBeLessThanOrEqual(result.text.length);
      // Everything before the edit and after it is untouched.
      const from = key === 'Backspace' ? start - 1 : start;
      const to = key === 'Backspace' ? end + 1 : end;
      expect(result.text.startsWith(text.slice(0, from)), label).toBe(true);
      expect(result.text.endsWith(text.slice(to)), label).toBe(true);
      // Auto-closing outside a string never unbalances a balanced buffer.
      if ((key === '{' || key === '[') && balanced(text)) expect(balanced(result.text), label).toBe(true);
    }
  });
});
