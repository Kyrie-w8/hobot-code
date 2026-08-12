import assert from 'node:assert/strict';
import test from 'node:test';

import {rdkWorkflows} from './rdk-workflows.js';

test('RDK starters are bounded and contain verification-oriented instructions', () => {
  const values = rdkWorkflows('s600');
  assert.equal(values.length, 5);
  assert.equal(new Set(values.map((value) => value.id)).size, values.length);
  assert.match(values.find((value) => value.id === 'deploy-model').prompt, /toolchain|accuracy|latency/);
  assert.ok(values.every((value) => value.prompt.length < 600));
});

test('unknown boards lead with diagnosis', () => {
  assert.equal(rdkWorkflows('unknown')[0].id, 'diagnose');
});
