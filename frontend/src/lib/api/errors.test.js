import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { Call } from '@wailsio/runtime';
import { decodeError, errorCode, errorMessage } from './errors.js';
import { stats, timeline } from './index.js';
import { ERROR_CODES } from '../consts/index.js';

// The runtime's call, replaced so call() can be driven without a backend. The spy records the
// call; `answer` gives the result. A spy that returned the rejection itself would track it, and
// vitest would report it as unhandled.
const mocked = vi.hoisted(() => ({ byName: vi.fn(), answer: () => Promise.resolve() }));
const byName = mocked.byName;
vi.mock('@wailsio/runtime', async (orig) => {
  const real = await orig();
  return {
    ...real,
    Call: {
      ...real.Call,
      ByName: (...a) => {
        mocked.byName(...a);
        return mocked.answer();
      },
    },
  };
});

// A failed call as the Wails runtime delivers it: a RuntimeError carrying the error's own text,
// whose cause is what the backend's MarshalError sent (runtime.ts: `err.cause = json.cause`).
function rejection(code, message) {
  const e = new Call.RuntimeError(`wrapped: ${message}`);
  e.cause = { code, message };
  return e;
}

describe('decodeError', () => {
  it('turns the backend cause into an Error with the message and the code', () => {
    const e = decodeError(rejection('case_read_only', 'the signing key is locked'));
    expect(e).toBeInstanceOf(Error);
    expect(e.message).toBe('the signing key is locked');
    expect(e.code).toBe('case_read_only');
  });

  it('keeps quotes, newlines and braces in the message', () => {
    const text = 'field "user"\nis used twice {x}';
    expect(decodeError(rejection('query_error', text)).message).toBe(text);
  });

  it('leaves everything else exactly as it was', () => {
    const typeErr = new TypeError('api.EventsAPI.Timeline expects 3 arguments, got 2'); // Wails' own, no cause
    const other = Object.assign(new Error('x'), { cause: { status: 500 } });
    const numeric = Object.assign(new Error('x'), { cause: { code: 7, message: 'x' } });
    const empty = Object.assign(new Error('x'), { cause: { code: '', message: 'x' } });
    const textCause = Object.assign(new Error('x'), { cause: 'a string' });
    for (const e of [typeErr, other, numeric, empty, textCause]) {
      expect(decodeError(e)).toBe(e);
    }
    expect(decodeError(undefined)).toBeUndefined();
    expect(decodeError('a string')).toBe('a string');
  });
});

describe('errorCode / errorMessage', () => {
  it('read a decoded error', () => {
    const e = decodeError(rejection('case_busy', 'an ingest is running'));
    expect(errorCode(e)).toBe('case_busy');
    expect(errorMessage(e)).toBe('an ingest is running');
  });

  it('call an uncoded failure internal, so it never reads as the case refusing', () => {
    expect(errorCode(new TypeError('bad arguments'))).toBe(ERROR_CODES.INTERNAL);
    expect(errorCode(undefined)).toBe(ERROR_CODES.INTERNAL);
    expect(errorCode('text')).toBe(ERROR_CODES.INTERNAL);
  });

  it('read a message from anything', () => {
    expect(errorMessage(new Error('x'))).toBe('x');
    expect(errorMessage('plain')).toBe('plain');
    expect(errorMessage(undefined)).toBe('');
  });
});

describe('call()', () => {
  beforeEach(() => byName.mockReset());

  it('calls the bound method by its full name, every argument forwarded', async () => {
    mocked.answer = () => Promise.resolve({});
    await timeline({ provider: 'p' }, 40, '');
    expect(byName).toHaveBeenCalledWith('rohy/backend/api.EventsAPI.Timeline', { provider: 'p' }, 40, '');
  });

  it('decodes a rejected call, so every binding carries its code', async () => {
    mocked.answer = () => Promise.reject(rejection('persistence_error', 'disk gone'));
    const err = await stats().catch((e) => e);
    expect(err.message).toBe('disk gone');
    expect(err.code).toBe('persistence_error');
  });

  it('passes a resolved call through untouched', async () => {
    const value = { nodes: 3, edges: 1 };
    mocked.answer = () => Promise.resolve(value);
    expect(await stats()).toBe(value);
  });
});

// Every code the backend can send must be one the frontend knows, or the page has no name for it.
describe('error code parity', () => {
  it('every backend ErrCode is in ERROR_CODES', () => {
    const src = readFileSync(
      fileURLToPath(new URL('../../../../backend/consts/consts.go', import.meta.url)),
      'utf8',
    );
    const backend = [...src.matchAll(/\bErrCode\w+\s*=\s*"([^"]+)"/g)].map((m) => m[1]);
    expect(backend.length).toBeGreaterThan(5);
    const known = new Set(Object.values(ERROR_CODES));
    expect(backend.filter((c) => !known.has(c))).toEqual([]);
  });
});
