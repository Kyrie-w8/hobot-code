import assert from 'node:assert/strict';
import test from 'node:test';

import {boardHealth, capacityPair, durationLabel, maximumTemperature, temperatureTone} from './board-health.js';

function snapshot(overrides = {}) {
  return {
    boardId: 's100',
    thermalZones: [{name: 'cpu', celsius: 54}],
    memory: {totalBytes: 8 * 1024 ** 3, availableBytes: 4 * 1024 ** 3},
    disk: {totalBytes: 64 * 1024 ** 3, availableBytes: 32 * 1024 ** 3},
    bpuDevices: ['/dev/bpu0'],
    rdkUtilities: {hrt_model_exec: true},
    ...overrides,
  };
}

test('healthy RDK reports no readiness issues', () => {
  assert.deepEqual(boardHealth(snapshot()), {tone: 'healthy', issues: []});
  assert.equal(maximumTemperature(snapshot()), '54.0 C');
  assert.equal(temperatureTone(snapshot()), 'healthy');
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
