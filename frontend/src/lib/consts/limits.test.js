import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
  ANALYST_MAX_NAME,
  FINDINGS,
  KEY_MAX_PASSPHRASE_BYTES,
  KEY_MIN_PASSPHRASE_BYTES,
  MAX_SIGNING_REASON_BYTES,
} from './index.js';

// The frontend checks these bounds before a call so it can say what is wrong before the backend
// refuses. A mirror that drifts from consts.go warns at the wrong length, or not at all.
describe('mirrored bounds', () => {
  const src = readFileSync(fileURLToPath(new URL('../../../../backend/consts/consts.go', import.meta.url)), 'utf8');
  const goValue = (name) => {
    const m = src.match(new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)\\s*$`, 'm'));
    return m ? Number(m[1]) : undefined;
  };

  for (const [name, value] of [
    ['MaxFindingNoteLen', FINDINGS.NOTE_MAX],
    ['MaxFindingTagLen', FINDINGS.TAG_MAX],
    ['MaxFindingTags', FINDINGS.TAGS_MAX],
    ['AnalystMaxName', ANALYST_MAX_NAME],
    ['MaxSigningReasonLen', MAX_SIGNING_REASON_BYTES],
    ['KeyMinPassphrase', KEY_MIN_PASSPHRASE_BYTES],
    ['KeyMaxPassphrase', KEY_MAX_PASSPHRASE_BYTES],
  ]) {
    it(`every mirrored bound matches backend consts.go: ${name}`, () => {
      expect(goValue(name)).toBe(value);
    });
  }
});
