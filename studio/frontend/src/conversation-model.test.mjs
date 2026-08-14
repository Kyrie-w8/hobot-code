import assert from 'node:assert/strict';
import test from 'node:test';
import {buildConversation, elapsedLabel, eventRetentionPresentation, failurePresentation, recentEventsAfter} from './conversation-model.js';

const event = (sequence, type, data = {}, raw = {}) => ({
  protocol: 1, kind: 'event', taskId: 'task', sequence,
  time: new Date(sequence * 1000).toISOString(), event: raw,
  normalized: {schema: 3, type, data},
});

test('conversation groups thinking, tools, and text into one assistant turn', () => {
  const result = buildConversation([
    event(1, 'user.message', {text: 'Check the board', attachments: [{name: 'board.png', mimeType: 'image/png'}]}),
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
  assert.deepEqual(result[0].attachments, [{name: 'board.png', mimeType: 'image/png'}]);
  assert.equal(result[1].kind, 'assistant');
  assert.equal(result[1].thinking, 'Inspecting results.');
  assert.equal(result[1].text, 'Board is healthy.');
  assert.equal(result[1].tools[0].input, '{\n  "command": "uname -a"\n}');
  assert.equal(result[1].tools[0].output, 'Linux');
  assert.equal(result[1].completed, true);
});

test('schema four tool previews do not require raw provider fields', () => {
  const result = buildConversation([
    event(1, 'user.message', {text: 'Run'}),
    event(2, 'tool.started', {toolCallId: 'one', toolName: 'bash', inputPreview: 'pwd'}),
    event(3, 'tool.completed', {toolCallId: 'one', toolName: 'bash', outputPreview: '/root', isError: false}),
    event(4, 'task.idle'),
  ]);
  assert.equal(result[1].tools[0].input, 'pwd');
  assert.equal(result[1].tools[0].output, '/root');
});

test('compaction lifecycle remains visible inside the active turn', () => {
  const result = buildConversation([
    event(1, 'user.message', {text: 'Continue the long task'}),
    event(2, 'assistant.text.delta', {delta: 'Working.'}),
    event(3, 'compaction_start'),
    event(4, 'compaction_end'),
    event(5, 'assistant.text.delta', {delta: ' Done.'}),
    event(6, 'task.idle'),
  ]);
  assert.equal(result.length, 2);
  assert.equal(result[1].text, 'Working. Done.');
  assert.deepEqual(result[1].notices.map((notice) => notice.label), [
    'Compacting context',
    'Context compacted',
  ]);
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

test('a failed message remains visible even without assistant text', () => {
  const result = buildConversation([
    event(1, 'user.message', {text: 'Run the task'}),
    event(2, 'assistant.message.completed', {errorMessage: 'HTTP 400: Unsupported model: kimi/missing'}),
    event(3, 'task.idle'),
  ]);
  assert.equal(result.length, 2);
  assert.equal(result[1].kind, 'assistant');
  assert.equal(result[1].failure.category, 'model');
  assert.equal(result[1].failure.title, 'The selected model is unavailable');
  assert.doesNotMatch(result[1].failure.message, /kimi|HTTP 400/);
});

test('failure presentation classifies common failures without exposing raw details', () => {
  const cases = [
    ['Bearer sk-private HTTP 401 unauthorized', 'authentication'],
    ['HTTP 429 quota exceeded', 'rate-limit'],
    ['gateway stream ended before message_stop request_id=private', 'connection'],
    ['unexpected provider detail /root/private', 'unknown'],
  ];
  for (const [raw, category] of cases) {
    const view = failurePresentation(raw);
    assert.equal(view.category, category);
    assert.doesNotMatch(`${view.title} ${view.message}`, /sk-private|request_id|\/root\/private|provider detail/);
  }
});

test('elapsed labels remain compact', () => {
  assert.equal(elapsedLabel(new Date(0).toISOString(), new Date(750).toISOString()), '<1s');
  assert.equal(elapsedLabel(new Date(0).toISOString(), new Date(12_000).toISOString()), '12s');
  assert.equal(elapsedLabel(new Date(0).toISOString(), new Date(72_000).toISOString()), '1m 12s');
});
test('long conversations load a bounded window from the newest events', () => {
  assert.equal(recentEventsAfter(1933), 1533);
  assert.equal(recentEventsAfter(120), 0);
  assert.equal(recentEventsAfter(1933, 200), 1733);
});

test('event retention distinguishes a normal window, an expired cursor, and a legacy durability gap', () => {
  assert.equal(eventRetentionPresentation({historyTruncated: false}), null);
  assert.deepEqual(eventRetentionPresentation({historyTruncated: true, retainedFrom: 401, retainedThrough: 800, latestSequence: 800}), {
    title: 'This long task is retaining its newest activity.', detail: 'Conversation history currently begins at event 401.',
  });
  assert.equal(eventRetentionPresentation({historyTruncated: true, cursorExpired: true, retainedFrom: 401, retainedThrough: 800, latestSequence: 800}).title, 'Earlier activity is outside this retained window.');
  assert.equal(eventRetentionPresentation({historyTruncated: true, retainedFrom: 1, retainedThrough: 300, latestSequence: 500}).title, 'Some recent activity could not be recovered.');
});
