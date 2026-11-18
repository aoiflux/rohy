// @ts-check
'use strict';

// Running rohyctl. The text always goes on stdin and never on the command line, so nothing
// the author typed can become an argument, and the file on disk is never what is checked:
// the buffer is, saved or not.
//
// Every run is bounded — by time, by output size, and by cancellation — because the program
// on the other end is whichever rohyctl the machine has, and a check that never returns
// would leave stale problems on screen.

const { spawn } = require('node:child_process');
const { parseReport, ReportError } = require('./report.js');

const DEFAULT_TIMEOUT_MS = 10_000;
/** rohyctl refuses a rule over 1 MiB; a report on one is a fraction of that. */
const DEFAULT_MAX_OUTPUT_BYTES = 8 * 1024 * 1024;

class RunError extends Error {
  /**
   * @param {'spawn' | 'timeout' | 'too-large' | 'cancelled' | 'failed'} kind
   * @param {string} message
   */
  constructor(kind, message) {
    super(message);
    this.name = 'RunError';
    this.kind = kind;
  }
}

/**
 * @typedef {{code: number, stdout: string, stderr: string}} RunResult
 * @typedef {{timeoutMs?: number, maxOutputBytes?: number, signal?: AbortSignal}} RunOptions
 */

/**
 * runProcess runs exe with args, writes input to its stdin, and collects its output.
 * @param {string} exe
 * @param {string[]} args
 * @param {string} input
 * @param {RunOptions} options
 * @returns {Promise<RunResult>}
 */
function runProcess(exe, args, input, options) {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const maxBytes = options.maxOutputBytes ?? DEFAULT_MAX_OUTPUT_BYTES;
  const { signal } = options;

  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new RunError('cancelled', 'cancelled'));
      return;
    }
    // No shell: the arguments are fixed, and a shell would add a second parser to get wrong.
    const child = spawn(exe, args, { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] });
    /** @type {RunError | null} */
    let failure = null;
    let settled = false;
    const out = { stdout: /** @type {Buffer[]} */ ([]), stderr: /** @type {Buffer[]} */ ([]), bytes: 0 };

    const stop = (/** @type {RunError} */ err) => {
      if (failure) return;
      failure = err;
      child.kill();
    };
    const timer = setTimeout(() => stop(new RunError('timeout', `rohyctl did not finish in ${timeoutMs} ms`)), timeoutMs);
    const onAbort = () => stop(new RunError('cancelled', 'cancelled'));
    signal?.addEventListener('abort', onAbort, { once: true });

    const finish = (/** @type {() => void} */ settle) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal?.removeEventListener('abort', onAbort);
      settle();
    };

    for (const stream of /** @type {const} */ (['stdout', 'stderr'])) {
      child[stream].on('data', (/** @type {Buffer} */ chunk) => {
        out.bytes += chunk.length;
        if (out.bytes > maxBytes) {
          stop(new RunError('too-large', `rohyctl printed more than ${maxBytes} bytes`));
          return;
        }
        out[stream].push(chunk);
      });
    }

    child.on('error', (err) => {
      // The program could not be started at all: missing, not executable, not a program.
      finish(() => reject(failure ?? new RunError('spawn', `could not run ${exe}: ${err.message}`)));
    });
    child.on('close', (code) => {
      finish(() => {
        if (failure) return reject(failure);
        if (code === null) return reject(new RunError('failed', 'rohyctl was stopped before it finished'));
        resolve({
          code,
          stdout: Buffer.concat(out.stdout).toString('utf8'),
          stderr: Buffer.concat(out.stderr).toString('utf8'),
        });
      });
    });

    // A rohyctl that exits before reading everything (a usage error, say) closes its end of
    // the pipe; the write then fails with EPIPE. That is not the failure — the exit code is.
    child.stdin.on('error', () => {});
    child.stdin.end(input, 'utf8');
  });
}

/** The first line rohyctl wrote, for a message. */
function firstLine(/** @type {string} */ text) {
  return text.split(/\r?\n/, 1)[0].trim();
}

/**
 * createClient binds the commands the extension uses to one rohyctl.
 * @param {{exe: string, prefixArgs?: string[], run?: typeof runProcess, timeoutMs?: number, maxOutputBytes?: number}} options
 */
function createClient({ exe, prefixArgs = [], run = runProcess, timeoutMs, maxOutputBytes }) {
  /** @param {string[]} args @param {string} input @param {{signal?: AbortSignal}} [extra] */
  const call = (args, input, extra = {}) =>
    run(exe, [...prefixArgs, ...args], input, { timeoutMs, maxOutputBytes, signal: extra.signal });

  /** A run that rohyctl itself says could not happen: exit 2, or a code it never uses. */
  const failed = (/** @type {RunResult} */ r) => {
    const said = firstLine(r.stderr);
    if (/flag provided but not defined: -json/.test(r.stderr)) {
      return new RunError('failed', 'this rohyctl is older than this extension: it has no validate --json');
    }
    return new RunError('failed', said || `rohyctl exited with ${r.code}`);
  };

  return {
    exe,

    /**
     * validate checks text as one rule file and returns its report.
     * @param {string} text
     * @param {{signal?: AbortSignal}} [extra]
     * @returns {Promise<import('./report.js').FileReport>}
     */
    async validate(text, extra) {
      const r = await call(['validate', '--json'], text, extra);
      if (r.code !== 0 && r.code !== 1) throw failed(r);
      const report = parseReport(r.stdout);
      if (report.files.length !== 1) throw new ReportError(`one file was sent and ${report.files.length} came back`);
      // 0 means every file loaded and 1 that one did not. A report that says otherwise came
      // from a rohyctl this extension does not understand.
      if ((r.code === 1) !== (report.failed > 0)) throw new ReportError('the exit code and the report disagree');
      return report.files[0];
    },

    /**
     * format returns the text in canonical form, or why it has none (it does not parse).
     * @param {string} text
     * @param {{signal?: AbortSignal}} [extra]
     * @returns {Promise<{ok: true, text: string} | {ok: false, reason: string}>}
     */
    async format(text, extra) {
      const r = await call(['format'], text, extra);
      if (r.code === 0) return { ok: true, text: r.stdout };
      if (r.code === 1) return { ok: false, reason: firstLine(r.stderr).replace(/^<stdin>: cannot format: /, '') };
      throw failed(r);
    },

    /** version returns the build identity rohyctl prints. */
    async version() {
      const r = await call(['version'], '');
      if (r.code !== 0) throw failed(r);
      return firstLine(r.stdout);
    },
  };
}

module.exports = { createClient, runProcess, RunError };
