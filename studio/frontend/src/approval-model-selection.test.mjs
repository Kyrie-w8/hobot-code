import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

test('Studio exposes a task-scoped approval model without coupling it to the Agent model', async () => {
  const [app, api] = await Promise.all([
    readFile(new URL('./App.tsx', import.meta.url), 'utf8'),
    readFile(new URL('./api.ts', import.meta.url), 'utf8'),
  ]);
  assert.match(app, /tasks\.permissions\.model\.v1/u);
  assert.match(app, /aria-label="Approval model"/u);
  assert.match(app, /<option value="">Follow Agent model<\/option>/u);
  assert.match(app, /approvalModel: selectedTask\.approvalModel \|\| undefined/u);
  assert.match(api, /SetTaskApprovalModel/u);
  assert.match(api, /setApprovalModel/u);
});

test('Approve for me explains routine direct execution instead of claiming every action uses a model', async () => {
  const source = await readFile(new URL('./App.tsx', import.meta.url), 'utf8');
  assert.match(source, /Routine actions run directly/u);
  assert.match(source, /reviews meaningful side effects/u);
  assert.doesNotMatch(source, /reviews each requested action/u);
});
