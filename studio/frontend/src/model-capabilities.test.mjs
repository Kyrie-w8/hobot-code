import assert from 'node:assert/strict';
import test from 'node:test';
import {effectiveModel, modelAcceptsImages} from './model-capabilities.js';

const models = [
  {provider: 'drobotics', id: 'vision', default: true, capabilities: {reasoning: true, imageInput: true}},
  {provider: 'drobotics', id: 'text', capabilities: {reasoning: true, imageInput: false}},
  {provider: 'drobotics', id: 'legacy'},
];

test('blank model selection uses the board default capability', () => {
  assert.equal(effectiveModel(models, '').id, 'vision');
  assert.equal(modelAcceptsImages(models, ''), true);
});

test('explicit and legacy model capabilities remain conservative', () => {
  assert.equal(modelAcceptsImages(models, 'drobotics/text'), false);
  assert.equal(modelAcceptsImages(models, 'drobotics/legacy'), false);
  assert.equal(modelAcceptsImages(models, 'drobotics/removed'), false);
  assert.equal(effectiveModel(models, 'drobotics/removed'), undefined);
});
