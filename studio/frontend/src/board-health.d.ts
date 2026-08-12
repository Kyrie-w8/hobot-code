import type {SystemSnapshot} from './types';

export type HealthTone = 'neutral' | 'healthy' | 'warning' | 'danger';
export type HealthIssue = {tone: 'warning' | 'danger'; label: string};

export function boardHealth(snapshot: SystemSnapshot | null): {tone: HealthTone; issues: HealthIssue[]};
export function capacityPair(available: number, total: number): string;
export function durationLabel(seconds: number): string;
export function maximumTemperature(snapshot: SystemSnapshot): string;
export function temperatureTone(snapshot: SystemSnapshot): HealthTone;
export function bpuCoreLabel(snapshot: SystemSnapshot): string;
export function loadLabel(snapshot: SystemSnapshot): string;
