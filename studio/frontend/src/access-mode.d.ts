export type AccessModeInput = {
  permissionMode?: 'review' | 'ask' | 'developer';
  sandboxMode?: 'review' | 'workspace' | 'system' | 'off';
  networkMode?: 'shared' | 'model-only' | 'offline';
};

export function accessModePresentation(input?: AccessModeInput): {
  label: string;
  tone: 'standard' | 'elevated' | 'danger';
  summary: string;
};
