import type {Approval} from './types';

export function approvalPresentation(approval: Approval): {
  title: string;
  detail: string;
  remembersExactCall: boolean;
};
