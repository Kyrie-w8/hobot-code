import assert from 'node:assert/strict';
import test from 'node:test';
import {buildConversation, elapsedLabel} from './conversation-model.js';

const event = (sequence, type, data = {}, raw = {}) => ({
  protocol: 1, kind: 'event', taskId: 'task', sequence,
  time: new Date(sequence * 1000).toISOString(), event: raw,
  normalized: {schema: 3, type, data},
});

test('conversation groups thinking, tools, and text into one assistant turn', () => {
  const result = buildConversation([
    event(1, 'user.message', {text: 'Check the board'}),
    event(2, 'task.running'),
    event(3, 'assistant.thinking.delta', {delta: 'Inspecting '}),
    event(4, 'tool.started', {toolCallId: 'one', toolName: 'bash'}, {args: {command: 'uname -a'}}),
    event(5, 'assistant.thinking.delta', {delta: 'results.'}),
    event(6, 'tool.completed', {toolCallId: 'one', toolName: 'bash', isError: false}, {result: 'Linux'}),
    event(7, 'assistant.text.delta', {delta: 'Board is healthy.'}),
    event(8, 'assistant.message.completed'),
    event(9, 'task.idle'),
  ]);
  assert.equal(result.length, 2);
  assert.equal(result[0].kind, 'user');
  assert.equal(result[0].text, 'Check the board');
  assert.equal(result[1].kind, 'assistant');
  assert.equal(result[1].thinking, 'Inspecting results.');
  assert.equal(result[1].text, 'Board is healthy.');
  assert.equal(result[1].tools[0].input, '{\n  "command": "uname -a"\n}');
  assert.equal(result[1].tools[0].output, 'Linux');
  assert.equal(result[1].completed, true);
});

test('conversation separates turns at each persisted user message', () => {
  const result = buildConversation([
    event(1, 'assistant.text.delta', {delta: 'Legacy response'}),
    event(2, 'task.idle'),
    event(3, 'user.message', {text: 'Follow up'}),
    event(4, 'assistant.text.delta', {delta: 'New response'}),
    event(5, 'task.idle'),
  ]);
  assert.deepEqual(result.map((item) => item.kind), ['assistant', 'user', 'assistant']);
});

test('lifecycle noise alone does not create conversation items', () => {
  assert.deepEqual(buildConversation([event(1, 'task.running'), event(2, 'task.idle')]), []);
});

test('elapsed labels remain compact', () => {
  assert.equal(elapsedLabel(new Date(0).toISOString(), new Date(750).toISOString()), '<1s');
  assert.equal(elapsedLabel(new Date(0).toISOString(), new Date(12_000).toISOString()), '12s');
  assert.equal(elapsedLabel(new Date(0).toISOString(), new Date(72_000).toISOString()), '1m 12s');
});
