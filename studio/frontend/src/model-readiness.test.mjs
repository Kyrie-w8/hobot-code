import assert from 'node:assert/strict';
import test from 'node:test';
import {currentModelProbe, currentModelQualification, modelReadinessPresentation, qualificationEvidenceNotice, qualificationExpirations, qualificationLayer} from './model-readiness.js';

const model = {provider: 'drobotics', id: 'kimi-k3'};

test('deep probe results are scoped to the exact model', () => {
  const result = {provider: 'drobotics', model: 'kimi-k3', status: 'partial'};
  assert.equal(currentModelProbe(result, model), result);
  assert.equal(currentModelProbe(result, {...model, id: 'glm-5.2'}), undefined);
});

test('readiness labels preserve evidence boundaries', () => {
  assert.equal(modelReadinessPresentation({health: {status: 'available'}}).label, 'Route available');
  assert.equal(modelReadinessPresentation({conformance: {status: 'verified'}}).label, 'Protocol OK');
  assert.equal(modelReadinessPresentation({runtime: {status: 'partial'}}).label, 'Runtime tested');
  const development = modelReadinessPresentation({rdk: {status: 'passed', releaseEligible: false}});
  assert.equal(development.label, 'Profile passed');
  assert.match(development.title, /not eligible as public release evidence/);
  const release = modelReadinessPresentation({rdk: {status: 'passed', releaseEligible: true}});
  assert.equal(release.label, 'Profile qualified');
  assert.match(release.title, /One named RDK profile/);
  assert.doesNotMatch(release.label, /^Verified$/);
});

test('failure dominates and running is explicit', () => {
  assert.equal(modelReadinessPresentation({health: {status: 'available'}, runtime: {status: 'failed'}}).label, 'Needs attention');
  assert.equal(modelReadinessPresentation({running: true}).label, 'Testing');
});

test('persisted evidence is exact-model and stale layers never count as current', () => {
  const qualification = {provider: 'drobotics', model: 'kimi-k3', state: 'stale', staleReasons: ['board-changed'], staleLayers: ['rdk'], expiredLayers: [], rdk: {status: 'passed'}};
  assert.equal(currentModelQualification(qualification, model), qualification);
  assert.equal(currentModelQualification(qualification, {...model, id: 'glm-5.2'}), undefined);
  assert.equal(qualificationLayer(qualification, 'rdk', qualification.rdk), undefined);
  assert.equal(modelReadinessPresentation({evidenceState: 'stale'}).label, 'Retest needed');
  assert.match(qualificationEvidenceNotice(qualification), /board or RDK OS/);
});

test('expired short-lived evidence is visible but cannot drive readiness', () => {
  const qualification = {provider: 'drobotics', model: 'kimi-k3', state: 'expired', staleReasons: [], staleLayers: [], expiredLayers: ['route'], health: {status: 'available'}};
  assert.equal(qualificationLayer(qualification, 'route', qualification.health), undefined);
  assert.equal(modelReadinessPresentation({evidenceState: 'expired'}).label, 'Checks expired');
  assert.match(qualificationEvidenceNotice(qualification), /route evidence expired/);
  const dynamicallyExpired = {...qualification, state: 'current', expiredLayers: [], health: {status: 'available', expiresAt: '2026-08-14T00:05:00Z'}};
  assert.equal(qualificationLayer(dynamicallyExpired, 'route', dynamicallyExpired.health, Date.parse('2026-08-14T00:05:01Z')), undefined);
  assert.deepEqual(qualificationExpirations(dynamicallyExpired, Date.parse('2026-08-14T00:05:01Z')), ['route']);
});
