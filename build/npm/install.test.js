'use strict';

const path = require('path');
const test = require('node:test');
const assert = require('node:assert/strict');

const { assetName, binPath, welcomeText } = require('./install.js');

test('selects Windows release assets for both supported architectures', () => {
  assert.equal(assetName('win32', 'x64'), 'wecom-calendar-cli-windows-amd64.exe');
  assert.equal(assetName('win32', 'arm64'), 'wecom-calendar-cli-windows-arm64.exe');
});

test('selects darwin and linux release assets', () => {
  assert.equal(assetName('darwin', 'arm64'), 'wecom-calendar-cli-darwin-arm64');
  assert.equal(assetName('linux', 'x64'), 'wecom-calendar-cli-linux-amd64');
});

test('uses an exe launcher target on Windows', () => {
  assert.equal(path.basename(binPath('win32').file), 'wecom-calendar-cli.exe');
});

test('rejects unsupported Windows architectures', () => {
  assert.throws(() => assetName('win32', 'ia32'), /unsupported platform win32\/ia32/);
});

test('welcome text recommends valid wecom-calendar commands', () => {
  assert.match(welcomeText(), /wecom-calendar-cli sync/);
  assert.match(welcomeText(), /wecom-calendar-cli event list/);
  assert.doesNotMatch(welcomeText(), /wecom-calendar-cli (issue|page)/);
});
