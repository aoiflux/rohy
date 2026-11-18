'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const manifest = require('../package.json');
const { plan } = require('../scripts/package.js');

const EXT = path.join(__dirname, '..');

const option = (args, name) => args[args.indexOf(name) + 1];

test('the package is named for the extension and its own version', () => {
  const p = plan(manifest);
  assert.equal(p.out, `rohy-rules-${manifest.version}.vsix`);
  assert.equal(option(p.vsceArgs, '--out'), p.out);
});

test('a relative link in the README lands on the file it names in the repository', () => {
  // vsce guesses the base from the repository URL alone, which puts ../../README.md above the
  // repository root. The base must include the extension's own directory.
  const base = option(plan(manifest).vsceArgs, '--baseContentUrl');
  assert.equal(new URL('../../README.md', `${base}/`).href, 'https://github.com/aoiflux/rohy/blob/main/README.md');
  const images = option(plan(manifest).vsceArgs, '--baseImagesUrl');
  assert.equal(new URL('schemas/x.png', `${images}/`).href, 'https://github.com/aoiflux/rohy/raw/main/tools/rohy-rules/schemas/x.png');
});

test('vsce is pinned, and never resolves dependencies', () => {
  const args = plan(manifest).vsceArgs;
  assert.match(args[1], /^@vscode\/vsce@\d+\.\d+\.\d+$/);
  assert.equal(args[2], 'package');
  assert.ok(args.includes('--no-dependencies'));
});

test('a version the Marketplace cannot take is refused before anything is built', () => {
  for (const version of ['0.1.0-beta.1', '0.1', 'v0.1.0', '']) {
    assert.throws(() => plan({ ...manifest, version }), /version/, version);
  }
});

test('the icon is a square PNG the Marketplace takes', () => {
  // The Marketplace refuses an SVG icon and asks for at least 128 px. The file is drawn from
  // frontend/src/assets/logo.svg by tools/gen_icons.py, never by hand.
  assert.equal(manifest.icon, 'icon.png');
  const png = fs.readFileSync(path.join(EXT, manifest.icon));
  assert.deepEqual([...png.subarray(0, 8)], [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  const width = png.readUInt32BE(16);
  const height = png.readUInt32BE(20);
  assert.equal(width, height);
  assert.ok(width >= 128, `${width} px`);
});

test('the build script and built packages are left out of the package', () => {
  const ignored = fs.readFileSync(path.join(EXT, '.vscodeignore'), 'utf8').split(/\r?\n/);
  assert.ok(ignored.includes('scripts/**'));
  assert.ok(ignored.includes('*.vsix'));
});
