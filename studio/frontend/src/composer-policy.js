export const terminalStatuses = new Set(['stopped', 'failed', 'interrupted']);

export function composerMode(task) {
  if (!terminalStatuses.has(task.status)) return 'send';
  if (task.failure?.recovery === 'restart') return 'restart';
  if (task.failure?.recovery === 'resume' && task.sessionFile) return 'resume';
  return task.sessionFile ? 'resume' : 'restart';
}

export function composerIsBlocked(status) {
  return ['queued', 'starting', 'running', 'waiting', 'stopping'].includes(status);
}

export function shouldSubmitComposer(key, shiftKey, isComposing) {
  return key === 'Enter' && !shiftKey && !isComposing;
}

export function turnCancellationMode(status) {
  if (status === 'queued') return 'stop';
  if (['starting', 'running', 'waiting'].includes(status)) return 'abort';
  return undefined;
}

export function shouldCancelTurnShortcut(key, isComposing, repeat, status) {
  return key === 'Escape' && !isComposing && !repeat && Boolean(turnCancellationMode(status));
}
