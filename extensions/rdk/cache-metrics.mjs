import { createHash } from "node:crypto";

const MAX_RECENT_OBSERVATIONS = 32;

let observations = [];
let totals = emptyTotals();

function emptyTotals() {
  return {
    requests: 0,
    input: 0,
    cacheRead: 0,
    cacheWrite: 0,
    transitions: 0,
    prefixChanges: 0,
  };
}

function tokenCount(value) {
  return Number.isSafeInteger(value) && value >= 0 ? value : 0;
}

function hashText(value) {
  return createHash("sha256").update(value).digest("hex");
}

function percent(numerator, denominator) {
  return denominator > 0 ? (numerator / denominator) * 100 : 0;
}

function formatPercent(value) {
  return `${value.toFixed(1)}%`;
}

function formatTokens(value) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(value);
}

export function calculateCacheHitRate(usage) {
  const input = tokenCount(usage?.input);
  const cacheRead = tokenCount(usage?.cacheRead);
  const cacheWrite = tokenCount(usage?.cacheWrite);
  return percent(cacheRead, input + cacheRead + cacheWrite);
}

export function cacheContractFingerprints(systemPrompt, tools) {
  const system = typeof systemPrompt === "string" ? systemPrompt : "";
  const serializedTools = JSON.stringify(Array.isArray(tools) ? tools : []);
  const systemFingerprint = hashText(system);
  const toolsFingerprint = hashText(serializedTools);
  return {
    systemFingerprint,
    toolsFingerprint,
    contractFingerprint: hashText(`${systemFingerprint}\0${toolsFingerprint}`),
  };
}

export function resetCacheMetrics() {
  observations = [];
  totals = emptyTotals();
}

export function recordCacheObservation({ model, usage, systemPrompt, tools }) {
  const input = tokenCount(usage?.input);
  const cacheRead = tokenCount(usage?.cacheRead);
  const cacheWrite = tokenCount(usage?.cacheWrite);
  const output = tokenCount(usage?.output);
  const fingerprints = cacheContractFingerprints(systemPrompt, tools);
  const normalizedModel = typeof model === "string" && model ? model : "unknown";
  const previous = observations.at(-1);
  const prefixStable = previous
    ? previous.model === normalizedModel && previous.contractFingerprint === fingerprints.contractFingerprint
    : null;
  const observation = {
    timestamp: new Date().toISOString(),
    model: normalizedModel,
    input,
    output,
    cacheRead,
    cacheWrite,
    totalInput: input + cacheRead + cacheWrite,
    hitRate: calculateCacheHitRate({ input, cacheRead, cacheWrite }),
    ...fingerprints,
    prefixStable,
  };
  totals.requests += 1;
  totals.input += input;
  totals.cacheRead += cacheRead;
  totals.cacheWrite += cacheWrite;
  if (previous) totals.transitions += 1;
  if (prefixStable === false) totals.prefixChanges += 1;
  observations.push(observation);
  if (observations.length > MAX_RECENT_OBSERVATIONS) observations.shift();
  return { ...observation };
}

export function getCacheMetrics() {
  const { requests, input, cacheRead, cacheWrite, transitions, prefixChanges } = totals;
  const totalInput = input + cacheRead + cacheWrite;
  return {
    requests,
    input,
    cacheRead,
    cacheWrite,
    totalInput,
    hitRate: percent(cacheRead, totalInput),
    transitions,
    prefixChanges,
    latest: observations.length > 0 ? { ...observations.at(-1) } : undefined,
    recent: observations.map((item) => ({ ...item })),
  };
}

export function formatCacheMetrics() {
  const metrics = getCacheMetrics();
  if (metrics.requests === 0) {
    return [
      "Cache: no completed D-Robotics model requests observed in this process.",
      "Run at least two turns, then use /cache again.",
    ].join("\n");
  }
  const latest = metrics.latest;
  const prefix = metrics.transitions === 0
    ? "contract baseline recorded"
    : `${metrics.prefixChanges} route/contract change(s) across ${metrics.transitions} transition(s)`;
  return [
    `Cache: ${metrics.requests} request(s) | model ${latest.model}`,
    `Hit rate: ${formatPercent(metrics.hitRate)} aggregate | ${formatPercent(latest.hitRate)} latest`,
    `Input: ${formatTokens(metrics.totalInput)} total | ${formatTokens(metrics.cacheRead)} read | ${formatTokens(metrics.cacheWrite)} write | ${formatTokens(metrics.input)} uncached`,
    `Prefix stability: ${prefix}`,
    `Fingerprints: system ${latest.systemFingerprint.slice(0, 12)} | tools ${latest.toolsFingerprint.slice(0, 12)}`,
    "Metric: cacheRead / (input + cacheRead + cacheWrite). Hashes contain no prompt or tool content.",
  ].join("\n");
}
