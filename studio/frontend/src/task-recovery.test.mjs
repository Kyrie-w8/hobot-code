import assert from 'node:assert/strict';
import test from 'node:test';

import {taskRecovery, taskRecoveryActionAvailable} from './task-recovery.js';

test('structured failures map to one safe recovery action', () => {
  assert.deepEqual(taskRecovery({status: 'failed', failure: {code: 'model-unavailable', message: 'ignored', recovery: 'check-model'}}), {
    title: 'Task stopped unexpectedly', message: 'The selected model route could not complete this task.', recovery: 'check-model', action: {label: 'Check model'},
  });
  const interrupted = taskRecovery({status: 'interrupted', sessionFile: '/private/session', failure: {code: 'handoff-uncertain', message: 'ignored', recovery: 'resume'}});
  assert.equal(interrupted.recovery, 'resume');
  assert.match(interrupted.action.prompt, /without repeating completed side effects/);
});

test('capability-dependent recovery actions fail closed', () => {
  assert.equal(taskRecoveryActionAvailable('check-model', false, true), false);
  assert.equal(taskRecoveryActionAvailable('diagnose', true, false), false);
  assert.equal(taskRecoveryActionAvailable('resume', false, false), true);
});

test('legacy errors never expose raw backend details', () => {
  const raw = 'HTTP 401 token=top-secret at /root/private/project';
  const result = taskRecovery({status: 'failed', lastError: raw});
  assert.equal(result.recovery, 'restart');
  assert.doesNotMatch(`${result.title} ${result.message}`, /top-secret|\/root\/private|HTTP 401/);
  assert.equal(taskRecovery({status: 'stopped', lastError: raw}), null);
});

test('interrupted task explains incomplete tool and workspace evidence', () => {
  const result = taskRecovery({
    status: 'interrupted', sessionFile: '/private/session',
    failure: {code: 'service-restarted'},
    turnEvidence: [{turn: 2, status: 'interrupted', evidence: 'partial', openTools: 1}],
  });
  assert.match(result.message, /1 tool action did not report completion/);
  assert.match(result.message, /side effects are unknown/);

  const changed = taskRecovery({
    status: 'interrupted', failure: {code: 'service-restarted'},
    turnEvidence: [{turn: 1, status: 'interrupted', evidence: 'complete', openTools: 0, workspaceChanged: true}],
  });
  assert.match(changed.message, /Git workspace changes were detected/);
});
