// @ts-check
'use strict';

// The entry point VS Code loads. It only connects the real API, the real filesystem, the
// packaged rohyctl and the real PATH to the controller; everything it connects is tested on its
// own.

const vscode = require('vscode');
const { createController } = require('./controller.js');
const { resolveRohyctl, isExecutable, bundledRohyctl, restoreExecutableBit } = require('./locate.js');
const { createClient } = require('./rohyctl.js');

/** @param {any} context vscode.ExtensionContext */
function activate(context) {
  const bundled = bundledRohyctl(context.extensionPath, process.platform, process.arch);
  restoreExecutableBit(bundled);
  const controller = createController(vscode, {
    manifest: context.extension.packageJSON,
    resolve: (configured) =>
      resolveRohyctl({ configured, bundled, platform: process.platform, pathVar: process.env.PATH, isExecutable }),
    createClient,
  });
  context.subscriptions.push(controller);
  void controller.start();
}

function deactivate() {}

module.exports = { activate, deactivate };
