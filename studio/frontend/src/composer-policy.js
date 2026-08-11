export const terminalStatuses = new Set(['stopped', 'failed', 'interrupted']);

export function composerMode(task) {
  if (!terminalStatuses.has(task.status)) return 'send';
  return task.sessionFile ? 'resume' : 'restart';
}

export function composerIsBlocked(status) {
  return ['starting', 'running', 'waiting', 'stopping'].includes(status);
}

export function shouldSubmitComposer(key, shiftKey, isComposing) {
  return key === 'Enter' && !shiftKey && !isComposing;
}
