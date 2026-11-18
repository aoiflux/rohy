'use strict';

// Builds the extension's .vsix, the file that is published and installed.
//
// Run from tools/rohy-rules with `npm run package`. It checks before it builds, and stops at the
// first failure:
//   1. the version is one the Marketplace accepts (plain x.y.z; it has no pre-release labels);
//   2. the bundled rule schema is what tools/genschema generates now, because the schema-only check
//      the extension falls back to is only as right as that file;
//   3. the extension's own tests pass.
// Then it packages with a pinned vsce and prints the package's SHA-256.
//
// It never publishes. Publishing needs the publisher's token, and a script that reads a token is
// one more place a token can leak from; the command to run is printed at the end instead.

const { spawnSync } = require('node:child_process');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const EXT = path.join(__dirname, '..');
const REPO = path.join(EXT, '..', '..');

// Pinned, so a package built today and one built next month come from the same packager.
const VSCE = '@vscode/vsce@4.1.0';

// Links in the README are rewritten against this branch of the repository.
const LINK_REF = 'main';

/**
 * plan works out what to build from the manifest, and refuses a manifest that cannot be published.
 * @param {{name: string, version: string, repository: {url: string, directory: string}}} manifest
 */
function plan(manifest) {
  if (!/^\d+\.\d+\.\d+$/.test(manifest.version ?? '')) {
    throw new Error(`version ${JSON.stringify(manifest.version)} is not x.y.z, which the Marketplace requires`);
  }
  const { url, directory } = manifest.repository;
  // vsce guesses the link base from the repository URL alone, so a link such as ../../README.md
  // would resolve above the repository root. The base must name the extension's directory.
  const tree = (kind) => `${url}/${kind}/${LINK_REF}/${directory}`;
  const out = `${manifest.name}-${manifest.version}.vsix`;
  return {
    out,
    vsceArgs: [
      '--yes', VSCE, 'package',
      // The extension has no dependencies; this keeps vsce from running npm to look for them.
      '--no-dependencies',
      '--baseContentUrl', tree('blob'),
      '--baseImagesUrl', tree('raw'),
      '--out', out,
    ],
  };
}

function run(label, cmd, args, cwd, shell = false) {
  console.log(`\n== ${label}`);
  // Through a shell the command is one string; handing Node separate arguments as well only
  // gets them joined the same way, with a deprecation warning.
  const r = shell
    ? spawnSync([cmd, ...args].join(' '), { cwd, stdio: 'inherit', shell })
    : spawnSync(cmd, args, { cwd, stdio: 'inherit' });
  if (r.error) throw new Error(`${label}: ${r.error.message}`);
  if (r.status !== 0) throw new Error(`${label}: exited ${r.status}`);
}

function main() {
  const manifest = JSON.parse(fs.readFileSync(path.join(EXT, 'package.json'), 'utf8'));
  const p = plan(manifest);

  const dirty = spawnSync('git', ['status', '--porcelain', '--', '.'], { cwd: EXT, encoding: 'utf8' });
  if (dirty.status === 0 && dirty.stdout.trim() !== '') {
    console.warn('warning: tools/rohy-rules has uncommitted changes; this package matches no commit');
  }

  run('rule schema is current', 'go', ['run', './tools/genschema', '-check'], REPO);
  run('extension tests', process.execPath, ['--test', 'test/*.test.js'], EXT);
  // npx is a .cmd on Windows, which Node starts only through a shell. Every argument is fixed
  // here or checked by plan(), and none holds a space or a shell character.
  run(`package with ${VSCE}`, 'npx', p.vsceArgs, EXT, process.platform === 'win32');

  const file = path.join(EXT, p.out);
  const sum = crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
  console.log(`\n${p.out}\nsha256 ${sum}`);
  console.log(`\nTo publish, from tools/rohy-rules (vsce asks for the publisher's token):`);
  console.log(`  npx ${VSCE} publish --packagePath ${p.out}`);
}

if (require.main === module) {
  try {
    main();
  } catch (err) {
    console.error(`\npackage failed: ${err.message}`);
    process.exit(1);
  }
}

module.exports = { plan };
