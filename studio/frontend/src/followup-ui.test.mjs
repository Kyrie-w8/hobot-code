import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

const appSource = await readFile(new URL('./App.tsx', import.meta.url), 'utf8');

test('Working composer is capability gated and uses the same Stop/Queue control', () => {
  assert.match(appSource, /tasks\.followup-queue\.v1/);
  assert.match(appSource, /canCancelCurrentWork && !composer\.trim\(\)/);
  assert.match(appSource, /title=\{pendingUncertain \? 'Send again' : supportsFollowupQueue && \['starting', 'running', 'waiting'\]/);
  assert.match(appSource, /composerIsBlocked\(selectedTask\.status, supportsFollowupQueue\)/);
});

test('follow-up cards expose single-item cancel and explicit uncertain retry', () => {
  assert.match(appSource, /onCancel=\{async \(queueId\)/);
  assert.match(appSource, /item\.status === 'queued' \|\| item\.status === 'blocked'/);
  assert.match(appSource, /item\.recovery !== 'retry'/);
  assert.match(appSource, /item\.recovery === 'retry'/);
  assert.match(appSource, /Retry anyway/);
  assert.match(appSource, /Delivery status is uncertain/);
  assert.match(appSource, /Send again to create a new request/);
  assert.match(appSource, /pendingUncertain \? 'Send again'/);
});

test('failed Studio submissions reuse the complete prompt and attachment payload key', () => {
  assert.match(appSource, /pendingPromptRetry\.fingerprint === fingerprint/);
  assert.match(appSource, /images: submittedImages\.map/);
  assert.match(appSource, /setPendingPromptRetry\(\{taskId: selectedTask\.id, prompt, fingerprint, key: retryKey\}\)/);
});
