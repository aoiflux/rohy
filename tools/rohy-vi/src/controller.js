// @ts-check
'use strict';

// The extension's wiring: which documents are rules, when they are checked, where the
// problems go, and what the status bar says about how they were checked.
//
// The VS Code API is passed in rather than required, so the tests can drive this with a fake
// one. Everything with a decision in it that does not need the API is in the other modules.
//
// Two ways of checking, and the status bar always says which is in use:
// - rohyctl, when it is found: the loader's own check, run on the buffer as it is typed;
// - the schema otherwise, through VS Code's JSON support. It describes the shape of a rule and
//   cannot say anything that depends on the algorithm, so "no problems" means less there.

const { placeProblem } = require('./report.js');
const { RunError } = require('./rohyctl.js');
const { isStateFile, rulePatterns } = require('./rulefiles.js');

const DEBOUNCE_MS = 300;

/**
 * @typedef {ReturnType<typeof import('./rohyctl.js').createClient>} Client
 * @typedef {{exe: string, from: string} | {reason: string}} Resolution
 * @typedef {object} Deps
 * @property {any} manifest package.json, for the default rule-file patterns
 * @property {(configured: unknown) => Resolution} resolve finds rohyctl
 * @property {(options: {exe: string}) => Client} createClient
 * @property {number} [debounceMs]
 * @property {typeof setTimeout} [setTimer]
 * @property {typeof clearTimeout} [clearTimer]
 */

/**
 * @param {any} vscode the VS Code API
 * @param {Deps} deps
 */
