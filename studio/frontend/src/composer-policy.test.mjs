import assert from 'node:assert/strict';
import test from 'node:test';
import {composerIsBlocked, composerMode, shouldCancelTurnShortcut, shouldSubmitComposer, turnCancellationMode} from './composer-policy.js';

test('Enter submits while Shift+Enter and IME composition keep editing', () => {
  assert.equal(shouldSubmitComposer('Enter', false, false), true);
  assert.equal(shouldSubmitComposer('Enter', true, false), false);
  assert.equal(shouldSubmitComposer('Enter', false, true), false);
  assert.equal(shouldSubmitComposer('a', false, false), false);
});

test('terminal tasks resume only when a session exists', () => {
  assert.equal(composerMode({status: 'idle'}), 'send');
  assert.equal(composerMode({status: 'stopped', sessionFile: '/state/session.jsonl'}), 'resume');
  assert.equal(composerMode({status: 'stopped'}), 'restart');
  assert.equal(composerMode({status: 'failed'}), 'restart');
});

test('structured recovery overrides stale session inference', () => {
  assert.equal(composerMode({status: 'failed', sessionFile: '/state/session.jsonl', failure: {recovery: 'restart'}}), 'restart');
  assert.equal(composerMode({status: 'interrupted', sessionFile: '/state/session.jsonl', failure: {recovery: 'resume'}}), 'resume');
  assert.equal(composerMode({status: 'interrupted', failure: {recovery: 'resume'}}), 'restart');
});

test('composer blocks transient and busy task states', () => {
	assert.equal(composerIsBlocked('queued'), true);
	assert.equal(composerIsBlocked('stopping'), true);
	for (const status of ['starting', 'running', 'waiting']) {
		assert.equal(composerIsBlocked(status), true);
		assert.equal(composerIsBlocked(status, true), false);
	}
  for (const status of ['idle', 'stopped', 'failed', 'interrupted']) {
    assert.equal(composerIsBlocked(status), false);
  }
});

test('Escape aborts only an active turn while queued work is cancelled', () => {
  assert.equal(turnCancellationMode('queued'), 'stop');
  for (const status of ['starting', 'running', 'waiting']) assert.equal(turnCancellationMode(status), 'abort');
  for (const status of ['idle', 'stopping', 'stopped', 'failed', 'interrupted']) assert.equal(turnCancellationMode(status), undefined);

  assert.equal(shouldCancelTurnShortcut('Escape', false, false, 'running'), true);
  assert.equal(shouldCancelTurnShortcut('Escape', false, false, 'waiting'), true);
  assert.equal(shouldCancelTurnShortcut('Escape', false, false, 'queued'), true);
  assert.equal(shouldCancelTurnShortcut('Escape', true, false, 'running'), false);
  assert.equal(shouldCancelTurnShortcut('Escape', false, true, 'running'), false);
  assert.equal(shouldCancelTurnShortcut('Enter', false, false, 'running'), false);
  assert.equal(shouldCancelTurnShortcut('Escape', false, false, 'idle'), false);
});
