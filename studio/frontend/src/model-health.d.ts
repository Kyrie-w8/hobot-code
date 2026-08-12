import type {ModelHealth, ModelOption} from './types';
export function currentModelHealth(health: ModelHealth | null, model: ModelOption | undefined, now?: number): ModelHealth | undefined;
export function modelHealthLabel(category: string): string;
