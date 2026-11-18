'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const manifest = require('../package.json');
const { STATE_FILE, defaultPatterns, normalizePattern, rulePatterns, isStateFile } = require('../src/rulefiles.js');
const { globToRegExp } = require('./fake-vscode.js');

const EXT = path.join(__dirname, '..');
const REPO = path.join(EXT, '..', '..');
const read = (...parts) => fs.readFileSync(path.join(...parts), 'utf8');

test('the extension has no dependencies', () => {
  // It runs inside the author's editor; everything it needs is Node and the VS Code API.
  assert.equal(manifest.dependencies, undefined);
  assert.equal(manifest.devDependencies, undefined);
});

test('its licence is the repository licence, word for word', () => {
  // The packager wants a LICENSE beside the manifest; a copy that drifted would license the
  // extension differently from the code it ships.
  assert.equal(read(EXT, 'LICENSE').replace(/\r\n/g, '\n'), read(REPO, 'LICENSE').replace(/\r\n/g, '\n'));
  assert.equal(manifest.license, 'MIT');
});

test('the entry point and every module it loads exist', () => {
  const main = path.join(EXT, manifest.main);
  assert.ok(fs.existsSync(main), manifest.main);
  for (const file of fs.readdirSync(path.join(EXT, 'src'))) {
    for (const [, dep] of read(EXT, 'src', file).matchAll(/require\('(\.[^']+)'\)/g)) {
      assert.ok(fs.existsSync(path.join(EXT, 'src', dep)), `${file} requires ${dep}`);
    }
  }
});

test('the schema it attaches is the one tools/genschema writes', () => {
  // genschema's -check keeps that file current; this keeps the manifest pointing at it.
  const [association] = manifest.contributes.jsonValidation;
  const [, out] = read(REPO, 'tools', 'genschema', 'main.go').match(/defaultOut = "([^"]+)"/);
  assert.equal(path.resolve(EXT, association.url), path.resolve(REPO, out));
  assert.ok(fs.existsSync(path.resolve(EXT, association.url)));
});

test('the state file is excluded from the schema, under the name the application uses', () => {
  const [, name] = read(REPO, 'backend', 'consts', 'consts.go').match(/RuleStateFile\s*=\s*"([^"]+)"/);
  assert.equal(STATE_FILE, name);
  const { fileMatch } = manifest.contributes.jsonValidation[0];
  // VS Code applies the last pattern that matches, so the exclusion has to come last.
  assert.equal(fileMatch.at(-1), `!**/${name}`);
  assert.deepEqual(defaultPatterns(manifest), fileMatch.slice(0, -1));
});

test('the default patterns find the application rules folder and nothing beside it', () => {
  const matches = (p) => defaultPatterns(manifest).some((g) => globToRegExp(g).test(p));
  assert.ok(matches('C:/cases/a/rohy-data/rules/brute-force.json'));
  assert.ok(matches('/home/a/rohy/backend/rules/builtin/brute-force-then-success.json'));
  assert.ok(!matches('C:/cases/a/rohy-data/graphs/registry.json'));
  assert.ok(!matches('C:/cases/a/rohy-data/rules/old/brute-force.json'));
  assert.ok(isStateFile('C:\\cases\\a\\rohy-data\\rules\\rules-state.json'));
  assert.ok(!isStateFile('C:\\cases\\a\\rohy-data\\rules\\my-rules-state.json'));
});

test('a pattern from the setting is made to match an absolute path', () => {
  assert.equal(normalizePattern('rules/*.json'), '**/rules/*.json');
  assert.equal(normalizePattern('./rules/*.json'), '**/rules/*.json');
  assert.equal(normalizePattern('**/x/*.json'), '**/x/*.json');
  assert.equal(normalizePattern('C:\\work\\rules\\*.json'), 'c:/work/rules/*.json');
  assert.equal(normalizePattern('/srv/rules/*.json'), '/srv/rules/*.json');
  assert.equal(normalizePattern('  '), '');
  assert.deepEqual(rulePatterns(manifest, ['rules/*.json', 7, null, '', 'rules/*.json']), [
    ...defaultPatterns(manifest),
    '**/rules/*.json',
  ]);
  assert.deepEqual(rulePatterns(manifest, 'not a list'), defaultPatterns(manifest));
});

test('an opened folder cannot choose the program that is run', () => {
  // 🔒 The manifest declares full support in untrusted folders. That is true only because the
  // rohyctl path is machine-scoped: VS Code never reads it from a folder's settings, trusted or
  // not, so a repository cannot point the extension at a binary of its own.
  const { properties } = manifest.contributes.configuration;
  assert.equal(properties['rohy.rohyctlPath'].scope, 'machine');
  assert.equal(manifest.capabilities.untrustedWorkspaces.supported, true);
});

test('every contributed command is registered, and every registered one contributed', () => {
  const contributed = manifest.contributes.commands.map((c) => c.command).sort();
  const registered = [...read(EXT, 'src', 'controller.js').matchAll(/registerCommand\('([^']+)'/g)].map((m) => m[1]).sort();
  assert.deepEqual(registered, contributed);
});

test('it activates for JSON, and the test script runs only the tests', () => {
  assert.deepEqual(manifest.activationEvents, ['onLanguage:json']);
  assert.ok(manifest.engines.vscode);
  // The fake rohyctl lives in test/ too; Node's default discovery would run it as a test.
  assert.equal(manifest.scripts.test, 'node --test test/*.test.js');
});
