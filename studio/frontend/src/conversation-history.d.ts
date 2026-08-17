import type {TaskEvent} from './types';
import type {UserConversationItem} from './conversation-model';

export const eventPageSize: number;
export function mergeEventHistory(current: TaskEvent[], incoming: TaskEvent[]): TaskEvent[];
export function navigationEventWindow(events: TaskEvent[] | undefined): TaskEvent[];
export function eventPageContinuesAfter(after: number, events: TaskEvent[] | undefined): boolean;
export function eventPageHasLater(page: Pick<import('./types').EventPage, 'events' | 'hasMore' | 'retainedThrough' | 'latestSequence'>): boolean;
export function userMessagesFromEvents(events: TaskEvent[]): UserConversationItem[];
export function mergeMessageIndex(current: UserConversationItem[], incoming: UserConversationItem[]): UserConversationItem[];
export function navigatorGroups(messages: UserConversationItem[], maxMarkers?: number): UserConversationItem[][];
export function historyPageBefore(events: TaskEvent[], before?: number, limit?: number): import('./types').EventPage;
