import type {TaskEvent} from './types';

export type UserConversationItem = {
  kind: 'user'; key: string; sequence: number; time: string; text: string;
  attachments: Array<{name?: string; mimeType: string; preview?: string}>;
};
export type ToolActivity = {
  id: string; name: string; status: string; isError: boolean;
  startedAt: string; endedAt: string; input: string; output: string;
};
export type AssistantConversationItem = {
  kind: 'assistant'; key: string; sequence: number; startedAt: string; endedAt: string;
  thinking: string; text: string; tools: ToolActivity[];
  notices: Array<{type: string; label: string; time: string}>; completed: boolean;
  failure: null | {category: string; title: string; message: string};
	retry: null | {attempt: number; maxAttempts: number; active: boolean; recovered: boolean; automatic: boolean};
};
export type ConversationItem = UserConversationItem | AssistantConversationItem;

export function buildConversation(events: TaskEvent[]): ConversationItem[];
export function elapsedLabel(start: string, end: string): string;
export function recentEventsAfter(lastSequence: number, windowSize?: number): number;
export function eventRetentionPresentation(retention: {
  retainedFrom?: number; retainedThrough?: number; latestSequence?: number;
  historyTruncated?: boolean; cursorExpired?: boolean;
} | null): {title: string; detail: string} | null;
export function failurePresentation(value: string): {category: string; title: string; message: string};
