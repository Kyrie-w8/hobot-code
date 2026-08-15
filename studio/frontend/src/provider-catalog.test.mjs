import assert from 'node:assert/strict';
import test from 'node:test';
import {includedModelSummary, includedProviderGroups} from './provider-catalog.js';

test('included D-Robotics models stay visible when no custom providers exist', () => {
  const groups = includedProviderGroups([
    {provider: 'drobotics', id: 'glm-5.2', name: 'GLM 5.2'},
    {provider: 'drobotics', id: 'kimi-k3', name: 'Kimi K3', default: true, capabilities: {reasoning: true, imageInput: true}},
  ], []);

  assert.equal(groups.length, 1);
  assert.equal(groups[0].name, 'D-Robotics');
  assert.deepEqual(groups[0].models.map((model) => model.id), ['kimi-k3', 'glm-5.2']);
  assert.equal(includedModelSummary(groups[0].models[0]), 'Reasoning · Images · Default');
});

test('managed providers are rendered only in the editable provider section', () => {
  const groups = includedProviderGroups([
    {provider: 'drobotics', id: 'kimi-k3'},
    {provider: 'acme', id: 'coder', managed: true},
    {provider: 'other', id: 'model'},
  ], [{id: 'other'}]);

  assert.deepEqual(groups.map((group) => group.id), ['drobotics']);
});

test('included provider grouping is deterministic and removes duplicate model rows', () => {
  const groups = includedProviderGroups([
    {provider: 'zeta', id: 'second'},
    {provider: 'alpha', id: 'one'},
    {provider: 'zeta', id: 'first'},
    {provider: 'zeta', id: 'first'},
  ]);

  assert.deepEqual(groups.map((group) => group.id), ['alpha', 'zeta']);
  assert.deepEqual(groups[1].models.map((model) => model.id), ['first', 'second']);
  assert.equal(includedModelSummary(groups[0].models[0]), 'Text');
});
