import assert from 'node:assert/strict';
import {readdir, readFile} from 'node:fs/promises';
import test from 'node:test';

const sourceDirectory = new URL('.', import.meta.url);

test('tracked frontend source does not import ignored Wails bindings', async () => {
  const entries = await readdir(sourceDirectory, {withFileTypes: true});
  const violations = [];

  for (const entry of entries) {
    if (!entry.isFile() || !/\.(?:js|mjs|ts|tsx)$/.test(entry.name)) continue;
    const source = await readFile(new URL(entry.name, sourceDirectory), 'utf8');
    if (/\b(?:from|import)\s*\(?\s*['"][^'"]*wailsjs\//.test(source)) violations.push(entry.name);
  }

  assert.deepEqual(violations, [], 'generated frontend/wailsjs modules are ignored by Git and unavailable in clean builds');
});

test('production frontend source does not publish private board addresses', async () => {
  const entries = await readdir(sourceDirectory, {withFileTypes: true});
  const violations = [];

  for (const entry of entries) {
    if (!entry.isFile() || !/\.(?:js|ts|tsx)$/.test(entry.name) || entry.name.endsWith('.test.mjs')) continue;
    const source = await readFile(new URL(entry.name, sourceDirectory), 'utf8');
    if (/\b10\.112\.\d{1,3}\.\d{1,3}\b/.test(source)) violations.push(entry.name);
  }

  assert.deepEqual(violations, [], 'board addresses must come from user configuration, not the shipped UI');
});

test('provider API keys remain transient and outside React state or browser storage', async () => {
  const source = await readFile(new URL('App.tsx', sourceDirectory), 'utf8');
  const providerDialog = source.slice(source.indexOf('function ProviderDialog('), source.indexOf('function ExtensionCenterDialog('));

  assert.match(providerDialog, /ref=\{apiKeyRef\}\s+type="password"\s+autoComplete="new-password"/);
  assert.match(providerDialog, /api\.addProvider\(boardId, form, apiKey\)/);
  assert.match(providerDialog, /api\.rotateProvider\(boardId, rotateTarget\.id, apiKey, confirmSharedRotation\)/);
  assert.doesNotMatch(providerDialog, /useState<[^>]*>\([^)]*apiKey|setApiKey|localStorage|sessionStorage/);
  assert.doesNotMatch(providerDialog, /\.addProvider\([^)]*apiKeyRef\.current/);
  assert.doesNotMatch(providerDialog, /\.rotateProvider\([^)]*apiKeyRef\.current/);
});

test('readiness dialog distinguishes unavailable diagnostics from active loading', async () => {
  const source = await readFile(new URL('App.tsx', sourceDirectory), 'utf8');
  const dialog = source.slice(source.indexOf('function ReadinessDiagnosticsDialog('), source.indexOf('function InspectorSection('));

  assert.match(dialog, /!report && loading/);
  assert.match(dialog, /failure \|\| 'Readiness diagnostics are unavailable/);
  assert.match(dialog, /Readiness diagnostics are unavailable for this connection/);
  assert.match(dialog, /loading \? 'Checking' : 'Unavailable'/);
});

test('update center keeps Studio and board updates independent', async () => {
  const source = await readFile(new URL('App.tsx', sourceDirectory), 'utf8');
  const dialog = source.slice(source.indexOf('function AboutDialog('), source.indexOf('const providerAPIs'));

  assert.match(dialog, /api\.checkStudioUpdate\(\)/);
  assert.match(dialog, /api\.checkBoardUpdate\(boardID\)/);
  assert.match(dialog, /Studio updates do not stop them/);
  assert.match(dialog, /Update board, then Studio/);
  assert.match(dialog, /activeTasks > 0/);
  assert.doesNotMatch(dialog, /api\.openExternalURL\(studioCheck\.releaseUrl/);
});
