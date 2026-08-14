import assert from 'node:assert/strict';
import test from 'node:test';

import {supportBundlePresentation} from './support-diagnostics.js';

test('support diagnostics use explicit v2 status', () => {
  assert.equal(supportBundlePresentation({status: 'action-required', checks: {fail: 1, warn: 0}}).tone, 'failed');
  assert.equal(supportBundlePresentation({status: 'attention', checks: {fail: 0, warn: 1}}).tone, 'partial');
  assert.equal(supportBundlePresentation({status: 'healthy', checks: {fail: 0, warn: 0}}).tone, 'passed');
});

test('legacy bundles derive a truthful presentation from check counts', () => {
  assert.equal(supportBundlePresentation({checks: {fail: 2, warn: 0}}).label, 'Action required');
  assert.equal(supportBundlePresentation({checks: {fail: 0, warn: 2}}).label, 'Needs attention');
  assert.equal(supportBundlePresentation({checks: {fail: 0, warn: 0}}).label, 'No current faults');
});
