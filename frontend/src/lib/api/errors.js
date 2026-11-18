// How a failed backend call carries its error code across the Wails boundary.
//
// The backend's MarshalError (backend/api/api_errors.go) describes every failure as {code, message}.
// The Wails runtime rejects the call with an Error whose .cause is that object. call() in index.js
// passes every rejection through decodeError, so a handler that reads .message reads the backend's
// message, as it always has, and one that needs to know whose fault it was reads .code.
//
// Some failures carry no cause: a call Wails could not make (a ReferenceError for an unknown method,
// a TypeError for the wrong arguments), and a call made outside Wails. Those stay as they are, and
// errorCode calls them internal_error, never a refusal by the case.
import { ERROR_CODES } from '../consts/index.js';

/**
 * decodeError returns an Error with the backend's message and its .code when e carries the
 * backend's cause, and e unchanged otherwise.
 * @param {unknown} e
 */
export function decodeError(e) {
  if (!(e instanceof Error)) return e;
  const cause = e.cause;
  if (!cause || typeof cause !== 'object' || typeof cause.code !== 'string' || cause.code === '' || typeof cause.message !== 'string') {
    return e;
  }
  const out = new Error(cause.message);
  out.code = cause.code;
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
