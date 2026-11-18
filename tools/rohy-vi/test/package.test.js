'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const manifest = require('../package.json');
const { plan, PLATFORMS } = require('../scripts/package.js');

const EXT = path.join(__dirname, '..');

const ROHY = { version: '0.3.0', commit: 'abc1234', date: '2026-11-18T09:00:00Z' };

const option = (args, name) => args[args.indexOf(name) + 1];

test('the package is named for the extension and its own version', () => {
  const p = plan(manifest, ROHY);
  assert.equal(p.out, `rohy-vi-${manifest.version}.vsix`);
  assert.equal(option(p.vsceArgs, '--out'), p.out);
  // One package for every platform: the rohyctl builds inside it are what differ.
  assert.ok(!p.vsceArgs.includes('--target'));
});

test('it carries rohyctl for Windows, Linux and macOS, on x64 and arm64', () => {
  assert.deepEqual(
    plan(manifest, ROHY).rohyctl.map((b) => b.platform).sort(),
    ['darwin-arm64', 'darwin-x64', 'linux-arm64', 'linux-x64', 'win32-arm64', 'win32-x64'],
  );
});

test('each rohyctl is a static build for its own platform, stamped with rohy\'s version', () => {
  for (const b of plan(manifest, ROHY).rohyctl) {
    const { GOOS } = PLATFORMS[b.platform];
    assert.deepEqual(b.goEnv, { CGO_ENABLED: '0', ...PLATFORMS[b.platform] }, b.platform);
    assert.equal(b.file, `bin/${b.platform}/${GOOS === 'windows' ? 'rohyctl.exe' : 'rohyctl'}`);
    assert.equal(option(b.goArgs, '-o'), `tools/rohy-vi/${b.file}`, b.platform);
    assert.equal(b.goArgs.at(-1), './cmd/rohyctl');
    assert.ok(b.goArgs.includes('-trimpath'));
    const ldflags = option(b.goArgs, '-ldflags');
    assert.match(ldflags, /-X rohy\/backend\/version\.Version=0\.3\.0(\s|$)/);
    assert.match(ldflags, /-X rohy\/backend\/version\.Commit=abc1234(\s|$)/);
    assert.match(ldflags, /-X rohy\/backend\/version\.Date=2026-11-18T09:00:00Z(\s|$)/);
  }
});

test('a relative link in the README lands on the file it names in the repository', () => {
  // vsce guesses the base from the repository URL alone, which puts ../../README.md above the
  // repository root. The base must include the extension's own directory.
  const { vsceArgs } = plan(manifest, ROHY);
  const base = option(vsceArgs, '--baseContentUrl');
  assert.equal(new URL('../../README.md', `${base}/`).href, 'https://github.com/aoiflux/rohy/blob/main/README.md');
  const images = option(vsceArgs, '--baseImagesUrl');
  assert.equal(new URL('schemas/x.png', `${images}/`).href, 'https://github.com/aoiflux/rohy/raw/main/tools/rohy-vi/schemas/x.png');
});

test('vsce is pinned, and never resolves dependencies', () => {
  const { vsceArgs } = plan(manifest, ROHY);
  assert.match(vsceArgs[1], /^@vscode\/vsce@\d+\.\d+\.\d+$/);
  assert.equal(vsceArgs[2], 'package');
  assert.ok(vsceArgs.includes('--no-dependencies'));
});

test('a version the Marketplace cannot take is refused before anything is built', () => {
  for (const version of ['0.1.0-beta.1', '0.1', 'v0.1.0', '']) {
    assert.throws(() => plan({ ...manifest, version }, ROHY), /version/, version);
    assert.throws(() => plan(manifest, { ...ROHY, version }), /rohy version/, version);
  }
});

test('only a revision and a timestamp reach the linker flags', () => {
  for (const commit of ['', 'unknown', 'abc1234 -X evil=1', 'ABC1234']) {
    assert.throws(() => plan(manifest, { ...ROHY, commit }), /commit/, commit);
  }
  assert.doesNotThrow(() => plan(manifest, { ...ROHY, commit: 'abc1234-dirty' }));
  for (const date of ['', 'now', '2026-11-18 09:00:00', '2026-11-18T09:00:00+05:30']) {
    assert.throws(() => plan(manifest, { ...ROHY, date }), /date/, date);
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

test('the build script and built packages are left out of the package, and rohyctl is not', () => {
  const ignored = fs.readFileSync(path.join(EXT, '.vscodeignore'), 'utf8').split(/\r?\n/);
  assert.ok(ignored.includes('scripts/**'));
  assert.ok(ignored.includes('*.vsix'));
  assert.ok(!ignored.some((line) => /^(\*\*\/)?bin\b|^\*\*$|^\*$/.test(line.trim())), 'bin/ must be packaged');
});
