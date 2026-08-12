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
    if (assistant.thinking || assistant.text || assistant.tools.length || assistant.notices.length || assistant.failure) {
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
        failure: null,
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
        const attachments = Array.isArray(data.attachments)
          ? data.attachments.filter((item) => item && typeof item === 'object').map((item) => ({
            name: typeof item.name === 'string' ? item.name : '',
            mimeType: typeof item.mimeType === 'string' ? item.mimeType : 'image',
          }))
          : [];
        items.push({kind: 'user', key: `user-${event.sequence}`, sequence: event.sequence, time: event.time, text, attachments});
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
    else if (type === 'assistant.message.completed') {
      turn.completed = true;
      if (data.errorMessage) turn.failure = failurePresentation(String(data.errorMessage));
    }
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

export function failurePresentation(value) {
  const message = String(value ?? '').replace(/^Error:\s*/i, '').slice(0, 8192);
  const normalized = message.toLowerCase();
  if (/unsupported model|model.*not (found|available)|invalid.*model|unknown model/.test(normalized)) {
    return {category: 'model', title: 'The selected model is unavailable', message: 'Check this model or choose another one, then try again.'};
  }
  if (/unauthori[sz]ed|forbidden|invalid.*(token|credential|api key)|authentication|\b401\b|\b403\b/.test(normalized)) {
    return {category: 'authentication', title: 'Model authentication failed', message: 'Check the board model credentials, then try again.'};
  }
  if (/rate.?limit|too many requests|\b429\b|quota/.test(normalized)) {
    return {category: 'rate-limit', title: 'The model is temporarily busy', message: 'Wait a moment, then try again.'};
  }
  if (/timed? out|deadline exceeded|stream ended|message_stop|connection|network|gateway|\b5\d\d\b/.test(normalized)) {
    return {category: 'connection', title: 'The model connection was interrupted', message: 'Check model availability, then retry this request.'};
  }
  return {category: 'unknown', title: 'This response could not be completed', message: 'Retry the request. If it fails again, check the selected model.'};
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
