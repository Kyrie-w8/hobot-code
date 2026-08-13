import type {ExtensionCatalog, ExtensionEntry} from './types';

export type ExtensionTargetState = {state: 'required' | 'included' | 'listed' | 'unavailable'; label: string};
export function extensionKindLabel(kind: string): string;
export function extensionTargetState(entry: ExtensionEntry, boardId?: string): ExtensionTargetState;
export function extensionCatalogHealth(catalog: ExtensionCatalog): {healthy: boolean; issues: string[]};
export function extensionCatalogSummary(catalog: ExtensionCatalog, boardId?: string): {total: number; supported: number; required: number; skills: number};
export function filterExtensions(entries: ExtensionEntry[], query?: string, kind?: string): ExtensionEntry[];
