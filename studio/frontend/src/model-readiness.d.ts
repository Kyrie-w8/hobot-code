import type {ModelConformance, ModelHealth, ModelOption, ModelQualification, ModelRDKProbe, ModelRuntimeProbe} from './types';

export function currentModelProbe<T extends ModelRuntimeProbe | ModelRDKProbe>(result: T | null | undefined, model: ModelOption | undefined): T | undefined;
export function currentModelQualification(result: ModelQualification | null | undefined, model: ModelOption | undefined): ModelQualification | undefined;
export function qualificationLayer<T>(qualification: ModelQualification | undefined, layer: 'route' | 'protocol' | 'runtime' | 'rdk', value: T | undefined, now?: number): T | undefined;
export function qualificationExpirations(qualification: ModelQualification | undefined, now?: number): ModelQualification['expiredLayers'];
export function qualificationEvidenceNotice(qualification: ModelQualification | undefined): string;
export function modelReadinessPresentation(input: {health?: ModelHealth; conformance?: ModelConformance; runtime?: ModelRuntimeProbe; rdk?: ModelRDKProbe; evidenceState?: ModelQualification['state']; running?: boolean}): {label: string; tone: 'idle' | 'running' | 'passed' | 'partial' | 'failed'; title: string};
