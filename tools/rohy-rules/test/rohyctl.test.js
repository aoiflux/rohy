'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');

const { createClient, runProcess, RunError } = require('../src/rohyctl.js');
const { ReportError } = require('../src/report.js');

const FAKE = path.join(__dirname, 'fake-rohyctl.js');

/** A client whose rohyctl is the fake in the given mode. */
function fake(mode, options = {}) {
  return createClient({ exe: process.execPath, prefixArgs: [FAKE, mode], ...options });
}

test('validate sends the text on stdin and asks for the JSON report', async () => {
  const calls = [];
  const report = require('node:fs').readFileSync(path.join(__dirname, 'fixtures', 'reports', 'syntax.json'), 'utf8');
  const client = createClient({
    exe: '/opt/rohyctl',
    run: async (exe, args, input) => {
      calls.push({ exe, args, input });
      return { code: 1, stdout: report, stderr: '' };
    },
  });
  await client.validate('{"name": "é"}');
  await client.format('{}');
  assert.deepEqual(calls, [
    { exe: '/opt/rohyctl', args: ['validate', '--json'], input: '{"name": "é"}' },
    { exe: '/opt/rohyctl', args: ['format'], input: '{}' },
  ]);
});

test('the process runner passes arguments and stdin through unchanged', async () => {
  const r = await runProcess(process.execPath, [FAKE, 'args', 'validate', '--json', 'a b', '"q"'], '', {});
  assert.deepEqual(JSON.parse(r.stdout), ['validate', '--json', 'a b', '"q"']);
  const echoed = await runProcess(process.execPath, [FAKE, 'echo'], 'Ünïcode 🔥\r\n', {});
  assert.equal(echoed.stdout, 'Ünïcode 🔥\r\n');
  assert.equal(echoed.code, 0);
});

test('validate returns the one file of a report', async () => {
  const report = await fake('report:wide-characters.json').validate('{}');
  assert.equal(report.valid, false);
  assert.equal(report.errors[0].code, 'unknown_algorithm');
  const clean = await fake('report:valid-with-warning.json').validate('{}');
  assert.equal(clean.valid, true);
  assert.equal(clean.warnings.length, 1);
});

test('a large rule goes through whole, without the pipes deadlocking', async () => {
  // Bigger than any pipe buffer, and as big as a rule may be.
  const text = `{"description": "${'a'.repeat(1024 * 1024 - 40)}"}`;
  const out = await fake('echo').format(text);
  assert.ok(out.ok);
  assert.equal(out.text, text);
});

test('a rohyctl from before --json is reported as too old, not as a clean file', async () => {
  await assert.rejects(fake('old').validate('{}'), (e) => {
    assert.ok(e instanceof RunError);
    assert.match(e.message, /older than this extension/);
    return true;
  });
});

test('output that is not a report is refused', async () => {
  await assert.rejects(fake('garbage').validate('{}'), ReportError);
});

test('an exit code that contradicts the report is refused', async () => {
  await assert.rejects(fake('lies').validate('{}'), ReportError);
});

test('a rohyctl that hangs is stopped at the time limit', async () => {
  const started = Date.now();
  await assert.rejects(fake('hang', { timeoutMs: 300 }).validate('{}'), (e) => {
    assert.ok(e instanceof RunError && e.kind === 'timeout', String(e));
    return true;
  });
  assert.ok(Date.now() - started < 5000);
});

test('output past the cap is refused rather than read into memory', async () => {
  await assert.rejects(fake('flood', { maxOutputBytes: 1024 * 1024 }).format('{}'), (e) => {
    assert.ok(e instanceof RunError && e.kind === 'too-large', String(e));
    return true;
  });
});

test('a run can be cancelled', async () => {
  const controller = new AbortController();
  const pending = fake('hang', { timeoutMs: 10000 }).validate('{}', { signal: controller.signal });
  setTimeout(() => controller.abort(), 50);
  await assert.rejects(pending, (e) => {
    assert.ok(e instanceof RunError && e.kind === 'cancelled', String(e));
    return true;
  });
});

test('a program that is not there is a spawn failure', async () => {
  const client = createClient({ exe: path.join(__dirname, 'no-such-rohyctl.exe') });
  await assert.rejects(client.validate('{}'), (e) => {
    assert.ok(e instanceof RunError && e.kind === 'spawn', String(e));
    return true;
  });
});

test('format returns the canonical text, or why there is none', async () => {
  const ok = await fake('echo').format('{"a": 1}\n');
  assert.deepEqual(ok, { ok: true, text: '{"a": 1}\n' });
  const refused = await fake('cannot-format').format('{');
  assert.equal(refused.ok, false);
  // The reason is rohyctl's, without the <stdin> it would mean nothing to the author.
  assert.equal(refused.reason, "invalid character '}' looking for beginning of value");
});

test('version reads the build identity', async () => {
  assert.equal(await fake('version').version(), 'rohyctl 0.3.0 abc1234 2026-11-07');
});