function createController(vscode, deps) {
  const debounceMs = deps.debounceMs ?? DEBOUNCE_MS;
  const setTimer = deps.setTimer ?? setTimeout;
  const clearTimer = deps.clearTimer ?? clearTimeout;

  const diagnostics = vscode.languages.createDiagnosticCollection('rohy');
  const output = vscode.window.createOutputChannel('rohy');
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  status.command = 'rohy.showLog';
  /** @type {any[]} */
  const disposables = [diagnostics, output, status];
  /** @type {any} */
  let formatter = null;

  /** @type {{mode: 'starting'} | {mode: 'rohyctl', client: Client, version: string} | {mode: 'schema', reason: string}} */
  let state = { mode: 'starting' };
  /** @type {string[]} */
  let patterns = [];
  /** The last run's failure, per document, shown in the status bar until a run succeeds. */
  const lastFailure = new Map();
  /** Per document: the pending debounce timer, and the run in flight. */
  const timers = new Map();
  const inFlight = new Map();
  /** Discovery is numbered too: a slow probe must not overwrite a newer answer. */
  let discovery = 0;
  let lastWarned = '';

  const key = (/** @type {any} */ doc) => doc.uri.toString();
  const log = (/** @type {string} */ line) => output.appendLine(line);

  /** isRuleDocument reports whether a document is a rule file this extension checks. */
  function isRuleDocument(/** @type {any} */ doc) {
    if (doc.languageId !== 'json' || doc.uri.scheme !== 'file' || isStateFile(doc.uri.fsPath)) return false;
    return patterns.some((pattern) => vscode.languages.match({ pattern }, doc) > 0);
  }

  function readSettings() {
    const config = vscode.workspace.getConfiguration('rohy');
    return { rohyctlPath: config.get('rohyctlPath'), ruleFiles: config.get('ruleFiles') };
  }

  function updateStatus() {
    const doc = vscode.window.activeTextEditor?.document;
    if (!doc || !isRuleDocument(doc)) {
      status.hide();
      return;
    }
    const failure = lastFailure.get(key(doc));
    if (state.mode === 'rohyctl' && failure) {
      status.text = '$(warning) rohy';
      status.tooltip = `rohyctl could not check this file: ${failure}`;
    } else if (state.mode === 'rohyctl') {
      status.text = '$(check) rohy';
      status.tooltip = `Checked by ${state.client.exe} (${state.version}), the same check the application loads rules with.`;
    } else if (state.mode === 'schema') {
      status.text = '$(info) rohy: schema only';
      status.tooltip =
        `${state.reason}. Checking against the rule schema only: problems that depend on the ` +
        'algorithm are not shown, so a file with no problems may still not load.';
    } else {
      status.text = '$(sync~spin) rohy';
      status.tooltip = 'Looking for rohyctl';
    }
    status.show();
  }

  /** @param {any} doc @param {string} text @param {import('./report.js').FileReport} file */
  function toDiagnostics(doc, text, file) {
    const make = (/** @type {import('./report.js').Problem} */ p, /** @type {any} */ severity) => {
      const { start, end } = placeProblem(text, p);
      const range = new vscode.Range(doc.positionAt(start), doc.positionAt(end));
      const d = new vscode.Diagnostic(range, p.message, severity);
      d.source = 'rohy';
      d.code = p.code;
      return d;
    };
    return [
      ...file.errors.map((p) => make(p, vscode.DiagnosticSeverity.Error)),
      ...file.warnings.map((p) => make(p, vscode.DiagnosticSeverity.Warning)),
    ];
  }

  /** forget drops everything held for a document: its timer, its run, its problems. */
  function forget(/** @type {any} */ doc) {
    const k = key(doc);
    clearTimer(timers.get(k));
    timers.delete(k);
    inFlight.get(k)?.abort();
    inFlight.delete(k);
    lastFailure.delete(k);
    diagnostics.delete(doc.uri);
  }

  /** check runs rohyctl over a document's current text and shows what it found. */
  async function check(/** @type {any} */ doc) {
    if (state.mode !== 'rohyctl' || doc.isClosed) return;
    const k = key(doc);
    inFlight.get(k)?.abort();
    const run = new AbortController();
    inFlight.set(k, run);
    const { client } = state;
    const text = doc.getText();
    const version = doc.version;
    try {
      const file = await client.validate(text, { signal: run.signal });
      // A newer edit, a newer run, a closed document or another rohyctl: this answer is about
      // text that is no longer on screen.
      if (inFlight.get(k) !== run || doc.isClosed || doc.version !== version || state.mode !== 'rohyctl' || state.client !== client) return;
      lastFailure.delete(k);
      diagnostics.set(doc.uri, toDiagnostics(doc, text, file));
    } catch (e) {
      if (e instanceof RunError && e.kind === 'cancelled') return;
      if (inFlight.get(k) !== run) return;
      // Problems from an earlier text would now be in the wrong places, and keeping them
      // would claim a check that did not happen. The status bar says why there are none.
      diagnostics.delete(doc.uri);
      const message = /** @type {Error} */ (e).message;
      lastFailure.set(k, message);
      log(`rohyctl could not check ${doc.uri.fsPath}: ${message}`);
    } finally {
      if (inFlight.get(k) === run) inFlight.delete(k);
      updateStatus();
    }
  }

  /** schedule checks a document once typing pauses. */
  function schedule(/** @type {any} */ doc) {
    if (!isRuleDocument(doc)) return;
    const k = key(doc);
    clearTimer(timers.get(k));
    timers.set(
      k,
      setTimer(() => {
        timers.delete(k);
        void check(doc);
      }, debounceMs),
    );
  }

  function checkOpenDocuments() {
    for (const doc of vscode.workspace.textDocuments) {
      if (isRuleDocument(doc)) schedule(doc);
      else forget(doc);
    }
  }

  /** discover finds rohyctl and proves it speaks the report this extension reads. */
  async function discover() {
    const attempt = ++discovery;
    const settings = readSettings();
    const found = deps.resolve(settings.rohyctlPath);
    /** @type {typeof state} */
    let next;
    if ('reason' in found) {
      next = { mode: 'schema', reason: found.reason };
    } else {
      const client = deps.createClient({ exe: found.exe });
      try {
        // An empty object is never a valid rule, so a working rohyctl answers with a report
        // that has errors in it — and one that cannot, or answers in another shape, is found
        // out here rather than on the author's first keystroke.
        await client.validate('{}');
        const version = await client.version().catch(() => 'version unknown');
        next = { mode: 'rohyctl', client, version };
        log(`Checking rule files with ${found.exe} (${version}), found from ${found.from}.`);
      } catch (e) {
        next = { mode: 'schema', reason: `${found.exe} cannot be used: ${/** @type {Error} */ (e).message}` };
      }
    }
    if (attempt !== discovery) return;
    state = next;
    lastFailure.clear();
    if (state.mode === 'schema') {
      log(`${state.reason}. Checking rule files against the schema only.`);
      diagnostics.clear();
      // A setting the author wrote that does not work is worth interrupting them for, once.
      // rohyctl merely not being installed is not; the status bar says so.
      if (state.reason.includes('rohy.rohyctlPath') && state.reason !== lastWarned) {
        lastWarned = state.reason;
        void vscode.window.showWarningMessage(`rohy: ${state.reason}.`);
      }
    }
    updateStatus();
    checkOpenDocuments();
  }

  /** @param {any} doc @param {any} _options @param {any} token */
  async function provideFormatting(doc, _options, token) {
    if (!isRuleDocument(doc)) return [];
    if (state.mode !== 'rohyctl') {
      const why = state.mode === 'schema' ? state.reason : 'rohyctl is still being looked for';
      void vscode.window.showWarningMessage(`rohy: cannot format without rohyctl — ${why}.`);
      return [];
    }
    const run = new AbortController();
    const subscription = token?.onCancellationRequested?.(() => run.abort());
    const text = doc.getText();
    try {
      const result = await state.client.format(text, { signal: run.signal });
      if (token?.isCancellationRequested) return [];
      if (!result.ok) {
        void vscode.window.showWarningMessage(`rohy: this file cannot be formatted: ${result.reason}`);
        return [];
      }
      if (result.text === text) return [];
      const whole = new vscode.Range(doc.positionAt(0), doc.positionAt(text.length));
      return [vscode.TextEdit.replace(whole, result.text)];
    } catch (e) {
      if (e instanceof RunError && e.kind === 'cancelled') return [];
      const message = /** @type {Error} */ (e).message;
      log(`rohyctl could not format ${doc.uri.fsPath}: ${message}`);
      void vscode.window.showErrorMessage(`rohy: rohyctl could not format this file: ${message}`);
      return [];
    } finally {
      subscription?.dispose?.();
    }
  }

  /** applyPatterns reads the rule-file patterns and registers the formatter for them. */
  function applyPatterns() {
    patterns = rulePatterns(deps.manifest, readSettings().ruleFiles);
    formatter?.dispose();
    const selector = patterns.map((pattern) => ({ language: 'json', scheme: 'file', pattern }));
    formatter = vscode.languages.registerDocumentFormattingEditProvider(selector, {
      provideDocumentFormattingEdits: provideFormatting,
    });
  }

  return {
    /** start wires the events and looks for rohyctl. The returned promise is for tests. */
    start() {
      applyPatterns();
      disposables.push(
        vscode.commands.registerCommand('rohy.showLog', () => output.show()),
        vscode.workspace.onDidOpenTextDocument(schedule),
        vscode.workspace.onDidChangeTextDocument((/** @type {any} */ e) => schedule(e.document)),
        vscode.workspace.onDidCloseTextDocument(forget),
        vscode.window.onDidChangeActiveTextEditor(updateStatus),
        vscode.workspace.onDidChangeConfiguration((/** @type {any} */ e) => {
          if (!e.affectsConfiguration('rohy')) return;
          applyPatterns();
          void discover();
        }),
      );
      updateStatus();
      return discover();
    },

    dispose() {
      for (const k of timers.keys()) clearTimer(timers.get(k));
      for (const run of inFlight.values()) run.abort();
      formatter?.dispose();
      for (const d of disposables) d.dispose();
    },
  };
}

module.exports = { createController, DEBOUNCE_MS };
