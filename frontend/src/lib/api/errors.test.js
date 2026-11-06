import { describe, it, expect, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { decodeError, errorCode, errorMessage } from './errors.js';
import { stats } from './index.js';
import { ERROR_CODES } from '../consts/index.js';

// What the backend's FormatError sends, as the Wails runtime delivers it: the formatter's string
// wrapped in `new Error(string)` (runtime_prod_desktop.js: `t.error instanceof Error ? t.error :
// new Error(t.error)`).
function rejection(code, message) {
  return new Error(JSON.stringify({ code, message }));
}

describe('decodeError', () => {
  it('turns a formatter string into an Error with the message and the code', () => {
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
    const plain = new Error('error parsing arguments: bad'); // Wails' own text, no formatter
    const other = new Error(JSON.stringify({ status: 500 }));
    const broken = new Error('{"code":"io_error","message":');
    const numeric = new Error(JSON.stringify({ code: 7, message: 'x' }));
    const empty = new Error(JSON.stringify({ code: '', message: 'x' }));
    for (const e of [plain, other, broken, numeric, empty]) {
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
    expect(errorCode(new Error('error parsing arguments: bad'))).toBe(ERROR_CODES.INTERNAL);
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
  afterEach(() => {
    delete globalThis.window;
  });

  it('decodes a rejected call, so every binding carries its code', async () => {
    globalThis.window = {
      go: { api: { EventsAPI: { Stats: () => Promise.reject(rejection('persistence_error', 'disk gone')) } } },
    };
    const err = await stats().catch((e) => e);
    expect(err.message).toBe('disk gone');
    expect(err.code).toBe('persistence_error');
  });

  it('passes a resolved call through untouched', async () => {
    const value = { nodes: 3, edges: 1 };
    globalThis.window = { go: { api: { EventsAPI: { Stats: () => Promise.resolve(value) } } } };
    expect(await stats()).toBe(value);
  });

  it('still says the backend is unavailable outside Wails', async () => {
    const err = await stats().catch((e) => e);
    expect(err.message).toMatch(/backend unavailable/);
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
