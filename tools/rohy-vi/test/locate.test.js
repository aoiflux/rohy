'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { findOnPath, resolveRohyctl, bundledRohyctl, restoreExecutableBit } = require('../src/locate.js');

/** A filesystem holding exactly the executables listed. */
const only = (...paths) => (p) => paths.includes(p);

test('finds rohyctl.exe on a Windows PATH, in PATH order', () => {
  const found = findOnPath({
    platform: 'win32',
    pathVar: 'C:\\Windows;C:\\tools;D:\\more',
    isExecutable: only('C:\\tools\\rohyctl.exe', 'D:\\more\\rohyctl.exe'),
  });
  assert.equal(found, 'C:\\tools\\rohyctl.exe');
});

test('finds rohyctl on a POSIX PATH', () => {
  const found = findOnPath({
    platform: 'linux',
    pathVar: '/usr/bin:/home/a/go/bin',
    isExecutable: only('/home/a/go/bin/rohyctl'),
  });
  assert.equal(found, '/home/a/go/bin/rohyctl');
});

test('never looks in a relative PATH entry', () => {
  // An empty or relative entry resolves against the current directory, which is how a binary
  // planted in an opened repository would be run in place of the real one.
  const planted = only('rohyctl', './rohyctl', '.\\rohyctl.exe', 'rohyctl.exe', 'bin\\rohyctl.exe');
  assert.equal(findOnPath({ platform: 'linux', pathVar: ':.:./bin', isExecutable: planted }), null);
  assert.equal(findOnPath({ platform: 'win32', pathVar: ';.;bin;C:rel', isExecutable: planted }), null);
});

test('reads a quoted Windows PATH entry', () => {
  const found = findOnPath({
    platform: 'win32',
    pathVar: '"C:\\Program Files\\rohy";C:\\x',
    isExecutable: only('C:\\Program Files\\rohy\\rohyctl.exe'),
  });
  assert.equal(found, 'C:\\Program Files\\rohy\\rohyctl.exe');
});

test('takes only the .exe on Windows, never a script that shares the name', () => {
  // Node will not run a .cmd or .bat without a shell, and a shell is what this avoids.
  const found = findOnPath({
    platform: 'win32',
    pathVar: 'C:\\a;C:\\b',
    isExecutable: only('C:\\a\\rohyctl.cmd', 'C:\\a\\rohyctl.bat', 'C:\\a\\rohyctl'),
  });
  assert.equal(found, null);
});

test('reports nothing found for an empty PATH', () => {
  assert.equal(findOnPath({ platform: 'linux', pathVar: '', isExecutable: () => true }), null);
  assert.equal(findOnPath({ platform: 'linux', pathVar: undefined, isExecutable: () => true }), null);
});

test('a configured path wins over PATH', () => {
  const got = resolveRohyctl({
    configured: '/opt/rohy/rohyctl',
    platform: 'linux',
    pathVar: '/usr/bin',
    isExecutable: only('/opt/rohy/rohyctl', '/usr/bin/rohyctl'),
  });
  assert.deepEqual(got, { exe: '/opt/rohy/rohyctl', from: 'setting' });
});

test('a configured path that is wrong is reported, not replaced by whatever is on PATH', () => {
  // Falling back would check the file with a rohyctl the author did not choose, and hide the
  // typo in the setting they did.
  for (const configured of ['rohyctl', './bin/rohyctl', '/missing/rohyctl']) {
    const got = resolveRohyctl({
      configured,
      platform: 'linux',
      pathVar: '/usr/bin',
      isExecutable: only('/usr/bin/rohyctl'),
    });
    assert.ok('reason' in got && got.reason.includes('rohy.rohyctlPath'), configured);
  }
});

test('an empty setting means PATH', () => {
  const got = resolveRohyctl({
    configured: '   ',
    platform: 'linux',
    pathVar: '/usr/bin',
    isExecutable: only('/usr/bin/rohyctl'),
  });
  assert.deepEqual(got, { exe: '/usr/bin/rohyctl', from: 'PATH' });
});

