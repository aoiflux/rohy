// The findings editor's bounds, kept apart from the component so they are tested on their own.
// backend/findings/testdata/note-limit-cases.json holds them to the Go side's count.
import { FINDINGS } from './consts/index.js';

/** Code points in s: what Go's utf8.RuneCountInString counts once s has crossed the binding.
 *  A JS string's .length counts UTF-16 units, so a character outside the basic plane counts twice. */
export const runeLength = (s) => [...(s ?? '')].length;

/** Whether the backend would refuse this note as too long.
 *  It runs on every keystroke, so it never builds the array runeLength does: a string's UTF-16
 *  length is never less than its code-point count, so a short note is answered at once, and a
 *  long one is counted only as far as the limit. */
export function noteTooLong(note) {
  const s = note ?? '';
  if (s.length <= FINDINGS.NOTE_MAX) return false;
  let n = 0;
  for (const _ of s) {
    if (++n > FINDINGS.NOTE_MAX) return true;
  }
  return false;
}
