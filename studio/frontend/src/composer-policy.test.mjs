import assert from 'node:assert/strict';
import test from 'node:test';
import {composerIsBlocked, composerMode, shouldSubmitComposer} from './composer-policy.js';

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

test('composer blocks transient and busy task states', () => {
  for (const status of ['starting', 'running', 'waiting', 'stopping']) {
    assert.equal(composerIsBlocked(status), true);
  }
  for (const status of ['idle', 'stopped', 'failed', 'interrupted']) {
    assert.equal(composerIsBlocked(status), false);
  }
});
