import assert from 'node:assert/strict';
import test from 'node:test';

import {workspaceChangeLabel, workspaceChangeSummary, workspaceDeliverySummary, workspaceDiffLines} from './workspace-changes.js';

test('workspace summaries do not attribute shared changes to one agent', () => {
  const summary = workspaceChangeSummary({repository: true, files: [{path: 'main.go'}]});
  assert.equal(summary.title, '1 changed file');
  assert.match(summary.detail, /another task, or manual edits/);
  assert.equal(workspaceChangeSummary({available: false, repository: false, files: []}).title, 'Git review unavailable');
  assert.equal(workspaceChangeSummary({available: true, repository: false, files: []}).title, 'No Git repository');
  assert.equal(workspaceChangeSummary({available: true, repository: true, files: []}).title, 'Working tree clean');
});

test('diff rendering is classified and bounded', () => {
  const result = workspaceDiffLines('diff --git a/a b/a\n@@ -1 +1 @@\n-old\n+new\n context', 4);
  assert.equal(result.truncated, true);
  assert.deepEqual(result.lines.map((line) => line.kind), ['meta', 'hunk', 'deleted', 'added']);
});

test('file labels preserve staging and conflict state', () => {
  assert.equal(workspaceChangeLabel({conflict: true}), 'Conflict');
  assert.equal(workspaceChangeLabel({untracked: true}), 'Untracked');
  assert.equal(workspaceChangeLabel({staged: true, unstaged: true}), 'Staged + Unstaged');
});

test('workspace delivery explains ready, blocked, and applied states without implying a commit', () => {
  const ready = workspaceDeliverySummary({ready: true, reason: 'ready'});
  assert.equal(ready.title, 'Ready to apply');
  assert.match(ready.detail, /never commits or pushes/);
  const blocked = workspaceDeliverySummary({ready: false, reason: 'Finish the active turn.'});
  assert.equal(blocked.detail, 'Finish the active turn.');
  assert.equal(workspaceDeliverySummary({ready: false, alreadyApplied: true}).tone, 'success');
  assert.equal(workspaceDeliverySummary(null), null);
});
