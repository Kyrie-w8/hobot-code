import assert from 'node:assert/strict';
import test from 'node:test';

import {aiMemoryLabel, boardHealth, bpuCoreLabel, bpuFrequency, bpuTemperature, bpuUtilization, capacityPair, durationLabel, loadLabel, maximumTemperature, temperatureTone} from './board-health.js';

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
  assert.equal(maximumTemperature(snapshot()), '54.0 C');
  assert.equal(temperatureTone(snapshot()), 'healthy');
  assert.equal(bpuCoreLabel(snapshot()), '1 core ready');
  assert.deepEqual(bpuUtilization(snapshot()), {available: true, average: 42, peak: 42, peakCore: 0});
  assert.equal(bpuTemperature(snapshot({thermalZones: [{name: 'pvt_bpu', celsius: 52.5}]})), '52.5 C');
  assert.equal(bpuFrequency(snapshot()), '1 GHz / 1.5 GHz');
  assert.equal(aiMemoryLabel(snapshot()), '32 MiB BPU');
  assert.equal(loadLabel(snapshot()), '2.50 / 6');
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
});

test('multi-core BPU summaries and unavailable metrics stay honest', () => {
  const multi = snapshot({bpuCores: [
    {index: 0, name: 'BPU 0', utilizationPercent: 15},
    {index: 1, name: 'BPU 1', utilizationPercent: 75},
  ], thermalZones: [{name: 'cpu', celsius: 60}], aiMemory: {available: false}});
  assert.deepEqual(bpuUtilization(multi), {available: true, average: 45, peak: 75, peakCore: 1});
  assert.equal(bpuTemperature(multi), 'Unavailable');
  assert.equal(aiMemoryLabel(multi), 'Unavailable');
});
