'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');

const { createController } = require('../src/controller.js');
const { parseReport } = require('../src/report.js');
const { RunError } = require('../src/rohyctl.js');
const { createFakeVscode } = require('./fake-vscode.js');

const manifest = require('../package.json');
const FIXTURES = path.join(__dirname, 'fixtures');
const RULES = path.join('C:', 'case', 'rohy-data', 'rules');

function fixture(name) {
  const read = (dir) => fs.readFileSync(path.join(FIXTURES, dir, name), 'utf8');
  return { text: read('rules'), file: parseReport(read('reports')).files[0] };
}

/** Timers that fire only when told to, so "typing pauses" is a step of the test. */
function manualClock() {
  let next = 1;
  const pending = new Map();
  return {
    setTimer: (fn) => {
      pending.set(next, fn);
      return next++;
    },
    clearTimer: (id) => pending.delete(id),
    flush() {
      const due = [...pending.values()];
      pending.clear();
      due.forEach((fn) => fn());
    },
  };
}

const settle = async () => {
  for (let i = 0; i < 5; i++) await new Promise((r) => setImmediate(r));
};

/** A rohyctl that answers with fixture reports: the probe's `{}` with an invalid one. */
function fakeClient() {
  const client = {
    exe: 'C:\\tools\\rohyctl.exe',
    validated: [],
    formatted: [],
    answer: async (text) => (text === '{}' ? fixture('unlocated.json').file : fixture('valid-with-warning.json').file),
    formatAnswer: async (text) => ({ ok: true, text }),
    validate(text, extra) {
      if (text !== '{}') client.validated.push(text);
      return client.answer(text, extra);
    },
    format(text) {
      client.formatted.push(text);
      return client.formatAnswer(text);
    },
    version: async () => 'rohyctl 0.3.0 abc1234 2026-11-07',
  };
  return client;
}

async function setup({ settings = {}, resolve, client = fakeClient() } = {}) {
  const fake = createFakeVscode(settings);
  const clock = manualClock();
  const controller = createController(fake.vscode, {
    manifest,
    resolve: resolve ?? (() => ({ exe: client.exe, from: 'PATH' })),
    createClient: () => client,
    setTimer: clock.setTimer,
    clearTimer: clock.clearTimer,
  });
  await controller.start();
  // Whatever is under way settles first: a re-discovery schedules its checks only once its
  // probe has answered.
  const pause = async () => {
    await settle();
    clock.flush();
    await settle();
  };
  return { fake, clock, client, controller, pause };
}

test('checks a rule file and places its problems on the token they name', async () => {
  const { fake, client, pause } = await setup();
  const { text, file } = fixture('wide-characters.json');
  client.answer = async () => file;
  const doc = fake.open(path.join(RULES, 'wide.json'), text);
  await pause();

  const [d] = fake.diagnostics.get(doc.uri.toString());
  assert.equal(d.code, 'unknown_algorithm');
  assert.equal(d.source, 'rohy');
  assert.equal(d.severity, fake.vscode.DiagnosticSeverity.Error);
  // Line 2, rune column 44: "bogus" comes after an emoji that is one rune and two UTF-16
  // units, so it starts at UTF-16 character 44, not 43.
  assert.deepEqual([d.range.start.line, d.range.start.character], [1, 44]);
  assert.deepEqual([d.range.end.line, d.range.end.character], [1, 51]);
});

test('leaves other JSON, and the rules folder state file, alone', async () => {
  const { fake, client, pause } = await setup();
  fake.open(path.join('C:', 'repo', 'package.json'), '{}');
  fake.open(path.join(RULES, 'rules-state.json'), '{"a": false}');
  fake.open(path.join(RULES, 'notes.txt'), 'not json');
  await pause();
  assert.deepEqual(client.validated, []);
  assert.equal(fake.diagnostics.size, 0);
});

test('checks once when typing pauses, and checks the latest text', async () => {
  const { fake, client, pause } = await setup();
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  for (const text of ['{"', '{"n', '{"na', '{"name": 1}']) fake.edit(doc, text);
  await pause();
  assert.deepEqual(client.validated, ['{"name": 1}']);
});

