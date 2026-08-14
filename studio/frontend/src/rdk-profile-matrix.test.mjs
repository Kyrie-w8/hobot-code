import assert from 'node:assert/strict';
import test from 'node:test';
import {currentModelRDKMatrix, rdkProfileEvidenceLabel, rdkProfileState} from './rdk-profile-matrix.js';

const model = {provider: 'drobotics', id: 'kimi-k3'};

test('RDK matrices are scoped to the exact model', () => {
	const matrix = {provider: 'drobotics', model: 'kimi-k3', profiles: []};
	assert.equal(currentModelRDKMatrix(matrix, model), matrix);
	assert.equal(currentModelRDKMatrix(matrix, {...model, id: 'glm-5.2'}), undefined);
});

test('profile state never turns plans or stale evidence into current evidence', () => {
	assert.equal(rdkProfileState({id: 'future', availability: 'planned', evidenceState: 'untested'}), 'planned');
	assert.equal(rdkProfileState({id: 'target', availability: 'unsupported-target', evidenceState: 'untested'}), 'unsupported');
	assert.equal(rdkProfileState({id: 'live', availability: 'available', evidenceState: 'untested'}, 'live'), 'running');
	assert.equal(rdkProfileState({id: 'old', availability: 'available', evidenceState: 'stale', result: {status: 'passed', releaseEligible: true}}), 'stale');
	assert.equal(rdkProfileEvidenceLabel({id: 'old', availability: 'available', evidenceState: 'stale', result: {status: 'passed', releaseEligible: true}}), 'Retest needed');
});

test('current development and release evidence remain distinct', () => {
	assert.equal(rdkProfileState({id: 'dev', availability: 'available', evidenceState: 'current', result: {status: 'passed', releaseEligible: false}}), 'partial');
	assert.equal(rdkProfileState({id: 'release', availability: 'available', evidenceState: 'current', result: {status: 'passed', releaseEligible: true}}), 'passed');
	assert.equal(rdkProfileState({id: 'failed', availability: 'available', evidenceState: 'current', result: {status: 'failed'}}), 'failed');
});
