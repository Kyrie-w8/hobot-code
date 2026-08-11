const assistantEventTypes = new Set([
  'assistant.thinking.delta',
  'assistant.text.delta',
  'assistant.message.completed',
  'tool.started',
  'tool.progress',
  'tool.completed',
  'approval.requested',
  'approval.resolved',
  'retry_start',
  'retry_end',
  'compaction_start',
  'compaction_end',
  'extension_error',
]);

export function buildConversation(events) {
  const items = [];
  let assistant = null;

  const finishAssistant = (time) => {
    if (!assistant) return;
    assistant.endedAt = time ?? assistant.endedAt;
    if (assistant.thinking || assistant.text || assistant.tools.length || assistant.notices.length) {
      items.push(assistant);
    }
    assistant = null;
  };

  const ensureAssistant = (event) => {
    if (!assistant) {
      assistant = {
        kind: 'assistant',
        key: `assistant-${event.sequence}`,
        sequence: event.sequence,
        startedAt: event.time,
        endedAt: event.time,
        thinking: '',
        text: '',
        tools: [],
        notices: [],
        completed: false,
      };
    }
    assistant.endedAt = event.time;
    return assistant;
  };

  for (const event of events) {
    const normalized = event.normalized;
    const type = normalized?.type;
    const data = normalized?.data ?? {};
    if (type === 'user.message') {
      finishAssistant(event.time);
      const text = String(data.text ?? '');
      if (text) {
        items.push({kind: 'user', key: `user-${event.sequence}`, sequence: event.sequence, time: event.time, text});
      }
      continue;
    }
    if (type === 'task.idle') {
      if (assistant) assistant.completed = true;
      finishAssistant(event.time);
      continue;
    }
    if (!type || !assistantEventTypes.has(type)) continue;

    const turn = ensureAssistant(event);
    if (type === 'assistant.thinking.delta') turn.thinking += String(data.delta ?? '');
    else if (type === 'assistant.text.delta') turn.text += String(data.delta ?? '');
    else if (type === 'assistant.message.completed') turn.completed = true;
    else if (type.startsWith('tool.')) updateTool(turn, event, type, data);
    else if (type === 'approval.requested') turn.notices.push({type: 'approval', label: 'Approval requested', time: event.time});
    else if (type === 'approval.resolved') turn.notices.push({type: 'approval', label: 'Approval resolved', time: event.time});
    else if (type === 'retry_start') turn.notices.push({type: 'retry', label: 'Retrying model request', time: event.time});
    else if (type === 'retry_end') turn.notices.push({type: 'retry', label: 'Model request recovered', time: event.time});
    else if (type === 'compaction_start') turn.notices.push({type: 'compact', label: 'Compacting context', time: event.time});
    else if (type === 'compaction_end') turn.notices.push({type: 'compact', label: 'Context compacted', time: event.time});
    else if (type === 'extension_error') turn.notices.push({type: 'error', label: String(data.error ?? 'Extension error'), time: event.time});
  }
  finishAssistant(events[events.length - 1]?.time);
  return items;
}

function updateTool(turn, event, type, data) {
  const id = String(data.toolCallId ?? `tool-${event.sequence}`);
  let tool = turn.tools.find((candidate) => candidate.id === id);
  if (!tool) {
    tool = {
      id,
      name: String(data.toolName ?? 'Tool'),
      status: 'running',
      isError: false,
      startedAt: event.time,
      endedAt: event.time,
      input: eventPreview(event.event, ['input', 'args', 'arguments', 'params']),
      output: '',
    };
    turn.tools.push(tool);
  }
  tool.endedAt = event.time;
  if (type === 'tool.completed') {
    tool.status = 'completed';
    tool.isError = Boolean(data.isError);
    tool.output = eventPreview(event.event, ['result', 'output', 'error']);
  }
}

function eventPreview(event, fields) {
  if (!event || typeof event !== 'object') return '';
  for (const field of fields) {
    if (!(field in event)) continue;
    const value = event[field];
    if (typeof value === 'string') return value.slice(0, 12000);
    try {
      return JSON.stringify(value, null, 2).slice(0, 12000);
    } catch {
      return String(value).slice(0, 12000);
    }
  }
  return '';
}

export function elapsedLabel(start, end) {
  const milliseconds = Math.max(0, new Date(end).getTime() - new Date(start).getTime());
  if (milliseconds < 1000) return '<1s';
  if (milliseconds < 60_000) return `${Math.round(milliseconds / 1000)}s`;
  const minutes = Math.floor(milliseconds / 60_000);
  const seconds = Math.round((milliseconds % 60_000) / 1000);
  return seconds ? `${minutes}m ${seconds}s` : `${minutes}m`;
}

export function recentEventsAfter(lastSequence, windowSize = 400) {
  const sequence = Number.isFinite(lastSequence) ? Math.max(0, Math.floor(lastSequence)) : 0;
  const window = Number.isFinite(windowSize) ? Math.max(1, Math.floor(windowSize)) : 400;
  return Math.max(0, sequence - window);
}