test('drops an answer about text that has changed since', async () => {
  const { fake, client, pause } = await setup();
  const { file: stale } = fixture('wide-characters.json');
  const { file: fresh } = fixture('syntax.json');
  let release;
  client.answer = (text) =>
    text === 'first' ? new Promise((resolve) => (release = () => resolve(stale))) : Promise.resolve(fresh);

  const doc = fake.open(path.join(RULES, 'a.json'), 'first');
  await pause(); // the first run is now in flight
  fake.edit(doc, 'second');
  await pause();
  release();
  await settle();

  const shown = fake.diagnostics.get(doc.uri.toString());
  assert.deepEqual(
    shown.map((d) => d.code),
    ['syntax'],
  );
});

test('a closed file loses its problems, and a late answer does not bring them back', async () => {
  const { fake, client, pause } = await setup();
  let release;
  client.answer = () => new Promise((resolve) => (release = () => resolve(fixture('syntax.json').file)));
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  await pause();
  fake.close(doc);
  release();
  await settle();
  assert.equal(fake.diagnostics.has(doc.uri.toString()), false);
});

test('a failed run removes the earlier problems and says why', async () => {
  const { fake, client, pause } = await setup();
  client.answer = async () => fixture('syntax.json').file;
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  fake.activate(doc);
  await pause();
  assert.equal(fake.diagnostics.get(doc.uri.toString()).length, 1);

  client.answer = async () => {
    throw new RunError('timeout', 'rohyctl did not finish in 10000 ms');
  };
  fake.edit(doc, '{"name": "x"}');
  await pause();
  assert.equal(fake.diagnostics.has(doc.uri.toString()), false);
  assert.match(fake.status.text, /warning/);
  assert.match(fake.status.tooltip, /did not finish/);
});

test('without rohyctl, nothing is run and the status bar says the schema is all there is', async () => {
  const client = fakeClient();
  const { fake, pause } = await setup({ client, resolve: () => ({ reason: 'rohyctl was not found on PATH' }) });
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  fake.activate(doc);
  await pause();
  assert.deepEqual(client.validated, []);
  assert.equal(fake.diagnostics.size, 0);
  assert.match(fake.status.text, /schema only/);
  assert.match(fake.status.tooltip, /may still not load/);
  // Not having rohyctl installed is not worth a pop-up.
  assert.deepEqual(fake.messages.warnings, []);
});

test('a rohyctl that fails the probe is not used', async () => {
  const client = fakeClient();
  client.answer = async () => {
    throw new RunError('failed', 'this rohyctl is older than this extension: it has no validate --json');
  };
  const { fake, pause } = await setup({ client });
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  fake.activate(doc);
  await pause();
  assert.deepEqual(client.validated, []);
  assert.match(fake.status.text, /schema only/);
  assert.match(fake.status.tooltip, /older than this extension/);
});

test('a broken rohy.rohyctlPath warns once, not on every change', async () => {
  const reason = 'rohy.rohyctlPath is "C:\\nope.exe", which is not an executable file';
  const { fake } = await setup({ resolve: () => ({ reason }) });
  fake.configure({ 'rohy.ruleFiles': ['x/*.json'] });
  await settle();
  assert.equal(fake.messages.warnings.length, 1);
  assert.match(fake.messages.warnings[0], /rohyctlPath/);
});

test('formats the whole file, and leaves a formatted one alone', async () => {
  const { fake, client } = await setup();
  const doc = fake.open(path.join(RULES, 'a.json'), '{"a":1}');
  const [{ provider, selector }] = fake.formatters;
  assert.ok(selector.some((s) => s.pattern === '**/rohy-data/rules/*.json' && s.language === 'json'));

  client.formatAnswer = async () => ({ ok: true, text: '{\n  "a": 1\n}\n' });
  const [edit] = await provider.provideDocumentFormattingEdits(doc, {}, undefined);
  assert.equal(edit.newText, '{\n  "a": 1\n}\n');
  assert.deepEqual([edit.range.start, edit.range.end], [new fake.vscode.Position(0, 0), new fake.vscode.Position(0, 7)]);

  client.formatAnswer = async (text) => ({ ok: true, text });
  assert.deepEqual(await provider.provideDocumentFormattingEdits(doc, {}, undefined), []);
});

