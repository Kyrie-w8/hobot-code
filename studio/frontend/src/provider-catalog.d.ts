import type {ManagedProvider, ModelOption} from './types';

export type IncludedProviderGroup = {id: string; name: string; models: ModelOption[]};

export function includedProviderGroups(models: ModelOption[], managedProviders?: ManagedProvider[]): IncludedProviderGroup[];
export function includedModelSummary(model: ModelOption): string;
