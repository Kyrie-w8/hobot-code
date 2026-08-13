import type {WorkspaceChangeFile, WorkspaceChanges, WorkspaceDelivery} from './types';

export const maximumRenderedDiffLines: number;
export function workspaceChangeSummary(changes: WorkspaceChanges): {title: string; detail: string};
export function workspaceDiffLines(patch: string, maximum?: number): {lines: Array<{key: string; text: string; kind: 'meta' | 'hunk' | 'added' | 'deleted' | 'context'}>; truncated: boolean};
export function workspaceChangeLabel(file: WorkspaceChangeFile): string;
export function workspaceDeliverySummary(delivery: WorkspaceDelivery | null): {tone: 'success' | 'ready' | 'blocked'; title: string; detail: string} | null;
