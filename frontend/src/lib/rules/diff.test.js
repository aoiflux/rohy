import { describe, it, expect } from 'vitest';
import { diffLines, hasChanges, MAX_LINES } from './diff.js';

const ops = (before, after) => diffLines(before, after).rows.map((r) => `${r.op}:${r.text}`);

describe('diffLines', () => {
  it('reports no change for identical text', () => {
    const { rows, added, removed } = diffLines('a\nb\nc', 'a\nb\nc');
    expect(rows.every((r) => r.op === 'same')).toBe(true);
    expect({ added, removed }).toEqual({ added: 0, removed: 0 });
  });

  it('reports an insertion', () => {
    expect(ops('a\nc', 'a\nb\nc')).toEqual(['same:a', 'add:b', 'same:c']);
  });

  it('reports a deletion', () => {
    expect(ops('a\nb\nc', 'a\nc')).toEqual(['same:a', 'remove:b', 'same:c']);
  });

  it('reports a replacement as a removal followed by an addition', () => {
    expect(ops('a\nb', 'a\nB')).toEqual(['same:a', 'remove:b', 'add:B']);
  });

  it('handles one side being empty', () => {
    expect(diffLines('', 'a\nb').added).toBe(2);
    expect(diffLines('a\nb', '').removed).toBe(2);
  });

  // The gutters have to line up with the real files, so each row carries the line number it
  // occupies on the side it exists in — and null on the side it does not.
  it('numbers lines per side', () => {
    const { rows } = diffLines('a\nc', 'a\nb\nc');
    expect(rows.map((r) => [r.left, r.right])).toEqual([
      [1, 1],
      [null, 2],
      [2, 3],
    ]);
  });

  it('ignores a single trailing newline', () => {
    expect(diffLines('a\nb\n', 'a\nb').rows.every((r) => r.op === 'same')).toBe(true);
  });

  // A rule authored on Windows and one on Linux are the same rule; showing every line as
  // changed because of line endings would bury the real edit.
  it('ignores line-ending style', () => {
    expect(diffLines('a\r\nb', 'a\nb').rows.every((r) => r.op === 'same')).toBe(true);
  });

  it('refuses to build an O(n·m) table over a pathological input', () => {
    const huge = Array.from({ length: MAX_LINES + 1 }, (_, i) => `line ${i}`).join('\n');
    const result = diffLines(huge, `${huge}\nmore`);
    expect(result.truncated).toBe(true);
    expect(result.rows).toEqual([]);
  });

  it('produces a realistic rule-file diff', () => {
    const before = '{\n  "name": "X",\n  "sequence": ["4624", "1102"]\n}\n';
    const after = '{\n  "name": "X",\n  "description": "d",\n  "sequence": ["4624", "1102"]\n}\n';
    const { added, removed } = diffLines(before, after);
    expect({ added, removed }).toEqual({ added: 1, removed: 0 });
  });
});

describe('hasChanges', () => {
  it('answers without computing a diff', () => {
    expect(hasChanges('a\nb', 'a\nb')).toBe(false);
    expect(hasChanges('a\r\nb', 'a\nb')).toBe(false);
    expect(hasChanges('a\nb', 'a\nc')).toBe(true);
  });
});
