import assert from 'node:assert/strict';
import test from 'node:test';

import {taskAttention} from './task-notifications.js';

test('background task transitions surface only actionable attention', () => {
  assert.equal(taskAttention('running', 'idle'), 'Task completed');
  assert.equal(taskAttention('running', 'waiting'), 'Approval needed');
  assert.equal(taskAttention('running', 'failed'), 'Task failed');
  assert.equal(taskAttention('idle', 'running'), '');
});

test('initial, unchanged, and selected task states stay quiet', () => {
  assert.equal(taskAttention('', 'waiting'), '');
  assert.equal(taskAttention('waiting', 'waiting'), '');
  assert.equal(taskAttention('running', 'waiting', true), '');
});
