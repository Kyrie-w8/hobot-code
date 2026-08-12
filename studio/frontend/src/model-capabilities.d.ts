import type {ModelOption} from './types';

export function effectiveModel(models: ModelOption[], selection: string): ModelOption | undefined;
export function modelAcceptsImages(models: ModelOption[], selection: string): boolean;
