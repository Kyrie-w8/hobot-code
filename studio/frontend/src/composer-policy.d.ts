import type {Task} from './types';

export type ComposerMode = 'send' | 'resume' | 'restart';
export type TurnCancellationMode = 'abort' | 'stop';

export const terminalStatuses: ReadonlySet<string>;
export function composerMode(task: Pick<Task, 'status' | 'sessionFile'>): ComposerMode;
export function composerIsBlocked(status: string, supportsFollowup?: boolean): boolean;
export function shouldSubmitComposer(key: string, shiftKey: boolean, isComposing: boolean): boolean;
export function turnCancellationMode(status?: string): TurnCancellationMode | undefined;
export function shouldCancelTurnShortcut(key: string, isComposing: boolean, repeat: boolean, status?: string): boolean;
