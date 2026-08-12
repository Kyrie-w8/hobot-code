import assert from 'node:assert/strict';
import test from 'node:test';
import {deploymentCanStart, deploymentCompatibilityLabel, deploymentPhaseLabel, deploymentProfileFor, preferredDeploymentArtifact} from './deployment-model.js';

const artifact = (name, compatibility) => ({name, compatibility});

test('deployment selection prefers a board candidate over source models', () => {
  const selected = preferredDeploymentArtifact([artifact('model.onnx', 'conversion-required'), artifact('model_s100.hbm', 'candidate')]);
  assert.equal(selected.name, 'model_s100.hbm');
});

test('wrong-board artifacts cannot start deployment', () => {
  assert.equal(deploymentCanStart(artifact('model_s600.hbm', 'mismatch')), false);
  assert.equal(deploymentCanStart(artifact('model.onnx', 'conversion-required')), true);
});

test('deployment labels distinguish verified completion from incomplete reports', () => {
  assert.equal(deploymentCompatibilityLabel('mismatch'), 'Different board');
  assert.equal(deploymentPhaseLabel('passed'), 'Verified deployment');
  assert.equal(deploymentPhaseLabel('invalid-report'), 'Report rejected');
});

test('known X5 source models bind their frozen acceptance profile', () => {
  assert.equal(deploymentProfileFor(artifact('regnet_x_400mf_224.onnx', 'conversion-required'), 'x5'), 'regnet-x-400mf-x5');
  assert.equal(deploymentProfileFor(artifact('regnet_x_400mf_224.onnx', 'conversion-required'), 's100'), '');
  assert.equal(deploymentProfileFor(artifact('custom.onnx', 'conversion-required'), 'x5'), '');
});
