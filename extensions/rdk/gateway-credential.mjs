import { closeSync, lstatSync, readFileSync, unlinkSync } from "node:fs";
import { resolve } from "node:path";

import { resolveUserPaths } from "./user-paths.mjs";
import { parseStrictJSON } from "./strict-json.mjs";

const TOKEN_ENV = "ANTHROPIC_AUTH_TOKEN";
const TOKEN_FD_ENV = "HOBOT_CODE_GATEWAY_TOKEN_FD";
const TOKEN_FILE_ENV = "HOBOT_CODE_GATEWAY_TOKEN_FILE";
const MAX_TOKEN_BYTES = 8192;
const MAX_BUNDLE_BYTES = 512 * 1024;
const PROVIDER_TOKEN_PREFIX = "HOBOT_CODE_PROVIDER_KEY_";
const CREDENTIAL_ENV_PATTERN = /^HOBOT_CODE_PROVIDER_KEY_[A-Z0-9_]{1,96}$/u;
const CREDENTIAL_CACHE = Symbol.for("hobot-code.drobotics-gateway-credential");

function normalizeToken(value, label = "Gateway credential") {
  const token = String(value ?? "").trim();
  if (Buffer.byteLength(token) > MAX_TOKEN_BYTES) {
    throw new Error(`${label} exceeds ${MAX_TOKEN_BYTES} bytes`);
  }
  return token;
}

export function serializeGatewayCredentials(bundle) {
  const normalized = normalizeBundle(bundle);
  if (!normalized.drobotics && Object.keys(normalized.providerKeys).length === 0) return "";
  return JSON.stringify({ schemaVersion: 1, ...normalized });
}

export function captureGatewayCredential(env = process.env) {
	return captureGatewayCredentials(env).drobotics;
}

function normalizeBundle(value) {
	if (value === null || typeof value !== "object" || Array.isArray(value)) throw new Error("Gateway credential bundle is invalid");
	if (value.drobotics !== undefined && typeof value.drobotics !== "string") throw new Error("D-Robotics gateway credential is invalid");
	if (value.providerKeys !== undefined && (value.providerKeys === null || typeof value.providerKeys !== "object" || Array.isArray(value.providerKeys))) throw new Error("Managed provider credentials are invalid");
	const result = { drobotics: normalizeToken(value?.drobotics, "D-Robotics gateway credential"), providerKeys: {} };
	const entries = Object.entries(value?.providerKeys ?? {});
	if (entries.length > 64) throw new Error("Managed provider credentials exceed 64 entries");
	for (const [name, token] of entries) {
		if (!CREDENTIAL_ENV_PATTERN.test(name)) {
			throw new Error(`Managed provider credential name is invalid: ${name}`);
		}
		if (typeof token !== "string") throw new Error(`Managed provider credential ${name} is invalid`);
		const normalized = normalizeToken(token, `Managed provider credential ${name}`);
		if (!normalized) throw new Error(`Managed provider credential is empty: ${name}`);
		result.providerKeys[name] = normalized;
	}
	return result;
}

function decodeCredentialPayload(value) {
	if (value.byteLength > MAX_BUNDLE_BYTES) throw new Error(`Gateway credential bundle exceeds ${MAX_BUNDLE_BYTES} bytes`);
	const text = value.toString("utf8").trim();
	if (!text) return normalizeBundle({});
	if (!text.startsWith("{")) return normalizeBundle({ drobotics: text });
	const parsed = parseStrictJSON(text, "Gateway credential bundle");
	if (!parsed || parsed.schemaVersion !== 1 || Object.keys(parsed).some((key) => !["schemaVersion", "drobotics", "providerKeys"].includes(key))) {
		throw new Error("Gateway credential bundle is invalid");
	}
	return normalizeBundle(parsed);
}

export function captureGatewayCredentials(env = process.env) {
  const fdValue = String(env[TOKEN_FD_ENV] ?? "").trim();
  const fileValue = String(env[TOKEN_FILE_ENV] ?? "").trim();
  delete env[TOKEN_FD_ENV];
  delete env[TOKEN_FILE_ENV];
  const ambient = normalizeToken(env[TOKEN_ENV], "D-Robotics gateway credential");
  delete env[TOKEN_ENV];
  const ambientProviderKeys = {};
  for (const name of Object.keys(env)) {
    if (!name.startsWith(PROVIDER_TOKEN_PREFIX)) continue;
    if (!CREDENTIAL_ENV_PATTERN.test(name)) throw new Error(`Managed provider credential name is invalid: ${name}`);
    if (env[name]) ambientProviderKeys[name] = env[name];
    delete env[name];
  }
  let bundle = normalizeBundle({ drobotics: ambient, providerKeys: ambientProviderKeys });
  if (fdValue && fileValue) throw new Error("D-Robotics gateway credential transport is ambiguous");
  if (fdValue) {
    if (!/^\d+$/u.test(fdValue) || Number(fdValue) < 3) {
      throw new Error(`${TOKEN_FD_ENV} must identify an inherited file descriptor`);
    }
    const fd = Number(fdValue);
    try {
		bundle = decodeCredentialPayload(readFileSync(fd));
    } finally {
      closeSync(fd);
    }
  } else if (fileValue) {
    const expected = resolve(resolveUserPaths(env).stateRoot, "agentd", "credential", "token");
    if (resolve(fileValue) !== expected) throw new Error(`${TOKEN_FILE_ENV} must use the managed sandbox credential path`);
    const info = lstatSync(expected);
    const currentUid = process.getuid?.();
    if (!info.isFile() || info.isSymbolicLink() || (currentUid !== undefined && info.uid !== currentUid) || (info.mode & 0o077) !== 0) {
      throw new Error("D-Robotics sandbox credential must be a private regular file owned by the current user");
    }
    try {
		bundle = decodeCredentialPayload(readFileSync(expected));
    } finally {
      unlinkSync(expected);
    }
  }
	if (env !== process.env) return bundle;
	if (bundle.drobotics || Object.keys(bundle.providerKeys).length > 0) {
    Object.defineProperty(globalThis, CREDENTIAL_CACHE, {
		value: bundle,
      writable: true,
      configurable: false,
      enumerable: false,
    });
  }
	return globalThis[CREDENTIAL_CACHE] || bundle;
}

export function sideAgentCredentialStdio(token) {
  return token ? ["pipe", "pipe", "pipe", "pipe"] : ["pipe", "pipe", "pipe"];
}

export function writeSideAgentCredential(child, token) {
  if (!token) return;
  const stream = child.stdio?.[3];
  if (!stream || typeof stream.end !== "function") throw new Error("Side Agent credential pipe is unavailable");
  stream.end(token);
}

export const gatewayCredentialFdEnvironment = TOKEN_FD_ENV;
