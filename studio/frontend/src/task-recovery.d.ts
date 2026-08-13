import type {Task} from './types';

export type TaskRecoveryPresentation = {
  title: string;
  message: string;
  recovery: 'resume' | 'restart' | 'check-model' | 'diagnose';
  action: null | {label: string; prompt?: string};
};

export function taskRecovery(task: Task | null | undefined): TaskRecoveryPresentation | null;
export function taskRecoveryActionAvailable(recovery: TaskRecoveryPresentation['recovery'], canCheckModel: boolean, canDiagnose: boolean): boolean;
