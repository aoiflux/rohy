'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { parseReport, placeProblem, ReportError, OUTPUT_VERSION } = require('../src/report.js');

const FIXTURES = path.join(__dirname, 'fixtures');

/** Each rule beside the report rohyctl's own test pins for it (cmd/rohyctl/validatejson_test.go). */
function fixture(name) {
  const read = (dir) => fs.readFileSync(path.join(FIXTURES, dir, name), 'utf8');
  return { text: read('rules'), report: parseReport(read('reports')) };
}

function problems(report) {
  const file = report.files[0];
  return [...file.errors, ...file.warnings];
}

function slice(text, problem) {
  const { start, end } = placeProblem(text, problem);
  return text.slice(start, end);
}

test('reads every report rohyctl pins for the extension', () => {
  const names = fs.readdirSync(path.join(FIXTURES, 'rules'));
  assert.ok(names.length >= 4);
  for (const name of names) {
    const { text, report } = fixture(name);
    assert.equal(report.files.length, 1, name);
    for (const p of problems(report)) {
      const { start, end } = placeProblem(text, p);
      assert.ok(0 <= start && start <= end && end <= text.length, `${name}: ${start}..${end}`);
    }
  }
});

test('underlines the value a problem names', () => {
  const { text, report } = fixture('valid-with-warning.json');
  const [warning] = report.files[0].warnings;
  assert.equal(warning.code, 'unknown_field');
  assert.equal(slice(text, warning), '"someone"');
});

test('counts columns in characters, so text after an emoji is not shifted', () => {
  // rohyctl counts runes; VS Code counts UTF-16 units, in which 🔥 is two. Read naively, the
  // underline would start one character early, inside the key before it.
  const { text, report } = fixture('wide-characters.json');
  const [error] = report.files[0].errors;
  assert.equal(error.code, 'unknown_algorithm');
  assert.equal(slice(text, error), '"bogus"');
});

test('underlines a word where a syntax error points into one', () => {
  const { text, report } = fixture('syntax.json');
  const [error] = report.files[0].errors;
  assert.equal(error.code, 'syntax');
  assert.equal(slice(text, error), 'sequence');
});

test('puts a problem with no position on the first line', () => {
  const { text, report } = fixture('unlocated.json');
  const [error] = report.files[0].errors;
  assert.equal(error.line, 0);
  const { start, end } = placeProblem(text, error);
  assert.equal(start, 0);
  assert.equal(text.slice(start, end), text.split('\n')[0]);
});

test('places a problem the same way in a file with CRLF line endings', () => {
  for (const name of ['valid-with-warning.json', 'wide-characters.json', 'syntax.json']) {
    const { text, report } = fixture(name);
    const crlf = text.replace(/\r?\n/g, '\r\n');
    for (const p of problems(report)) assert.equal(slice(crlf, p), slice(text.replace(/\r\n/g, '\n'), p), name);
  }
});

test('clamps a position past the end of its line or the file', () => {
  const text = '{\n  "a": 1\n}';
  const at = (line, col) => placeProblem(text, { code: 'x', index: -1, line, col, message: 'm' });
  const eol = at(2, 99);
  assert.ok(eol.start <= eol.end && eol.end <= text.indexOf('\n', 2));
  assert.ok(eol.end > eol.start, 'a squiggle at the end of a line must still be visible');
  const beyond = at(40, 1);
  assert.deepEqual(beyond, { start: 0, end: 1 }); // a line that does not exist is unlocated
  assert.deepEqual(placeProblem('', { code: 'x', index: -1, line: 1, col: 1, message: 'm' }), { start: 0, end: 0 });
});

test('refuses output it cannot trust, rather than showing a clean file', () => {
  const good = JSON.parse(fs.readFileSync(path.join(FIXTURES, 'reports', 'syntax.json'), 'utf8'));
  const variants = {
    'not JSON': 'checked 1 file, all valid',
    empty: '',
    'a newer shape': JSON.stringify({ ...good, output_version: OUTPUT_VERSION + 1 }),
    'no files': JSON.stringify({ ...good, files: undefined }),
    'null errors': JSON.stringify({ ...good, files: [{ ...good.files[0], errors: null }] }),
    'a fractional line': JSON.stringify({
      ...good,
      files: [{ ...good.files[0], errors: [{ ...good.files[0].errors[0], line: 1.5 }] }],
    }),
    'a negative column': JSON.stringify({
      ...good,
      files: [{ ...good.files[0], errors: [{ ...good.files[0].errors[0], col: -1 }] }],
    }),
    'a code that is not text': JSON.stringify({
      ...good,
      files: [{ ...good.files[0], errors: [{ ...good.files[0].errors[0], code: 7 }] }],
    }),
    'valid with errors': JSON.stringify({ ...good, files: [{ ...good.files[0], valid: true }] }),
  };
  for (const [name, out] of Object.entries(variants)) {
    assert.throws(() => parseReport(out), ReportError, name);
  }
});

// A seeded generator, so a failure names its case and reruns the same way.
function rng(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 2 ** 32;
  };
}

test('places any problem inside the text, whatever the text', () => {
  const next = rng(0x5eed);
  const alphabet = ['{', '}', '"', '\\', 'a', ' ', '\n', '\r', ',', ':', 'é', '🔥', ' '];
  for (let n = 0; n < 20000; n++) {
    let text = '';
    const length = Math.floor(next() * 30);
    for (let i = 0; i < length; i++) text += alphabet[Math.floor(next() * alphabet.length)];
    const problem = {
      code: 'x',
      index: -1,
      line: Math.floor(next() * 8),
      col: Math.floor(next() * 12),
      message: 'm',
    };
    const { start, end } = placeProblem(text, problem);
    if (!(Number.isInteger(start) && Number.isInteger(end) && 0 <= start && start <= end && end <= text.length)) {
      assert.fail(`${JSON.stringify(text)} ${problem.line}:${problem.col} -> ${start}..${end}`);
    }
    // Never between the halves of a surrogate pair, which would split a character in two.
    for (const at of [start, end]) {
      const unit = text.charCodeAt(at);
      if (at > 0 && at < text.length && unit >= 0xdc00 && unit <= 0xdfff) {
        assert.fail(`${JSON.stringify(text)} ${problem.line}:${problem.col} splits a character at ${at}`);
      }
    }
  }
});
