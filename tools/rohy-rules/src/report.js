// @ts-check
'use strict';

// Reading `rohyctl validate --json` and placing its problems in the text.
//
// The report is checked field by field before anything is shown. It comes from whichever
// rohyctl the machine has, which may be older or newer than this extension, and a reader that
// took what it could would turn a report it misread into a file with no problems — the one
// answer an editor must never give by accident.
//
// Pure: text and a report in, offsets out. The VS Code layer turns offsets into positions,
// which keeps it out of the business of counting lines (see placeProblem).

/** The shape of the report this extension reads; rohyctl's jsonOutputVersion. */
const OUTPUT_VERSION = 1;

class ReportError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = 'ReportError';
  }
}

/**
 * @typedef {{code: string, field?: string, index: number, line: number, col: number, message: string}} Problem
 * @typedef {{name: string, valid: boolean, errors: Problem[], warnings: Problem[]}} FileReport
 * @typedef {{files: FileReport[], checked: number, failed: number}} Report
 */

/** @param {unknown} v @returns {v is Record<string, unknown>} */
function isObject(v) {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/** @param {unknown} v */
function isCount(v) {
  return Number.isInteger(v) && /** @type {number} */ (v) >= 0;
}

/** @param {unknown} raw @param {string} where @returns {Problem} */
function problemOf(raw, where) {
  if (!isObject(raw)) throw new ReportError(`${where} is not a problem`);
  const { code, field, index, line, col, message } = raw;
  if (typeof code !== 'string' || code === '') throw new ReportError(`${where} has no code`);
  if (typeof message !== 'string') throw new ReportError(`${where} has no message`);
  if (field !== undefined && typeof field !== 'string') throw new ReportError(`${where} has a field that is not text`);
  if (!Number.isInteger(index)) throw new ReportError(`${where} has an index that is not a whole number`);
  if (!isCount(line) || !isCount(col)) throw new ReportError(`${where} has a position that is not a line and column`);
  return {
    code,
    ...(field === undefined ? {} : { field }),
    index: /** @type {number} */ (index),
    line: /** @type {number} */ (line),
    col: /** @type {number} */ (col),
    message,
  };
}

/** @param {unknown} raw @param {string} where @returns {Problem[]} */
function problemsOf(raw, where) {
  if (!Array.isArray(raw)) throw new ReportError(`${where} is not a list`);
  return raw.map((p, i) => problemOf(p, `${where}[${i}]`));
}

/**
 * parseReport reads rohyctl's stdout, or throws ReportError saying what was wrong with it.
 * @param {string} stdout
 * @returns {Report}
 */
function parseReport(stdout) {
  let data;
  try {
    data = JSON.parse(stdout);
  } catch (e) {
    throw new ReportError(`rohyctl did not print a report: ${/** @type {Error} */ (e).message}`);
  }
  if (!isObject(data)) throw new ReportError('rohyctl printed something other than a report');
  if (data.output_version !== OUTPUT_VERSION) {
    throw new ReportError(
      `rohyctl reports in shape ${String(data.output_version)}; this extension reads shape ${OUTPUT_VERSION}`,
    );
  }
  if (!Array.isArray(data.files)) throw new ReportError('the report lists no files');
  const files = data.files.map((raw, i) => {
    const where = `files[${i}]`;
    if (!isObject(raw)) throw new ReportError(`${where} is not a file`);
    if (typeof raw.name !== 'string' || typeof raw.valid !== 'boolean') {
      throw new ReportError(`${where} has no name or verdict`);
    }
    const errors = problemsOf(raw.errors, `${where}.errors`);
    const warnings = problemsOf(raw.warnings, `${where}.warnings`);
    // The verdict and the list must agree. A file called valid that has errors, or invalid with
    // none, is a report this extension does not understand, not one to pick a side of.
    if (raw.valid !== (errors.length === 0)) throw new ReportError(`${where} has a verdict its errors contradict`);
    return { name: raw.name, valid: raw.valid, errors, warnings };
  });
  if (!isCount(data.checked) || !isCount(data.failed)) throw new ReportError('the report has no totals');
  return { files, checked: /** @type {number} */ (data.checked), failed: /** @type {number} */ (data.failed) };
}

/** The UTF-16 width of the character starting at `at`: 2 for a surrogate pair, else 1. */
function widthAt(/** @type {string} */ text, /** @type {number} */ at) {
  const unit = text.charCodeAt(at);
  if (unit >= 0xd800 && unit <= 0xdbff && at + 1 < text.length) {
    const next = text.charCodeAt(at + 1);
    if (next >= 0xdc00 && next <= 0xdfff) return 2;
  }
  return 1;
}

/** The offset where the character ending at `end` starts. */
function startBefore(/** @type {string} */ text, /** @type {number} */ end) {
  if (end >= 2 && widthAt(text, end - 2) === 2) return end - 2;
  return end - 1;
}

/** Where line `line` (1-based) starts, counting lines by "\n" as rohyctl does; -1 past the end. */
function lineStart(/** @type {string} */ text, /** @type {number} */ line) {
  let at = 0;
  for (let n = 1; n < line; n++) {
    const nl = text.indexOf('\n', at);
    if (nl === -1) return -1;
    at = nl + 1;
  }
  return at;
}

/** Where the line starting at `start` ends, before its "\n" and a "\r" in front of it. */
function lineEnd(/** @type {string} */ text, /** @type {number} */ start) {
  let end = text.indexOf('\n', start);
  if (end === -1) end = text.length;
  if (end > start && text[end - 1] === '\r') end--;
  return end;
}

const WORD = /[\w.+-]/;

/** The end of the token starting at `at`: a whole string, a whole word, or one character. */
function tokenEnd(/** @type {string} */ text, /** @type {number} */ at, /** @type {number} */ end) {
  if (text[at] === '"') {
    for (let i = at + 1; i < end; i++) {
      if (text[i] === '\\') i++;
      else if (text[i] === '"') return i + 1;
    }
    return end;
  }
  if (WORD.test(text[at])) {
    let i = at;
    while (i < end && WORD.test(text[i])) i++;
    return i;
  }
  return Math.min(at + widthAt(text, at), end);
}

/**
 * placeProblem returns the UTF-16 offsets to underline for a problem: the token at its line
 * and column, the last character of the line when it points past the end, and the first line
 * when it has no position.
 *
 * Offsets rather than line and character, so the caller converts them with the document's own
 * positionAt. rohyctl counts lines by "\n" only and VS Code also breaks at a lone "\r"; counting
 * both here would put a problem on the wrong line of such a file.
 *
 * rohyctl's columns count characters (runes); VS Code's count UTF-16 units, in which anything
 * outside the Basic Multilingual Plane is two. The column is walked character by character.
 * @param {string} text
 * @param {Problem} problem
 * @returns {{start: number, end: number}}
 */
function placeProblem(text, problem) {
  const start = problem.line >= 1 ? lineStart(text, problem.line) : -1;
  if (start === -1) return { start: 0, end: lineEnd(text, 0) };
  const end = lineEnd(text, start);
  let at = start;
  for (let c = 1; c < problem.col && at < end; c++) at += widthAt(text, at);
  at = Math.min(at, end);
  if (at === end) {
    // Past the end of the line: underline its last character, so the squiggle can be seen.
    return at > start ? { start: startBefore(text, at), end: at } : { start: at, end: at };
  }
  return { start: at, end: tokenEnd(text, at, end) };
}

module.exports = { OUTPUT_VERSION, ReportError, parseReport, placeProblem };
