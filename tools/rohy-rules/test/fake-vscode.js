'use strict';

// The slice of the VS Code API the controller uses, faked closely enough to drive it: documents
// that can be opened, edited and closed; settings that can change; a diagnostics collection,
// a status bar item and a formatter registry that can be read back.
//
// It is a fake, so it can be wrong about VS Code. What it pins is the controller's own logic —
// which document is checked, when, and which answer is kept. The real API is exercised by hand
// (tools/rohy-rules/README.md, "Checked by hand").

/** A glob as VS Code's DocumentFilter reads one, over a path with forward slashes. */
function globToRegExp(glob) {
  let re = '';
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === '*' && glob[i + 1] === '*') {
      i++;
      if (glob[i + 1] === '/') {
        i++;
        re += '(?:.*/)?';
      } else {
        re += '.*';
      }
    } else if (c === '*') re += '[^/]*';
    else if (c === '?') re += '[^/]';
    else re += c.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(`^${re}$`, 'i');
}

function createFakeVscode(initialSettings = {}) {
  const listeners = { open: [], change: [], close: [], config: [], active: [] };
  const on = (name) => (fn) => {
    listeners[name].push(fn);
    return { dispose: () => listeners[name].splice(listeners[name].indexOf(fn), 1) };
  };
  const fire = (name, value) => [...listeners[name]].forEach((fn) => fn(value));

  let settings = { ...initialSettings };
  const diagnostics = new Map();
  const formatters = [];
  const commands = new Map();
  const messages = { warnings: [], errors: [], log: [] };
  const status = { text: '', tooltip: '', visible: false, command: undefined };

  class Position {
    constructor(line, character) {
      this.line = line;
      this.character = character;
    }
  }
  class Range {
    constructor(start, end) {
      this.start = start;
      this.end = end;
    }
  }
  class Diagnostic {
    constructor(range, message, severity) {
      this.range = range;
      this.message = message;
      this.severity = severity;
    }
  }

  const vscode = {
    Position,
    Range,
    Diagnostic,
    DiagnosticSeverity: { Error: 0, Warning: 1 },
    StatusBarAlignment: { Left: 1, Right: 2 },
    TextEdit: { replace: (range, newText) => ({ range, newText }) },
    languages: {
      createDiagnosticCollection: () => ({
        set: (uri, list) => diagnostics.set(uri.toString(), list),
        delete: (uri) => diagnostics.delete(uri.toString()),
        clear: () => diagnostics.clear(),
        dispose: () => diagnostics.clear(),
      }),
      match: (filter, doc) => (globToRegExp(filter.pattern).test(doc.uri.fsPath.replace(/\\/g, '/')) ? 10 : 0),
      registerDocumentFormattingEditProvider: (selector, provider) => {
        const entry = { selector, provider, disposed: false };
        formatters.push(entry);
        return { dispose: () => (entry.disposed = true) };
      },
    },
    window: {
      activeTextEditor: undefined,
      createOutputChannel: () => ({ appendLine: (l) => messages.log.push(l), show() {}, dispose() {} }),
      createStatusBarItem: () =>
        Object.assign(status, {
          show: () => (status.visible = true),
          hide: () => (status.visible = false),
          dispose() {},
        }),
      onDidChangeActiveTextEditor: on('active'),
      showWarningMessage: async (m) => messages.warnings.push(m),
      showErrorMessage: async (m) => messages.errors.push(m),
    },
    workspace: {
      textDocuments: [],
      getConfiguration: (section) => ({ get: (k) => settings[`${section}.${k}`] }),
      onDidOpenTextDocument: on('open'),
      onDidChangeTextDocument: on('change'),
      onDidCloseTextDocument: on('close'),
      onDidChangeConfiguration: on('config'),
    },
    commands: {
      registerCommand: (id, fn) => {
        commands.set(id, fn);
        return { dispose: () => commands.delete(id) };
      },
    },
  };

  /** A document whose positionAt breaks lines the way VS Code does: \r\n, \n and a lone \r. */
  function makeDocument(fsPath, text) {
    const doc = {
      uri: { fsPath, scheme: 'file', toString: () => `file:///${fsPath.replace(/\\/g, '/')}` },
      languageId: fsPath.endsWith('.json') ? 'json' : 'plaintext',
      version: 1,
      isClosed: false,
      text,
      getText: () => doc.text,
      positionAt(offset) {
        let line = 0;
        let start = 0;
        for (let i = 0; i < offset; i++) {
          const c = doc.text[i];
          if (c === '\n' || (c === '\r' && doc.text[i + 1] !== '\n')) {
            line++;
            start = i + 1;
          }
        }
        return new Position(line, offset - start);
      },
    };
    return doc;
  }

  return {
    vscode,
    diagnostics,
    formatters,
    commands,
    messages,
    status,
    open(fsPath, text) {
      const doc = makeDocument(fsPath, text);
      vscode.workspace.textDocuments.push(doc);
      fire('open', doc);
      return doc;
    },
    edit(doc, text) {
      doc.text = text;
      doc.version++;
      fire('change', { document: doc });
    },
    close(doc) {
      doc.isClosed = true;
      vscode.workspace.textDocuments.splice(vscode.workspace.textDocuments.indexOf(doc), 1);
      fire('close', doc);
    },
    activate(doc) {
      vscode.window.activeTextEditor = doc ? { document: doc } : undefined;
      fire('active', vscode.window.activeTextEditor);
    },
    configure(changes) {
      settings = { ...settings, ...changes };
      fire('config', { affectsConfiguration: (s) => Object.keys(changes).some((k) => k.startsWith(s)) });
    },
  };
}

module.exports = { createFakeVscode, globToRegExp };
