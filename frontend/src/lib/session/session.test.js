import { describe, expect, it } from 'vitest';
import { SESSION_CHOICE, SESSION_NEED, UI } from '../consts/index.js';
import {
  byteLength,
  defaultChoice,
  evidenceLine,
  formProblem,
  groupFingerprint,
  needs,
  newPassphraseProblem,
  sessionRequest,
} from './session.js';

const signed = {
  need: SESSION_NEED.CHOICE,
  choices: [SESSION_CHOICE.UNLOCK, SESSION_CHOICE.READ_ONLY, SESSION_CHOICE.CONTINUE_UNSIGNED],
  evidence: ['register', 'image'],
};
const form = (over) => ({ name: '', choice: '', passphrase: '', reason: '', acceptLog: false, ...over });

describe('the session step', () => {
  it('starts with the only choice, and makes the analyst pick among several', () => {
    expect(defaultChoice({ choices: [SESSION_CHOICE.START] })).toBe(SESSION_CHOICE.START);
    expect(defaultChoice(signed)).toBe('');
  });

  it('asks a first run for a name', () => {
    const v = { need: SESSION_NEED.IDENTITY, choices: [SESSION_CHOICE.START] };
    expect(formProblem(v, form({ choice: SESSION_CHOICE.START }))).toBe(UI.SESSION_NEED_NAME);
    expect(formProblem(v, form({ choice: SESSION_CHOICE.START, name: '  Ada ' }))).toBe('');
    expect(sessionRequest(v, form({ choice: SESSION_CHOICE.START, name: '  Ada ' }))).toEqual({
      choice: SESSION_CHOICE.START,
      name: 'Ada',
    });
  });

  it('needs a passphrase to sign and a reason to stop signing', () => {
    expect(formProblem(signed, form({ choice: SESSION_CHOICE.UNLOCK }))).toBe(UI.SESSION_NEED_PASSPHRASE);
    expect(formProblem(signed, form({ choice: SESSION_CHOICE.CONTINUE_UNSIGNED, reason: '  ' }))).toBe(
      UI.SESSION_NEED_REASON,
    );
    expect(formProblem(signed, form({ choice: SESSION_CHOICE.READ_ONLY }))).toBe('');
  });

  // 🔒 The passphrase goes only where it is used.
  it('sends the passphrase only for a choice that signs', () => {
    const ro = sessionRequest(signed, form({ choice: SESSION_CHOICE.READ_ONLY, passphrase: 'left over from typing' }));
    expect(ro).toEqual({ choice: SESSION_CHOICE.READ_ONLY });
    const un = sessionRequest(signed, form({ choice: SESSION_CHOICE.UNLOCK, passphrase: 'a long passphrase' }));
    expect(un.passphrase).toBe('a long passphrase');
  });

  // 🔒 A flagged log is accepted, with a reason, before anything signs over it.
  it('makes a flagged log be accepted with a reason before signing', () => {
    const v = { ...signed, log_flagged: true };
    expect(needs(v, SESSION_CHOICE.UNLOCK).acceptLog).toBe(true);
    expect(needs(v, SESSION_CHOICE.READ_ONLY).acceptLog).toBe(false);
    const base = { choice: SESSION_CHOICE.UNLOCK, passphrase: 'a long passphrase' };
    expect(formProblem(v, form(base))).toBe(UI.SESSION_ACCEPT_LOG);
    expect(formProblem(v, form({ ...base, acceptLog: true }))).toBe(UI.SESSION_NEED_REASON);
    const req = sessionRequest(v, form({ ...base, acceptLog: true, reason: ' rohy crashed ' }));
    expect(req).toMatchObject({ accept_log: true, reason: 'rohy crashed' });
  });

  it('bounds the reason in bytes, as the backend does', () => {
    const long = 'é'.repeat(1025); // 2050 bytes
    expect(byteLength(long)).toBe(2050);
    expect(formProblem(signed, form({ choice: SESSION_CHOICE.CONTINUE_UNSIGNED, reason: long }))).not.toBe('');
  });

  it('says why a case is treated as signed', () => {
    expect(evidenceLine(signed)).toContain(UI.SESSION_EVIDENCE_LABEL.image);
    expect(evidenceLine({ evidence: [] })).toBe('');
  });
});

describe('a new key', () => {
  it('checks the passphrase twice and by length', () => {
    expect(newPassphraseProblem('short', 'short')).not.toBe('');
    expect(newPassphraseProblem('a long passphrase', 'a long passphrasE')).toBe(UI.SIGNING_PASS_MISMATCH);
    expect(newPassphraseProblem('a long passphrase', 'a long passphrase')).toBe('');
  });

  it('groups a fingerprint for reading', () => {
    expect(groupFingerprint('abcdef0123')).toBe('abcd ef01 23');
    expect(groupFingerprint('')).toBe('');
  });
});
