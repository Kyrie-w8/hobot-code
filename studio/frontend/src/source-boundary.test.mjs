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
