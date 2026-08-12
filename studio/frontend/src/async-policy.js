export function isCurrentRequest(request, current) {
  return Number.isInteger(request) && request === current;
}

export function isCurrentTarget(board, task, currentBoard, currentTask) {
  return Boolean(board && task && board === currentBoard && task === currentTask);
}

export function watchRetryDelay(attempt, maximum = 15000) {
  const boundedAttempt = Math.max(1, Math.min(31, Number.isFinite(attempt) ? Math.floor(attempt) : 1));
  return Math.min(maximum, 1000 * 2 ** (boundedAttempt - 1));
}

export function watchStatusLabel(status) {
  if (status?.state === 'failed') return 'Live updates paused - retrying';
  if (status?.state === 'reconnecting') return 'Live updates reconnecting';
  return '';
}
