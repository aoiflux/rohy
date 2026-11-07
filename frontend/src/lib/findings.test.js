import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { FINDINGS } from './consts/index.js';
import { noteTooLong, runeLength } from './findings.js';

// Shared with backend/findings/findings_test.go. The editor warns before a save, so it has to
// count what the backend refuses: one fixture holds both sides to the same length.
const cases = JSON.parse(
  readFileSync(fileURLToPath(new URL('../../../backend/findings/testdata/note-limit-cases.json', import.meta.url)), 'utf8'),
);

describe('noteTooLong', () => {
  it('has shared cases to check', () => {
    expect(cases.length).toBeGreaterThan(0);
  });

  for (const testCase of cases) {
    it(`agrees with the backend: ${testCase.name}`, () => {
      expect(noteTooLong(testCase.unit.repeat(FINDINGS.NOTE_MAX + testCase.over))).toBe(!testCase.fits);
    });
  }

  it('treats a missing note as empty', () => {
    expect(noteTooLong(undefined)).toBe(false);
    expect(noteTooLong('')).toBe(false);
  });
});

describe('runeLength', () => {
  it('counts what Go counts once the string has crossed the binding', () => {
    expect(runeLength('日本')).toBe(2);
    expect(runeLength(String.fromCodePoint(0x1f600))).toBe(1);
    // A lone surrogate crosses the binding as one U+FFFD, which Go counts as one rune.
    expect(runeLength(String.fromCharCode(0xd800))).toBe(1);
    expect(runeLength('')).toBe(0);
    expect(runeLength(undefined)).toBe(0);
  });
});
