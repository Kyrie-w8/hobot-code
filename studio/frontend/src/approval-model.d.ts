import type {Approval} from './types';

export function approvalPresentation(approval: Approval): {
  title: string;
  detail: string;
  remembersExactCall: boolean;
};
export function approvalResponse(method: Approval['method'], action: 'confirm' | 'deny' | 'cancel' | 'submit' | 'select', value?: string): Record<string, unknown>;
