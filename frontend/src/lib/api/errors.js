// How a failed backend call carries its error code across the Wails boundary (item 33).
//
// The backend's ErrorFormatter (backend/api/api_errors.go) sends every failure as the JSON of
// {code, message}, as a STRING, because the Wails runtime wraps whatever it receives in
// `new Error(x)` and an object would read "[object Object]". call() in index.js passes every
// rejection through decodeError, so a handler that reads .message reads the message, as it always
// has, and one that needs to know whose fault it was reads .code.
//
// Some failures never reach the formatter: Wails' own "error parsing arguments" text, and the
// "backend unavailable" rejection outside Wails. Those stay as they are, and errorCode calls them
// internal_error, never a refusal by the case.
import { ERROR_CODES } from '../consts/index.js';

/**
 * decodeError returns an Error with the backend's message and its .code when e carries the
 * formatter's string, and e unchanged otherwise.
 * @param {unknown} e
 */
export function decodeError(e) {
  if (!(e instanceof Error) || !e.message.startsWith('{')) return e;
  let parsed;
  try {
    parsed = JSON.parse(e.message);
  } catch {
    return e;
  }
  if (!parsed || typeof parsed.code !== 'string' || parsed.code === '' || typeof parsed.message !== 'string') {
    return e;
  }
  const out = new Error(parsed.message);
  out.code = parsed.code;
  return out;
}

/**
 * errorCode is the error's ERROR_CODES value, or INTERNAL when it carries none.
 * @param {unknown} e
 */
export function errorCode(e) {
  const code = e && typeof e === 'object' ? e.code : undefined;
  return typeof code === 'string' && code !== '' ? code : ERROR_CODES.INTERNAL;
}

/**
 * errorMessage is the text to show for an error of any shape.
 * @param {unknown} e
 */
export function errorMessage(e) {
  if (e && typeof e === 'object' && 'message' in e) return String(e.message);
  return String(e ?? '');
}
