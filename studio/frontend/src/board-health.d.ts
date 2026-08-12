import type {SystemSnapshot} from './types';

export type HealthTone = 'neutral' | 'healthy' | 'warning' | 'danger';
export type HealthIssue = {tone: 'warning' | 'danger'; label: string};

export function boardHealth(snapshot: SystemSnapshot | null): {tone: HealthTone; issues: HealthIssue[]};
export function capacityPair(available: number, total: number): string;
export function durationLabel(seconds: number): string;
export function maximumTemperature(snapshot: SystemSnapshot): string;
export function temperatureTone(snapshot: SystemSnapshot): HealthTone;
export function bpuCoreLabel(snapshot: SystemSnapshot): string;
export function bpuUtilization(snapshot: SystemSnapshot): {available: boolean; average: number; peak: number; peakCore: number};
export function bpuUnavailableReason(snapshot: SystemSnapshot): string;
export function bpuTemperature(snapshot: SystemSnapshot): string;
export function bpuFrequency(snapshot: SystemSnapshot): string;
export function ionMemoryLabel(snapshot: SystemSnapshot): string;
export function percentLabel(value: number): string;
export function formatBytes(value: number): string;
export function loadLabel(snapshot: SystemSnapshot): string;
