// @ts-check
'use strict';

// Which files are rule files.
//
// The default patterns are the manifest's own: the fileMatch VS Code's JSON support uses to
// attach the schema. Reading them from there keeps one list, so the files the schema covers
// and the files rohyctl checks cannot drift apart. `rohy.ruleFiles` adds to them for rohyctl
// only — the schema association is static in the manifest and nothing at run time can extend it.

const path = require('node:path');

/** The app keeps its rules' on/off toggles beside them, under this name. It is not a rule. */
const STATE_FILE = 'rules-state.json';

/**
 * defaultPatterns returns the positive fileMatch entries of the schema association.
 * @param {any} manifest package.json
 * @returns {string[]}
 */
function defaultPatterns(manifest) {
  const associations = manifest?.contributes?.jsonValidation ?? [];
  return associations
    .flatMap((/** @type {{fileMatch?: string | string[]}} */ a) => [a.fileMatch ?? []].flat())
    .filter((/** @type {unknown} */ p) => typeof p === 'string' && !p.startsWith('!'));
}

/**
 * normalizePattern makes a setting's pattern match the absolute path VS Code matches it
 * against: forward slashes, and `**` in front of a pattern that names no root, so that
 * `rules/*.json` means a rules folder anywhere rather than nothing at all.
 * @param {string} pattern
 */
function normalizePattern(pattern) {
  const p = pattern.trim().replace(/\\/g, '/');
  if (p === '') return '';
  // VS Code spells a Windows drive in lower case (c:\…); a pattern written C:/… would miss.
  if (/^[a-z]:\//i.test(p)) return p[0].toLowerCase() + p.slice(1);
  if (p.startsWith('/') || p.startsWith('**/')) return p;
  return `**/${p.replace(/^\.\//, '')}`;
}

/**
 * rulePatterns is every pattern a rule file is recognised by.
 * @param {any} manifest
 * @param {unknown} extra the rohy.ruleFiles setting
 * @returns {string[]}
 */
function rulePatterns(manifest, extra) {
  const added = Array.isArray(extra) ? extra.filter((p) => typeof p === 'string').map(normalizePattern) : [];
  return [...new Set([...defaultPatterns(manifest), ...added.filter((p) => p !== '')])];
}

/** isStateFile reports whether a path is the rules directory's toggle file. */
function isStateFile(/** @type {string} */ fsPath) {
  return path.basename(fsPath.replace(/\\/g, '/')) === STATE_FILE;
}

module.exports = { STATE_FILE, defaultPatterns, normalizePattern, rulePatterns, isStateFile };
