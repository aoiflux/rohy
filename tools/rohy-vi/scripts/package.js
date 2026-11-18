'use strict';

// Builds the extension's .vsix, the file that is published and installed. The package carries a
// rohyctl for every platform below, so the extension checks rules with the real loader without
// anyone installing rohyctl first; at run time it picks the one for its machine.
//
// Run it through build-extension.ps1 or build-extension.sh at the repository root, or from
// tools/rohy-vi with `npm run package`. It checks before it builds, and stops at the first failure:
//   1. the extension's version, and rohy's (build/config.yml), are plain x.y.z; the Marketplace
//      takes no pre-release labels;
//   2. the bundled rule schema is what tools/genschema generates now, because the schema-only check
//      the extension falls back to is only as right as that file;
//   3. the extension's own tests pass.
// Then rohyctl is cross-built (pure Go, no cgo) into bin/<platform>-<arch>/ for each platform,
// stamped with rohy's version and commit; the one built for this machine is run; a pinned vsce
// packages the folder; the package is checked to hold every rohyctl, and its SHA-256 is printed.
//
// It never publishes. Publishing needs the publisher's token, and a script that reads a token is
// one more place a token can leak from; the command to run is printed at the end instead.

const { spawnSync } = require('node:child_process');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const EXT = path.join(__dirname, '..');
const REPO = path.join(EXT, '..', '..');
const BIN = path.join(EXT, 'bin');

// Pinned, so a package built today and one built next month come from the same packager.
const VSCE = '@vscode/vsce@4.1.0';

// Links in the README are rewritten against this branch of the repository.
const LINK_REF = 'main';

// Keyed as Node names them (process.platform-process.arch), which is how the extension looks its
// rohyctl up. rohyctl has no cgo, so each is a static cross-build; the Linux ones run on musl too.
const PLATFORMS = {
  'win32-x64': { GOOS: 'windows', GOARCH: 'amd64' },
  'win32-arm64': { GOOS: 'windows', GOARCH: 'arm64' },
  'linux-x64': { GOOS: 'linux', GOARCH: 'amd64' },
  'linux-arm64': { GOOS: 'linux', GOARCH: 'arm64' },
  'darwin-x64': { GOOS: 'darwin', GOARCH: 'amd64' },
  'darwin-arm64': { GOOS: 'darwin', GOARCH: 'arm64' },
};

const SEMVER = /^\d+\.\d+\.\d+$/;

/**
 * plan works out what to build, and refuses anything that cannot be published.
 * @param {{name: string, version: string, repository: {url: string, directory: string}}} manifest
 * @param {{version: string, commit: string, date: string}} rohy what the bundled rohyctl reports
 */
function plan(manifest, rohy) {
  if (!SEMVER.test(manifest.version ?? '')) {
    throw new Error(`version ${JSON.stringify(manifest.version)} is not x.y.z, which the Marketplace requires`);
  }
  if (!SEMVER.test(rohy.version ?? '')) {
    throw new Error(`rohy version ${JSON.stringify(rohy.version)} in build/config.yml is not x.y.z`);
  }
  // These go into a linker flag; nothing but a short revision and a timestamp belongs there.
  if (!/^[0-9a-f]{7,40}(-dirty)?$/.test(rohy.commit ?? '')) throw new Error(`commit ${JSON.stringify(rohy.commit)} is not a git revision`);
  if (!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/.test(rohy.date ?? '')) throw new Error(`date ${JSON.stringify(rohy.date)} is not RFC 3339 UTC`);

  const { url, directory } = manifest.repository;
  // vsce guesses the link base from the repository URL alone, so a link such as ../../README.md
  // would resolve above the repository root. The base must name the extension's directory.
  const tree = (kind) => `${url}/${kind}/${LINK_REF}/${directory}`;
  const ldflags = [
    '-s', '-w',
    `-X rohy/backend/version.Version=${rohy.version}`,
    `-X rohy/backend/version.Commit=${rohy.commit}`,
    `-X rohy/backend/version.Date=${rohy.date}`,
  ].join(' ');
  const out = `${manifest.name}-${manifest.version}.vsix`;

  return {
    out,
    rohyctl: Object.entries(PLATFORMS).map(([platform, goEnv]) => {
      // Where it sits inside the extension, with forward slashes, as a zip names it.
      const file = `bin/${platform}/${goEnv.GOOS === 'windows' ? 'rohyctl.exe' : 'rohyctl'}`;
      return {
        platform,
        file,
        goEnv: { CGO_ENABLED: '0', ...goEnv },
        // Run from the repository root.
        goArgs: ['build', '-trimpath', '-ldflags', ldflags, '-o', `${directory}/${file}`, './cmd/rohyctl'],
      };
    }),
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

function run(label, cmd, args, cwd, { shell = false, env } = {}) {
  console.log(`\n== ${label}`);
  // Through a shell the command is one string; handing Node separate arguments as well only
  // gets them joined the same way, with a deprecation warning.
  const opts = { cwd, stdio: 'inherit', env: env ? { ...process.env, ...env } : process.env };
  const r = shell ? spawnSync([cmd, ...args].join(' '), { ...opts, shell }) : spawnSync(cmd, args, opts);
  if (r.error) throw new Error(`${label}: ${r.error.message}`);
  if (r.status !== 0) throw new Error(`${label}: exited ${r.status}`);
}

function git(...args) {
  const r = spawnSync('git', args, { cwd: REPO, encoding: 'utf8' });
  if (r.status !== 0) throw new Error(`git ${args.join(' ')}: ${r.stderr.trim()}`);
  return r.stdout.trim();
}

/** rohyVersion reads the version build/config.yml carries for rohy's own packages. */
function rohyVersion() {
  const config = fs.readFileSync(path.join(REPO, 'build', 'config.yml'), 'utf8');
  const m = /^info:\r?\n(?:[ \t].*\r?\n)*?[ \t]+version:\s*"([^"]*)"/m.exec(config);
  if (!m) throw new Error('build/config.yml has no info.version');
  return m[1];
}

