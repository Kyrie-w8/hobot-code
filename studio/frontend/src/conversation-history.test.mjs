import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import {historyPageBefore, mergeEventHistory, mergeMessageIndex, navigatorGroups, userMessagesFromEvents} from './conversation-history.js';

const event = (sequence, text = `message ${sequence}`) => ({sequence, time: new Date(sequence * 1000).toISOString(), normalized: {type: 'user.message', data: {text}}});

test('history merge keeps a stable ordered 2000-event sequence without duplicates', () => {
  const newest = Array.from({length: 200}, (_, index) => event(1801 + index));
  const older = Array.from({length: 1800}, (_, index) => event(1 + index));
  const merged = mergeEventHistory(newest, older);
  assert.equal(merged.length, 2000);
  assert.equal(merged[0].sequence, 1);
  assert.equal(merged.at(-1).sequence, 2000);
  assert.equal(mergeEventHistory(merged, newest).length, 2000);
});

test('history merge rejects conflicting records rather than silently replacing them', () => {
  assert.throws(() => mergeEventHistory([event(8, 'original')], [event(8, 'changed')]), /conflicting/);
  assert.throws(() => mergeEventHistory([], [{sequence: 0}]), /invalid/);
});

test('user index excludes schedules and navigation groups keep every message reachable', () => {
  const indexed = userMessagesFromEvents([...Array.from({length: 2000}, (_, index) => event(index + 1)), {...event(2001, 'scheduled'), normalized: {type: 'user.message', data: {text: 'scheduled', source: 'schedule'}}}]);
  const groups = navigatorGroups(mergeMessageIndex([], indexed));
  assert.equal(indexed.length, 2000);
  assert.equal(groups.length, 118);
  assert.equal(new Set(groups.flat().map((message) => message.sequence)).size, 2000);
  assert.deepEqual(navigatorGroups(indexed.slice(0, 5)), []);
});

test('mock-style tail pages use an exclusive before cursor across two pages', () => {
  const events = Array.from({length: 240}, (_, index) => event(index + 1));
  const tail = historyPageBefore(events, 0, 200);
  const older = historyPageBefore(events, tail.nextBefore, 200);
  assert.deepEqual([tail.events[0].sequence, tail.events.at(-1).sequence, tail.nextBefore, tail.hasEarlier], [41, 240, 41, true]);
  assert.deepEqual([older.events[0].sequence, older.events.at(-1).sequence, older.nextBefore, older.hasEarlier], [1, 40, 1, false]);
});

test('Studio wires anchor preservation, hover previews, and accessible navigator state', async () => {
  const source = await readFile(new URL('./App.tsx', import.meta.url), 'utf8');
  assert.match(source, /prependAnchor/);
  assert.match(source, /navigator-preview/);
  assert.match(source, /aria-current/);
  assert.match(source, /IntersectionObserver/);
  assert.match(source, /api\.beforeEvents/);
  assert.match(source, /page\.cursorExpired/);
  assert.match(source, /setHasEarlierHistory\(false\)/);
  assert.match(source, /Earlier history is no longer retained by this board/);
});
