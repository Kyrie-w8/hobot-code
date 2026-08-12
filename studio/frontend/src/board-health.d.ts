import type {SystemSnapshot} from './types';

export type HealthTone = 'neutral' | 'healthy' | 'warning' | 'danger';
export type HealthIssue = {tone: 'warning' | 'danger'; label: string};
export type ResourceMetric = {key: string; label: string; value: string; percent: number; tone?: HealthTone};
export type AcceleratorMemoryMetric = {key: string; label: string; value: string; percent?: number; available?: boolean};

export function boardHealth(snapshot: SystemSnapshot | null): {tone: HealthTone; issues: HealthIssue[]};
export function capacityPair(available: number, total: number): string;
export function systemResourceMetrics(snapshot: SystemSnapshot): ResourceMetric[];
export function acceleratorMemoryMetrics(snapshot: SystemSnapshot): AcceleratorMemoryMetric[];
export function activeDDRBandwidth(snapshot: SystemSnapshot): {read: number; write: number} | null;
export function durationLabel(seconds: number): string;
export function bpuCoreLabel(snapshot: SystemSnapshot): string;
export function bpuUtilization(snapshot: SystemSnapshot): {available: boolean; average: number; peak: number; peakCore: number};
export function bpuUnavailableReason(snapshot: SystemSnapshot): string;
export function bpuTemperature(snapshot: SystemSnapshot): string;
export function bpuFrequency(snapshot: SystemSnapshot): string;
export function percentLabel(value: number): string;
export function formatBytes(value: number): string;
