const finishingStates = new Set(['running', 'starting', 'stopping']);

export function taskAttention(previous, current, selected = false) {
  if (!previous || !current || selected || previous === current) return '';
  if (current === 'waiting') return 'Approval needed';
  if (current === 'failed') return 'Task failed';
  if (current === 'interrupted') return 'Task interrupted';
  if (previous === 'queued' && ['starting', 'running'].includes(current)) return 'Queued task started';
  if (previous === 'queued' && current === 'idle') return 'Task completed';
  if (current === 'idle' && finishingStates.has(previous)) return 'Task completed';
  return '';
}
