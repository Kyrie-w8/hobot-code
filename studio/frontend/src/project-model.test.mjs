import assert from 'node:assert/strict';
import test from 'node:test';
import {arrangeTasks, groupTasksByProject} from './project-model.js';

const task = (id, overrides = {}) => ({id, cwd: '/root/project', createdAt: overrides.updatedAt ?? '2026-08-11T00:00:00Z', updatedAt: '2026-08-11T00:00:00Z', ...overrides});

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

test('edited timelines replace the source conversation instead of appearing as side agents', () => {
  const arranged = arrangeTasks([
    task('main'),
    task('side', {parentTaskId: 'main', branchKind: 'side', updatedAt: '2026-08-11T00:01:00Z'}),
    task('edit-old', {parentTaskId: 'main', branchKind: 'edit', updatedAt: '2026-08-11T00:02:00Z'}),
    task('edit-new', {parentTaskId: 'main', branchKind: 'edit', updatedAt: '2026-08-11T00:03:00Z'}),
  ]);
  assert.deepEqual(arranged.map(({task: item, depth, branchKind}) => [item.id, depth, branchKind]), [
    ['edit-new', 0, ''], ['side', 1, 'side'],
  ]);
});

test('editing a side agent keeps it in the side-agent slot', () => {
  const arranged = arrangeTasks([
    task('main'),
    task('side', {parentTaskId: 'main', branchKind: 'side', updatedAt: '2026-08-11T00:01:00Z'}),
    task('side-edit', {parentTaskId: 'side', branchKind: 'edit', updatedAt: '2026-08-11T00:02:00Z'}),
  ]);
  assert.deepEqual(arranged.map(({task: item, depth, branchKind}) => [item.id, depth, branchKind]), [
    ['main', 0, ''], ['side-edit', 1, 'side'],
  ]);
});

test('an edited timeline still appears when search excludes its internal source task', () => {
  const arranged = arrangeTasks([
    task('edit', {parentTaskId: 'hidden-by-search', branchKind: 'edit'}),
  ]);
  assert.deepEqual(arranged.map(({task: item, depth, branchKind}) => [item.id, depth, branchKind]), [
    ['edit', 0, ''],
  ]);
});
