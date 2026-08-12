import type {TaskWatchStatus} from './api';

export function isCurrentRequest(request: number, current: number): boolean;
export function isCurrentTarget(board: string, task: string, currentBoard: string, currentTask: string): boolean;
export function watchRetryDelay(attempt: number, maximum?: number): number;
export function watchStatusLabel(status: TaskWatchStatus | null): string;
