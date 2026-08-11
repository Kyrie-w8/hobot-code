import type {Task} from './types';

export type ComposerMode = 'send' | 'resume' | 'restart';

export const terminalStatuses: ReadonlySet<string>;
export function composerMode(task: Pick<Task, 'status' | 'sessionFile'>): ComposerMode;
export function composerIsBlocked(status: string): boolean;
export function shouldSubmitComposer(key: string, shiftKey: boolean, isComposing: boolean): boolean;
