import type {ModelOption, ModelRDKMatrix, ModelRDKProfileStatus} from './types';

export function currentModelRDKMatrix(matrix: ModelRDKMatrix | null | undefined, model: ModelOption | undefined): ModelRDKMatrix | undefined;
export function rdkProfileState(profile: ModelRDKProfileStatus | undefined, activeProfile?: string): 'idle' | 'planned' | 'unsupported' | 'running' | 'stale' | 'failed' | 'passed' | 'partial';
export function rdkProfileEvidenceLabel(profile: ModelRDKProfileStatus): string;