test('a file that cannot be formatted is left as it is, and the author is told why', async () => {
  const { fake, client } = await setup();
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  client.formatAnswer = async () => ({ ok: false, reason: 'unexpected end of JSON input' });
  const edits = await fake.formatters[0].provider.provideDocumentFormattingEdits(doc, {}, undefined);
  assert.deepEqual(edits, []);
  assert.match(fake.messages.warnings.at(-1), /unexpected end of JSON input/);
});

test('without rohyctl, formatting says why it cannot', async () => {
  const { fake } = await setup({ resolve: () => ({ reason: 'rohyctl was not found on PATH' }) });
  const doc = fake.open(path.join(RULES, 'a.json'), '{');
  assert.deepEqual(await fake.formatters[0].provider.provideDocumentFormattingEdits(doc, {}, undefined), []);
  assert.match(fake.messages.warnings.at(-1), /cannot format without rohyctl.*not found on PATH/);
});

test('the formatter never touches the state file', async () => {
  const { fake, client } = await setup();
  const doc = fake.open(path.join(RULES, 'rules-state.json'), '{"a":false}');
  assert.deepEqual(await fake.formatters[0].provider.provideDocumentFormattingEdits(doc, {}, undefined), []);
  assert.deepEqual(client.formatted, []);
});

test('rohy.ruleFiles adds folders, and removing one clears its problems', async () => {
  const { fake, client, pause } = await setup();
  const doc = fake.open(path.join('C:', 'repo', 'detections', 'a.json'), '{');
  await pause();
  assert.deepEqual(client.validated, []);

  fake.configure({ 'rohy.ruleFiles': ['detections/*.json'] });
  await pause();
  assert.deepEqual(client.validated, ['{']);
  assert.ok(fake.diagnostics.has(doc.uri.toString()));
  const live = fake.formatters.filter((f) => !f.disposed);
  assert.equal(live.length, 1, 'the old formatter registration was not disposed');
  assert.ok(live[0].selector.some((s) => s.pattern === '**/detections/*.json'));

  fake.configure({ 'rohy.ruleFiles': [] });
  await pause();
  assert.equal(fake.diagnostics.has(doc.uri.toString()), false);
});

test('the status bar shows only beside a rule file', async () => {
  const { fake, pause } = await setup();
  const rule = fake.open(path.join(RULES, 'a.json'), '{');
  const other = fake.open(path.join('C:', 'repo', 'package.json'), '{}');
  await pause();
  fake.activate(other);
  assert.equal(fake.status.visible, false);
  fake.activate(rule);
  assert.equal(fake.status.visible, true);
  assert.match(fake.status.tooltip, /rohyctl\.exe \(rohyctl 0\.3\.0/);
  assert.equal(fake.status.command, 'rohy.showLog');
});

test('the entry point wires the real modules to the API it is given', async () => {
  // extension.js requires 'vscode', which exists only inside VS Code. Resolve it to the fake.
  const fake = createFakeVscode({ 'rohy.rohyctlPath': path.join(__dirname, 'no-such-rohyctl.exe') });
  const resolveFilename = Module._resolveFilename;
  const fakePath = path.join(__dirname, '__vscode__.js');
  require.cache[fakePath] = { id: fakePath, filename: fakePath, loaded: true, exports: fake.vscode };
  Module._resolveFilename = function (request, ...rest) {
    return request === 'vscode' ? fakePath : resolveFilename.call(this, request, ...rest);
  };
  try {
    delete require.cache[require.resolve('../src/extension.js')];
    const extension = require('../src/extension.js');
    // An installed extension with no packaged rohyctl in it.
    const extensionPath = path.join(__dirname, 'no-such-extension');
    const context = { extensionPath, extension: { packageJSON: manifest }, subscriptions: [] };
    extension.activate(context);
    await settle();
    assert.equal(context.subscriptions.length, 1);
    assert.ok(fake.commands.has('rohy.showLog'));
    assert.equal(fake.formatters.length, 1);
    // The configured path does not exist: reported, and no fallback to whatever PATH holds.
    assert.equal(fake.messages.warnings.length, 1);
    assert.match(fake.messages.warnings[0], /not an executable file/);
    context.subscriptions[0].dispose();
    assert.equal(fake.formatters[0].disposed, true);
  } finally {
    Module._resolveFilename = resolveFilename;
    delete require.cache[fakePath];
  }
});
