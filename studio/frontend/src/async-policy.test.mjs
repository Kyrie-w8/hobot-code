import assert from 'node:assert/strict';
import test from 'node:test';

import {isCurrentRequest, isCurrentTarget, watchRetryDelay, watchStatusLabel} from './async-policy.js';

test('only the latest asynchronous request may update the current view', () => {
  assert.equal(isCurrentRequest(4, 4), true);
  assert.equal(isCurrentRequest(3, 4), false);
  assert.equal(isCurrentRequest(Number.NaN, Number.NaN), false);
});

test('task-local results cannot update another board or conversation', () => {
  assert.equal(isCurrentTarget('board-a', 'task-a', 'board-a', 'task-a'), true);
  assert.equal(isCurrentTarget('board-a', 'task-a', 'board-a', 'task-b'), false);
  assert.equal(isCurrentTarget('board-a', 'task-a', 'board-b', 'task-a'), false);
});

test('watch recovery backs off quickly and caps the delay', () => {
  assert.deepEqual([1, 2, 3, 4, 5, 6, 20].map((attempt) => watchRetryDelay(attempt)), [1000, 2000, 4000, 8000, 15000, 15000, 15000]);
  assert.equal(watchRetryDelay(4, 5000), 5000);
});

test('watch failures remain visible while recovery is scheduled', () => {
  assert.equal(watchStatusLabel({state: 'failed'}), 'Live updates paused - retrying');
  assert.equal(watchStatusLabel({state: 'reconnecting'}), 'Live updates reconnecting');
  assert.equal(watchStatusLabel({state: 'connected'}), '');
});
