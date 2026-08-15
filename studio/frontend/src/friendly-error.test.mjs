import assert from 'node:assert/strict';
import test from 'node:test';

import {friendlyError} from './friendly-error.js';

test('protocol validation failures become actionable product messages', () => {
  assert.match(friendlyError('Error: board returned invalid diagnostics: invalid diagnostic check'), /Update Hobot Code on the board/);
  assert.doesNotMatch(friendlyError('Error: board returned invalid diagnostics: invalid diagnostic check'), /invalid diagnostic check/);
  assert.match(friendlyError('models_qualification_write_failed: refuse invalid qualification evidence: invalid protocol evidence'), /verification result could not be saved/);
});

test('connection, configuration, and session failures retain their recovery action', () => {
  assert.match(friendlyError('context deadline exceeded'), /network or VPN/);
  assert.match(friendlyError('configuration-restart-required'), /hobot daemon restart/);
  assert.match(friendlyError('task_resume_failed: task has no resumable Hobot Code session'), /Start a new session/);
  assert.match(friendlyError('requires a newer Hobot Code event schema'), /Update the board-side/);
});

test('unknown errors remain visible without redundant transport prefixes', () => {
  assert.equal(friendlyError('Error: task_start_failed: workspace is unavailable'), 'workspace is unavailable');
});
