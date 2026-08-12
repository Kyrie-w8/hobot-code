import assert from 'node:assert/strict';
import test from 'node:test';
import {currentModelHealth, modelHealthLabel} from './model-health.js';

const model = {provider: 'drobotics', id: 'kimi-k3'};
const health = {provider: 'drobotics', model: 'kimi-k3', expiresAt: '2026-08-13T10:05:00Z'};

test('model health is scoped to the exact model and expiry', () => {
  assert.equal(currentModelHealth(health, model, Date.parse('2026-08-13T10:04:59Z')), health);
  assert.equal(currentModelHealth(health, {...model, id: 'glm-5.2'}, Date.parse('2026-08-13T10:04:59Z')), undefined);
  assert.equal(currentModelHealth(health, model, Date.parse('2026-08-13T10:05:00Z')), undefined);
});

test('model health categories have compact user-facing labels', () => {
  assert.equal(modelHealthLabel('model-unavailable'), 'No route');
  assert.equal(modelHealthLabel('authentication'), 'Auth failed');
  assert.equal(modelHealthLabel('protocol'), 'Unavailable');
});
