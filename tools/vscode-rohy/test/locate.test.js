'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { findOnPath, resolveRohyctl } = require('../src/locate.js');

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
  const got = resolveRohyctl({ configured: '', platform: 'linux', pathVar: '/usr/bin', isExecutable: () => false });
  assert.ok('reason' in got && /not found/.test(got.reason));
});
