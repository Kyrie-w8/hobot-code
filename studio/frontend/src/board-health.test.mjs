import assert from 'node:assert/strict';
import test from 'node:test';

import {acceleratorMemoryMetrics, activeDDRBandwidth, boardHealth, bpuCoreLabel, bpuFrequency, bpuTemperature, bpuUnavailableReason, bpuUtilization, capacityPair, durationLabel, systemResourceMetrics} from './board-health.js';

function snapshot(overrides = {}) {
  return {
    boardId: 's100',
    thermalZones: [{name: 'cpu', celsius: 54}],
    memory: {totalBytes: 8 * 1024 ** 3, availableBytes: 4 * 1024 ** 3},
    disk: {totalBytes: 64 * 1024 ** 3, availableBytes: 32 * 1024 ** 3},
    cpuCores: 6,
    loadAverage: [2.5, 2, 1],
    bpuDevices: ['/dev/bpu', '/dev/bpu_core0'],
    bpuCores: [{index: 0, name: 'BPU 0', utilizationPercent: 42, currentFrequencyHz: 1_000_000_000, maximumFrequencyHz: 1_500_000_000}],
    aiMemory: {available: true, bpuAllocationAvailable: true, ionAvailable: true, cmaAvailable: false, dmaBufAvailable: true, bpuAllocatedBytes: 32 * 1024 ** 2, ionAllocatedBytes: 128 * 1024 ** 2, dmaBufBytes: 4 * 1024 ** 2},
    rdkUtilities: {hrt_model_exec: true},
    ...overrides,
  };
}

test('healthy RDK reports no readiness issues', () => {
  assert.deepEqual(boardHealth(snapshot()), {tone: 'healthy', issues: []});
  assert.equal(bpuCoreLabel(snapshot()), '1 core ready');
  assert.deepEqual(bpuUtilization(snapshot()), {available: true, average: 42, peak: 42, peakCore: 0});
  assert.equal(bpuTemperature(snapshot({thermalZones: [{name: 'pvt_bpu', celsius: 52.5}]})), '52.5 C');
  assert.equal(bpuFrequency(snapshot()), '1 GHz / 1.5 GHz');
});

test('thermal, storage, memory, and BPU failures produce actionable issues', () => {
  const result = boardHealth(snapshot({
    thermalZones: [{name: 'bpu', celsius: 87}],
    memory: {totalBytes: 8 * 1024 ** 3, availableBytes: 300 * 1024 ** 2},
    disk: {totalBytes: 64 * 1024 ** 3, availableBytes: 500 * 1024 ** 2},
    bpuDevices: [],
    rdkUtilities: {hrt_model_exec: false},
  }));
  assert.equal(result.tone, 'danger');
  assert.equal(result.issues.length, 5);
  assert.match(result.issues.map((issue) => issue.label).join('\n'), /cooling|Memory|storage|BPU|hrt_model_exec/);
});

test('capacity and uptime labels stay compact', () => {
  assert.equal(capacityPair(3.5 * 1024 ** 3, 8 * 1024 ** 3), '3.5 GiB / 8 GiB');
  assert.equal(durationLabel(183_600), '2d 3h');
  assert.equal(acceleratorMemoryMetrics(snapshot({accelerator: {available: true, hbmemPools: [{name: 'cma_reserved', totalBytes: 1024 ** 3, usedBytes: 64 * 1024, freeBytes: 1024 ** 3 - 64 * 1024}]}}))[0].value, '64 KiB / 1 GiB used');
});

test('resource metrics use comparable capacity percentages', () => {
  const metrics = systemResourceMetrics(snapshot());
  assert.deepEqual(metrics.map(({key, percent}) => [key, percent]), [
    ['cpu', 41.66666666666667], ['memory', 50], ['disk', 50], ['temperature', 54],
  ]);
  assert.equal(metrics[1].value, '4 GiB / 8 GiB used');
  assert.equal(metrics[3].tone, 'healthy');
});