function main() {
  const manifest = JSON.parse(fs.readFileSync(path.join(EXT, 'package.json'), 'utf8'));
  // rohyctl is built from the whole repository, so the whole of it decides whether it is clean.
  const dirty = git('status', '--porcelain') !== '';
  const rohy = {
    version: rohyVersion(),
    commit: git('rev-parse', '--short', 'HEAD') + (dirty ? '-dirty' : ''),
    // The commit's time, not the build's, so the same commit stamps the same date.
    date: new Date(Number(git('log', '-1', '--format=%ct')) * 1000).toISOString().replace(/\.\d+Z$/, 'Z'),
  };
  const p = plan(manifest, rohy);
  if (dirty) console.warn('warning: the repository has uncommitted changes; this package matches no commit');

  run('rule schema is current', 'go', ['run', './tools/genschema', '-check'], REPO);
  run('extension tests', process.execPath, ['--test', 'test/*.test.js'], EXT);

  try {
    // bin/ starts empty, so the package holds exactly the rohyctl builds made here.
    fs.rmSync(BIN, { recursive: true, force: true });
    for (const b of p.rohyctl) {
      run(`rohyctl for ${b.platform}`, 'go', b.goArgs, REPO, { env: b.goEnv });
    }
    const native = p.rohyctl.find((b) => b.platform === `${process.platform}-${process.arch}`);
    if (native) {
      const v = spawnSync(path.join(EXT, native.file), ['version'], { encoding: 'utf8' });
      if (v.status !== 0 || !v.stdout.startsWith(`rohyctl ${rohy.version} ${rohy.commit} `)) {
        throw new Error(`the rohyctl built for this machine reports ${JSON.stringify(v.stdout.trim())}`);
      }
    }
    // npx is a .cmd on Windows, which Node starts only through a shell. Every argument is fixed
    // here or checked by plan(), and none holds a space or a shell character.
    run(`package with ${VSCE}`, 'npx', p.vsceArgs, EXT, { shell: process.platform === 'win32' });
  } finally {
    fs.rmSync(BIN, { recursive: true, force: true });
  }

  const vsix = fs.readFileSync(path.join(EXT, p.out));
  // A zip stores its entry names as they are, so a name is in the file if its entry is.
  const missing = p.rohyctl.filter((b) => !vsix.includes(Buffer.from(`extension/${b.file}`)));
  if (missing.length > 0) {
    throw new Error(`${p.out} does not hold ${missing.map((b) => b.file).join(', ')}; check .vscodeignore`);
  }
  const sum = crypto.createHash('sha256').update(vsix).digest('hex');
  console.log(`\n${p.out}\nsha256 ${sum}`);
  console.log(`rohyctl ${rohy.version} ${rohy.commit} ${rohy.date} for ${p.rohyctl.map((b) => b.platform).join(', ')}`);
  console.log(`\nTo publish, from tools/rohy-vi (vsce asks for the publisher's token):`);
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

module.exports = { plan, PLATFORMS };
