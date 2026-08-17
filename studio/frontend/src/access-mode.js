const permissionLabels = {
  review: 'Review only',
  ask: 'Ask first',
  'auto-review': 'Approve for me',
  developer: 'Developer',
};

const sandboxLabels = {
  review: 'Read only',
  workspace: 'Workspace',
  system: 'Board hardware',
  off: 'No sandbox',
};

const networkLabels = {
  shared: 'Network',
  'model-only': 'Model only',
  offline: 'Offline',
};

export function accessModePresentation({permissionMode = 'ask', sandboxMode = 'workspace', networkMode = 'shared'} = {}) {
  let label = permissionLabels[permissionMode] || 'Custom';
  let tone = 'standard';
  if (sandboxMode === 'off') {
    label = 'Unrestricted';
    tone = 'danger';
  } else if (sandboxMode === 'system') {
    label = 'Board access';
    tone = 'elevated';
  } else if (permissionMode === 'review' && sandboxMode === 'review') {
    label = 'Review only';
  } else if (permissionMode === 'developer' && sandboxMode === 'workspace') {
    label = 'Developer';
  } else if (permissionMode === 'ask' && sandboxMode === 'workspace') {
    label = 'Ask first';
  } else if (permissionMode === 'auto-review' && ['review', 'workspace'].includes(sandboxMode)) {
    label = 'Approve for me';
  } else {
    label = 'Custom';
  }
  return {
    label,
    tone,
    summary: [
      permissionLabels[permissionMode] || permissionMode,
      sandboxLabels[sandboxMode] || sandboxMode,
      networkLabels[networkMode] || networkMode,
    ].join(' · '),
  };
}
