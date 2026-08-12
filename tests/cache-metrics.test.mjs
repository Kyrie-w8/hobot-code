import assert from "node:assert/strict";
import test from "node:test";

import {
  cacheContractFingerprints,
  calculateCacheHitRate,
  formatCacheMetrics,
  getCacheMetrics,
  recordCacheObservation,
  resetCacheMetrics,
} from "../extensions/rdk/cache-metrics.mjs";

test.beforeEach(() => resetCacheMetrics());

test("calculates hit rate from uncached, read, and written input", () => {
  assert.equal(calculateCacheHitRate({ input: 100, cacheRead: 800, cacheWrite: 100 }), 80);
  assert.equal(calculateCacheHitRate({ input: -1, cacheRead: Number.POSITIVE_INFINITY, cacheWrite: 0 }), 0);
});

test("aggregates cold and warm requests without counting output tokens", () => {
  recordCacheObservation({
    model: "kimi-k3",
    usage: { input: 100, output: 50, cacheRead: 0, cacheWrite: 900 },
    systemPrompt: "stable system",
    tools: [{ name: "read" }],
  });
  recordCacheObservation({
    model: "kimi-k3",
    usage: { input: 10, output: 500, cacheRead: 990, cacheWrite: 0 },
    systemPrompt: "stable system",
    tools: [{ name: "read" }],
  });

  const metrics = getCacheMetrics();
  assert.equal(metrics.requests, 2);
  assert.equal(metrics.totalInput, 2000);
  assert.equal(metrics.cacheRead, 990);
  assert.equal(metrics.hitRate, 49.5);
  assert.equal(metrics.latest.hitRate, 99);
  assert.equal(metrics.prefixChanges, 0);
  assert.match(formatCacheMetrics(), /49\.5% aggregate \| 99\.0% latest/);
});

test("fingerprints exact system and ordered tool contracts without retaining their content", () => {
  const baseline = cacheContractFingerprints("system", [{ name: "read" }, { name: "edit" }]);
  const same = cacheContractFingerprints("system", [{ name: "read" }, { name: "edit" }]);
  const reordered = cacheContractFingerprints("system", [{ name: "edit" }, { name: "read" }]);
  const changedSystem = cacheContractFingerprints("system changed", [{ name: "read" }, { name: "edit" }]);

  assert.deepEqual(same, baseline);
  assert.notEqual(reordered.contractFingerprint, baseline.contractFingerprint);
  assert.notEqual(changedSystem.contractFingerprint, baseline.contractFingerprint);
  assert.equal(Object.values(baseline).every((value) => /^[a-f0-9]{64}$/.test(value)), true);
});

test("reports contract changes between consecutive observations", () => {
  recordCacheObservation({ model: "kimi-k3", usage: { input: 10 }, systemPrompt: "one", tools: [] });
  recordCacheObservation({ model: "kimi-k3", usage: { input: 10 }, systemPrompt: "two", tools: [] });

  const metrics = getCacheMetrics();
  assert.equal(metrics.transitions, 1);
  assert.equal(metrics.prefixChanges, 1);
  assert.equal(metrics.latest.prefixStable, false);
});

test("treats a model route change as a cache-prefix change", () => {
  recordCacheObservation({ model: "kimi-k3", usage: { input: 10 }, systemPrompt: "same", tools: [] });
  recordCacheObservation({ model: "glm-5.2", usage: { input: 10 }, systemPrompt: "same", tools: [] });

  assert.equal(getCacheMetrics().prefixChanges, 1);
});
