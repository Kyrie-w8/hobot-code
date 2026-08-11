import assert from 'node:assert/strict';
import test from 'node:test';
import {arrangeTasks, groupTasksByProject} from './project-model.js';

const task = (id, overrides = {}) => ({id, cwd: '/root/project', updatedAt: '2026-08-11T00:00:00Z', ...overrides});

test('projects group conversations beneath their workspace', () => {
  const groups = groupTasksByProject([task('one'), task('two'), task('general', {cwd: '/root'})]);
  assert.deepEqual(groups.map((group) => [group.name, group.tasks.length]), [['General', 1], ['project', 2]]);
});

test('side tasks remain siblings even when created from another side task', () => {
  const arranged = arrangeTasks([
    task('main'),
    task('side-one', {parentTaskId: 'main', branchKind: 'side', updatedAt: '2026-08-11T00:01:00Z'}),
    task('side-two', {parentTaskId: 'side-one', branchKind: 'side', updatedAt: '2026-08-11T00:02:00Z'}),
  ]);
  assert.deepEqual(arranged.map(({task: item, depth}) => [item.id, depth]), [
    ['main', 0], ['side-two', 1], ['side-one', 1],
  ]);
});
