import assert from 'node:assert/strict';
import test from 'node:test';
import {currentModelConformance, modelConformancePresentation} from './model-conformance.js';

const model = {provider: 'drobotics', id: 'kimi-k3'};
const result = {
  provider: 'drobotics', model: 'kimi-k3', status: 'verified', scope: 'gateway-protocol', agentRuntimeStatus: 'not-tested', rdkTaskStatus: 'not-tested',
  message: 'The gateway protocol probe passed.', expiresAt: '2026-08-14T12:00:00Z', checks: [{name: 'streaming', status: 'passed'}],
};

test('model conformance is scoped to the selected model and expiration', () => {
  assert.equal(currentModelConformance(result, model, Date.parse('2026-08-14T11:00:00Z')), result);
  assert.equal(currentModelConformance(result, {...model, id: 'glm-5.2'}, Date.parse('2026-08-14T11:00:00Z')), undefined);
  assert.equal(currentModelConformance(result, model, Date.parse('2026-08-14T12:00:00Z')), undefined);
});

test('model conformance presentation never implies RDK task qualification', () => {
  const verified = modelConformancePresentation(result);
  assert.equal(verified.label, 'Protocol OK');
  assert.match(verified.title, /Agent runtime: not-tested/);
  assert.match(verified.title, /RDK tasks: not-tested/);
  assert.doesNotMatch(verified.label, /^Verified$/);
  assert.equal(modelConformancePresentation({...result, status: 'compatible'}).label, 'Fallback');
  assert.equal(modelConformancePresentation({...result, status: 'failed'}).label, 'Protocol failed');
  assert.match(modelConformancePresentation(undefined).title, /does not test RDK task quality/);
});
