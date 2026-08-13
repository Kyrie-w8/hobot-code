import type {ConnectionCompatibility} from './types';

export type CompatibilityPresentation = {
  label: string;
  title: string;
  description: string;
  action: string;
  tone: 'healthy' | 'warning' | 'danger';
};

export function compatibilityPresentation(compatibility: ConnectionCompatibility): CompatibilityPresentation;
export function compatibilityTargetLabel(compatibility: ConnectionCompatibility): string;
