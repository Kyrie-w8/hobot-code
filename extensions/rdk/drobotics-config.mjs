export const DEFAULT_GATEWAY_TIMEOUT_MS = 3_000_000;

export function normalizeGatewayTimeout(value, fallback = DEFAULT_GATEWAY_TIMEOUT_MS) {
  const candidate = value === undefined || value === null || value === "" ? fallback : Number(value);
  return Number.isFinite(candidate)
    ? Math.max(1_000, Math.min(Math.round(candidate), 3_600_000))
    : fallback;
}

export function resolveGatewayTimeout(environmentValue, optionValue) {
  return normalizeGatewayTimeout(environmentValue ?? optionValue);
}
