import assert from 'node:assert/strict';
import test from 'node:test';

import {compatibilityPresentation, compatibilityTargetLabel} from './compatibility-presentation.js';

const base = {appVersion: '0.26.0', agentdVersion: '0.26.0', protocol: 1, eventSchema: 4, boardId: 's100', rdkOsVersion: '4.0.5', validatedTarget: true, issues: []};

test('validated targets are described in user outcomes', () => {
  const result = compatibilityPresentation({...base, status: 'supported'});
  assert.equal(result.label, 'Ready');
  assert.match(result.description, /Daily Agent work/);
  assert.equal(result.action, '');
  assert.equal(compatibilityTargetLabel({...base, status: 'supported'}), 'S100 · RDK OS 4.0.5 · validated');
});

test('an unvalidated build limits hardware confidence, not daily development', () => {
  const result = compatibilityPresentation({...base, status: 'limited', validatedTarget: false, rdkOsVersion: '4.0.5-RC1', issues: [{code: 'rdk-os-unvalidated-version', severity: 'warning', message: 'raw detail', action: 'Verify this workflow.'}]});
  assert.equal(result.label, 'Hardware unverified');
  assert.match(result.title, /Daily Agent work is available/);
  assert.equal(result.action, 'Verify this workflow.');
  assert.match(compatibilityTargetLabel({...base, status: 'limited', validatedTarget: false}), /not validated$/);
});

test('missing hardware identity does not imply that core tasks are blocked', () => {
  const result = compatibilityPresentation({...base, status: 'limited', boardId: undefined, validatedTarget: false, issues: [{code: 'snapshot-unavailable', severity: 'warning', message: 'No snapshot', action: 'Reconnect.'}]});
  assert.equal(result.label, 'Hardware unverified');
  assert.equal(compatibilityTargetLabel({...base, status: 'limited', boardId: undefined}), 'Board identity unavailable');
});

test('missing optional features recommend an update without claiming the board is unusable', () => {
  const result = compatibilityPresentation({...base, status: 'limited', issues: [{code: 'missing-tasks.queue.v1', severity: 'warning', message: 'No queue', action: 'Update Hobot Code on the board.'}]});
  assert.equal(result.label, 'Update recommended');
  assert.match(result.title, /Core Agent work is available/);
});

test('hard incompatibility clearly stops Studio use', () => {
  const result = compatibilityPresentation({...base, status: 'upgrade-required', issues: [{code: 'protocol-incompatible', severity: 'error', message: 'Mismatch', action: 'Update and reconnect.'}]});
  assert.equal(result.tone, 'danger');
  assert.match(result.title, /Update the board/);
  assert.equal(result.action, 'Update and reconnect.');
});
