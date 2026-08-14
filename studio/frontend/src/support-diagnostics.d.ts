import type {SupportBundle} from './types';

export function supportBundlePresentation(bundle: SupportBundle): {tone: 'passed' | 'partial' | 'failed'; label: string; summary: string};
