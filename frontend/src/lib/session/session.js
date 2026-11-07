// The session step's rules (item 41), apart from the component so they are tested on their own.
//
// 🔒 The passphrase is only ever placed in the one request that needs it, and nothing here keeps it.
import {
  ANALYST_MAX_NAME,
  KEY_MAX_PASSPHRASE_BYTES,
  KEY_MIN_PASSPHRASE_BYTES,
  MAX_SIGNING_REASON_BYTES,
  SESSION_CHOICE,
  SESSION_NEED,
  UI,
} from '../consts/index.js';

const encoder = new TextEncoder();

/** UTF-8 length, the unit the Go side's passphrase and reason bounds count in. Names count runes. */
export const byteLength = (s) => encoder.encode(s || '').length;

const fill = (template, value) => template.replace(/%[ds]/, String(value));

/** The choice to start with: the only one offered, or none — the analyst picks. */
export function defaultChoice(view) {
  const choices = (view && view.choices) || [];
  return choices.length === 1 ? choices[0] : '';
}

/** What a choice needs from the form. */
export function needs(view, choice) {
  const signs = choice === SESSION_CHOICE.UNLOCK || choice === SESSION_CHOICE.ADD_KEY;
  return {
    name: !!view && view.need === SESSION_NEED.IDENTITY,
    passphrase: signs,
    reason: choice === SESSION_CHOICE.CONTINUE_UNSIGNED,
    // A flagged log must be accepted, with a reason, before a session signs over it.
    acceptLog: signs && !!view && !!view.log_flagged,
  };
}

/** The first thing wrong with the form, or ''. */
export function formProblem(view, form) {
  const n = needs(view, form.choice);
  const name = (form.name || '').trim();
  if (n.name && !name) return UI.SESSION_NEED_NAME;
  if ([...name].length > ANALYST_MAX_NAME) return fill(UI.SESSION_NAME_TOO_LONG, ANALYST_MAX_NAME);
  if (!form.choice) return UI.SESSION_NEED_CHOICE;
  if (n.passphrase && !form.passphrase) return UI.SESSION_NEED_PASSPHRASE;
  if (n.acceptLog && !form.acceptLog) return UI.SESSION_ACCEPT_LOG;
  const reason = (form.reason || '').trim();
  if ((n.reason || (n.acceptLog && form.acceptLog)) && !reason) return UI.SESSION_NEED_REASON;
  if (byteLength(reason) > MAX_SIGNING_REASON_BYTES) return fill(UI.SESSION_REASON_TOO_LONG, MAX_SIGNING_REASON_BYTES);
  return '';
}

/** The request to send: only the fields the choice uses. */
export function sessionRequest(view, form) {
  const n = needs(view, form.choice);
  const req = { choice: form.choice };
  const name = (form.name || '').trim();
  if (name) req.name = name;
  if (n.passphrase) req.passphrase = form.passphrase;
  if (n.reason || (n.acceptLog && form.acceptLog)) req.reason = (form.reason || '').trim();
  if (n.acceptLog && form.acceptLog) req.accept_log = true;
  if (form.keepProfileAside) req.keep_profile_aside = true;
  return req;
}

/** Why the case is treated as signed, in words, or ''. */
export function evidenceLine(view) {
  const ev = ((view && view.evidence) || []).map((e) => UI.SESSION_EVIDENCE_LABEL[e] || e);
  if (!ev.length) return '';
  return fill(UI.SESSION_EVIDENCE, ev.join(', '));
}

/** A new key's passphrase: long enough, not too long, typed the same twice. */
export function newPassphraseProblem(pass, repeat) {
  const n = byteLength(pass);
  if (n < KEY_MIN_PASSPHRASE_BYTES) return fill(UI.SIGNING_PASS_SHORT, KEY_MIN_PASSPHRASE_BYTES);
  if (n > KEY_MAX_PASSPHRASE_BYTES) return fill(UI.SIGNING_PASS_LONG, KEY_MAX_PASSPHRASE_BYTES);
  if (pass !== repeat) return UI.SIGNING_PASS_MISMATCH;
  return '';
}

/** A fingerprint grouped in fours, for reading aloud and comparing by eye. */
export function groupFingerprint(fp) {
  return (fp || '').match(/.{1,4}/g)?.join(' ') ?? '';
}
