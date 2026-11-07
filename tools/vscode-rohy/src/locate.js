// @ts-check
'use strict';

// Finding rohyctl: the `rohy.rohyctlPath` setting when it is set, otherwise PATH.
//
// PATH is read here and nowhere else. It is the operating system's own answer to "which
// program is rohyctl", which is the question being asked — not configuration smuggled in
// through the environment.
//
// The search is done here rather than left to the process spawner, because a spawner may look
// in the current directory first (libuv does on Windows), and an opened folder is not a place
// to take programs from. Only absolute entries are searched, and the setting is read only from
// user settings (the manifest scopes it to the machine), so a repository cannot name the binary
// that is run over its own files.

const path = require('node:path');
const fs = require('node:fs');

const NAME = 'rohyctl';

/**
 * @typedef {object} SearchOptions
 * @property {string} platform process.platform
 * @property {string | undefined} pathVar the PATH variable
 * @property {(file: string) => boolean} isExecutable
 */

/** @param {string} platform */
function pathFor(platform) {
  return platform === 'win32' ? path.win32 : path.posix;
}

/**
 * findOnPath returns the first absolute PATH entry holding rohyctl, or null.
 * @param {SearchOptions} options
 */
function findOnPath({ platform, pathVar, isExecutable }) {
  const p = pathFor(platform);
  const windows = platform === 'win32';
  // Only the .exe on Windows: Node will not run a .cmd or .bat without a shell.
  const file = windows ? `${NAME}.exe` : NAME;
  for (const raw of String(pathVar ?? '').split(windows ? ';' : ':')) {
    const dir = windows ? raw.trim().replace(/^"(.*)"$/, '$1') : raw;
    // An empty entry means the current directory on POSIX; a relative one is relative to it.
    if (dir === '' || !p.isAbsolute(dir) || (windows && /^[a-z]:(?![\\/])/i.test(dir))) continue;
    const candidate = p.join(dir, file);
    if (isExecutable(candidate)) return candidate;
  }
  return null;
}

/**
 * resolveRohyctl decides which rohyctl to run, or says why there is none.
 * @param {SearchOptions & {configured: string | undefined}} options
 * @returns {{exe: string, from: 'setting' | 'PATH'} | {reason: string}}
 */
function resolveRohyctl({ configured, platform, pathVar, isExecutable }) {
  const setting = String(configured ?? '').trim();
  if (setting !== '') {
    if (!pathFor(platform).isAbsolute(setting)) {
      return { reason: `rohy.rohyctlPath must be an absolute path; "${setting}" is not` };
    }
    if (!isExecutable(setting)) {
      return { reason: `rohy.rohyctlPath is "${setting}", which is not an executable file` };
    }
    return { exe: setting, from: 'setting' };
  }
  const found = findOnPath({ platform, pathVar, isExecutable });
  if (found === null) return { reason: `${NAME} was not found on PATH` };
  return { exe: found, from: 'PATH' };
}

/**
 * isExecutable is the real filesystem check: a regular file, and on POSIX one this user may run.
 * @param {string} file
 */
function isExecutable(file) {
  try {
    if (!fs.statSync(file).isFile()) return false;
    if (process.platform !== 'win32') fs.accessSync(file, fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

module.exports = { findOnPath, resolveRohyctl, isExecutable };