test('says so when rohyctl is nowhere', () => {
  const got = resolveRohyctl({
    configured: '',
    bundled: '/ext/bin/rohyctl',
    platform: 'linux',
    pathVar: '/usr/bin',
    isExecutable: () => false,
  });
  assert.ok('reason' in got && /packaged/.test(got.reason) && /PATH/.test(got.reason));
});

const BUNDLED = '/home/a/.vscode/extensions/aoiflux.rohy-vi-0.1.0/bin/linux-x64/rohyctl';

test('the packaged rohyctl wins over PATH', () => {
  // It is built from the same commit as the extension, so it speaks the report the extension reads;
  // one on PATH may be any version.
  const got = resolveRohyctl({
    configured: '',
    bundled: BUNDLED,
    platform: 'linux',
    pathVar: '/usr/bin',
    isExecutable: only(BUNDLED, '/usr/bin/rohyctl'),
  });
  assert.deepEqual(got, { exe: BUNDLED, from: 'extension' });
});

test('a configured path wins over the packaged rohyctl', () => {
  const got = resolveRohyctl({
    configured: '/opt/rohy/rohyctl',
    bundled: BUNDLED,
    platform: 'linux',
    pathVar: '',
    isExecutable: only('/opt/rohy/rohyctl', BUNDLED),
  });
  assert.deepEqual(got, { exe: '/opt/rohy/rohyctl', from: 'setting' });
});

test('a configured path that is wrong is reported, not replaced by the packaged rohyctl', () => {
  const got = resolveRohyctl({
    configured: '/missing/rohyctl',
    bundled: BUNDLED,
    platform: 'linux',
    pathVar: '',
    isExecutable: only(BUNDLED),
  });
  assert.ok('reason' in got && got.reason.includes('rohy.rohyctlPath'));
});

test('a package with no rohyctl for this platform falls back to PATH', () => {
  const got = resolveRohyctl({
    configured: '',
    bundled: BUNDLED,
    platform: 'linux',
    pathVar: '/usr/bin',
    isExecutable: only('/usr/bin/rohyctl'),
  });
  assert.deepEqual(got, { exe: '/usr/bin/rohyctl', from: 'PATH' });
});

test('the packaged rohyctl is the one for this platform and architecture, .exe on Windows only', () => {
  assert.equal(bundledRohyctl('/ext', 'linux', 'arm64'), '/ext/bin/linux-arm64/rohyctl');
  assert.equal(bundledRohyctl('/ext', 'darwin', 'x64'), '/ext/bin/darwin-x64/rohyctl');
  assert.equal(bundledRohyctl('C:\\ext', 'win32', 'x64'), 'C:\\ext\\bin\\win32-x64\\rohyctl.exe');
});

test('every machine the package has a rohyctl for looks it up where the build puts it', () => {
  // The build names its folders process.platform-process.arch; the lookup must use the same names.
  const { plan } = require('../scripts/package.js');
  const manifest = require('../package.json');
  const built = plan(manifest, { version: '0.3.0', commit: 'abc1234', date: '2026-11-18T09:00:00Z' }).rohyctl;
  for (const b of built) {
    const [platform, arch] = b.platform.split('-');
    const looked = bundledRohyctl('/ext', platform, arch).replaceAll('\\', '/');
    assert.equal(looked, `/ext/${b.file}`, b.platform);
  }
});

test('the packaged rohyctl gets back the execute permission a Windows-built package drops', {
  skip: process.platform === 'win32' && 'Windows has no execute permission',
}, () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'rohy-vi-'));
  try {
    const file = path.join(dir, 'rohyctl');
    fs.writeFileSync(file, '', { mode: 0o644 });
    restoreExecutableBit(file);
    fs.accessSync(file, fs.constants.X_OK);
    restoreExecutableBit(path.join(dir, 'missing')); // a package without one: no throw
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
