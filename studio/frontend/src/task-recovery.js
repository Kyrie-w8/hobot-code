const knownFailures = {
  'queue-recovery-failed': ['Queued work could not be recovered safely.', 'restart'],
  'handoff-uncertain': ['The board service restarted while this task was starting. Review the last output before continuing.', 'session'],
  'service-restarted': ['This task was interrupted when the board service stopped or restarted.', 'session'],
  'state-persistence-failed': ['Hobot Code could not save this task state safely.', 'diagnose'],
  'model-unavailable': ['The selected model route could not complete this task.', 'check-model'],
  'worker-protocol-failed': ['The Agent worker returned an invalid response.', 'diagnose'],
  'worker-exited': ['The Agent worker exited before the task completed.', 'session'],
};

export function taskRecovery(task) {
  if (!task || !['failed', 'interrupted'].includes(task.status)) return null;
  const definition = knownFailures[task.failure?.code];
  const sessionRecovery = task.sessionFile ? 'resume' : 'restart';
  const recovery = definition?.[1] === 'session' ? sessionRecovery : definition?.[1] ?? sessionRecovery;
  const baseMessage = definition?.[0] ?? 'This task ended before completion. Review the last visible output before continuing.';
  const evidence = Array.isArray(task.turnEvidence) ? task.turnEvidence.at(-1) : null;
  let evidenceMessage = '';
  if (evidence?.openTools > 0) {
    evidenceMessage = ` ${evidence.openTools} tool action${evidence.openTools === 1 ? '' : 's'} did not report completion; side effects are unknown.`;
  } else if (evidence?.workspaceChanged === true) {
    evidenceMessage = ' Git workspace changes were detected during the interrupted turn; review Changes before continuing.';
  } else if (evidence?.workspaceChanged === false) {
    evidenceMessage = ' No Git workspace change was detected, but effects outside Git still require review.';
  } else if (evidence) {
    evidenceMessage = ' Workspace change evidence is unavailable; review the current state before continuing.';
  }
  const message = baseMessage + evidenceMessage;
  const action = {
    resume: {label: 'Prepare resume', prompt: 'Review the last output and workspace state, then continue safely without repeating completed side effects.'},
    restart: {label: 'Prepare new session', prompt: 'Start this task again. Inspect the current workspace state first and do not repeat completed side effects.'},
    'check-model': {label: 'Check model'},
    diagnose: {label: 'Save diagnostics'},
  }[recovery] ?? null;
  return {title: task.status === 'interrupted' ? 'Task interrupted' : 'Task stopped unexpectedly', message, recovery, action};
}

export function taskRecoveryActionAvailable(recovery, canCheckModel, canDiagnose) {
  if (recovery === 'check-model') return canCheckModel;
  if (recovery === 'diagnose') return canDiagnose;
  return true;
}