test('accelerator memory uses official Hbmem pools when available', () => {
  const official = acceleratorMemoryMetrics(snapshot({accelerator: {available: true, source: 'ion-debugfs', ddrReadMiBps: 320, ddrWriteMiBps: 120, hbmemPools: [
    {name: 'cma_reserved', totalBytes: 1024 ** 3, usedBytes: 256 * 1024 ** 2, freeBytes: 768 * 1024 ** 2, processBytes: 64 * 1024 ** 2, systemBytes: 192 * 1024 ** 2},
    {name: 'carveout', totalBytes: 512 * 1024 ** 2, usedBytes: 64 * 1024 ** 2, freeBytes: 448 * 1024 ** 2, processBytes: 48 * 1024 ** 2, systemBytes: 16 * 1024 ** 2},
  ]}}));
  assert.deepEqual(official.map(({label, percent}) => [label, percent]), [['BPU / codec memory', 12.5], ['VIO / system buffers', 25]]);
  assert.equal(official[0].detail, '48 MiB apps · 16 MiB system');
  assert.deepEqual(activeDDRBandwidth(snapshot({accelerator: {available: true, ddrReadMiBps: 320, ddrWriteMiBps: 120}})), {read: 320, write: 120});
  assert.equal(activeDDRBandwidth(snapshot({bpuCores: [{index: 0, utilizationPercent: 0}], accelerator: {available: true, ddrReadMiBps: 320}})), null);
  assert.equal(activeDDRBandwidth(snapshot({accelerator: {available: true, ddrReadMiBps: 0, ddrWriteMiBps: 0}})), null);
});

test('monitor fallback does not present estimated ownership as exact', () => {
  const metrics = acceleratorMemoryMetrics(snapshot({accelerator: {available: true, source: 'hrt_ucp_monitor-estimate', hbmemPools: [
    {name: 'carveout', totalBytes: 512 * 1024 ** 2, usedBytes: 1 * 1024 ** 2, freeBytes: 511 * 1024 ** 2},
  ]}}));
  assert.equal(metrics[0].detail, undefined);
});

test('accelerator memory falls back without inventing a capacity', () => {
  const metrics = acceleratorMemoryMetrics(snapshot());
  assert.deepEqual(metrics.map((metric) => metric.key), ['ion']);
  const cma = acceleratorMemoryMetrics(snapshot({aiMemory: {available: true, ionAvailable: false, bpuAllocationAvailable: false, cmaAvailable: true, cmaFreeBytes: 256 * 1024 ** 2, cmaTotalBytes: 1024 ** 3, dmaBufAvailable: false}}));
  assert.deepEqual(cma, [{key: 'cma', label: 'CMA available', value: '256 MiB / 1 GiB', percent: 25, available: true}]);
});

test('multi-core BPU summaries and unavailable metrics stay honest', () => {
  const multi = snapshot({bpuCores: [
    {index: 0, name: 'BPU 0', utilizationPercent: 15},
    {index: 1, name: 'BPU 1', utilizationPercent: 75},
  ], thermalZones: [{name: 'cpu', celsius: 60}], aiMemory: {available: false}});
  assert.deepEqual(bpuUtilization(multi), {available: true, average: 45, peak: 75, peakCore: 1});
  assert.equal(bpuTemperature(multi), 'Not exposed');
  assert.equal(bpuFrequency(snapshot({bpuCores: undefined})), 'Not reported');
});

test('BPU fallback states explain version and hardware differences', () => {
  assert.match(bpuUnavailableReason(snapshot({bpuCores: undefined, bpuTelemetry: undefined})), /too old/);
  assert.match(bpuUnavailableReason(snapshot({bpuCores: [], bpuTelemetry: {status: 'metrics-not-exposed'}})), /RDK OS/);
  assert.match(bpuUnavailableReason(snapshot({bpuDevices: [], bpuCores: [], bpuTelemetry: {status: 'device-not-detected'}})), /No BPU device/);
});
