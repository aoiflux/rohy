'use strict';

// A stand-in for rohyctl, run as a real child process so the runner's pipes, exit codes,
// timeouts and caps are exercised for real. The first argument picks the behaviour; the rest
// are what the runner passed, as rohyctl would receive them.

const fs = require('node:fs');
const path = require('node:path');

// A mode may carry one argument after a colon: report:syntax.json.
const [spec, ...args] = process.argv.slice(2);
const [mode, modeArg] = spec.split(':');

function readStdin() {
  return fs.readFileSync(0, 'utf8');
}

switch (mode) {
  case 'report': {
    // Prints the pinned report for a fixture, with the exit code rohyctl gives it.
    readStdin();
    const out = fs.readFileSync(path.join(__dirname, 'fixtures', 'reports', modeArg), 'utf8');
    process.stdout.write(out);
    process.exitCode = JSON.parse(out).failed > 0 ? 1 : 0;
    break;
  }
  case 'args':
    // Echoes what it was asked, to check the runner sends exactly the arguments it means to.
    readStdin();
    process.stdout.write(JSON.stringify(args));
    break;
  case 'echo':
    process.stdout.write(readStdin());
    break;
  case 'old':
    // A rohyctl from before validate --json.
    process.stderr.write('flag provided but not defined: -json\nUsage of validate:\n');
    process.exitCode = 2;
    break;
  case 'cannot-format':
    readStdin();
    process.stderr.write('<stdin>: cannot format: invalid character \'}\' looking for beginning of value\n');
    process.exitCode = 1;
    break;
  case 'garbage':
    readStdin();
    process.stdout.write('checked 1 file, all valid\n');
    break;
  case 'lies': {
    // A failing report under a success exit code.
    readStdin();
    process.stdout.write(fs.readFileSync(path.join(__dirname, 'fixtures', 'reports', 'syntax.json'), 'utf8'));
    break;
  }
  case 'hang':
    setInterval(() => {}, 1000);
    break;
  case 'flood':
    readStdin();
    process.stdout.write('x'.repeat(20 * 1024 * 1024));
    break;
  case 'version':
    process.stdout.write('rohyctl 0.3.0 abc1234 2026-11-07\n');
    break;
  default:
    process.stderr.write(`fake-rohyctl: unknown mode ${mode}\n`);
    process.exitCode = 2;
}
