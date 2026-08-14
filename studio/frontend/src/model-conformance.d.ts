import type {ModelConformance, ModelOption} from './types';

export function currentModelConformance(result: ModelConformance | null | undefined, model: ModelOption | null | undefined, now?: number): ModelConformance | undefined;
export function modelConformancePresentation(result: ModelConformance | null | undefined, probing?: boolean): {label: string; title: string};
