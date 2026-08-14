import { lstatSync, readFileSync } from "node:fs";
import { URL } from "node:url";

import { resolveUserPaths } from "./user-paths.mjs";
import { parseStrictJSON } from "./strict-json.mjs";

const MAX_CONFIG_BYTES = 512 * 1024;
const MAX_PROVIDERS = 64;
const MAX_MODELS = 128;
const PROVIDER_ID = /^[a-z0-9][a-z0-9._-]{0,63}$/u;
const MODEL_ID = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$/u;
const CREDENTIAL_ENV = /^HOBOT_CODE_PROVIDER_KEY_[A-Z0-9_]{1,96}$/u;
const APIS = new Set(["anthropic-messages", "openai-completions", "openai-responses", "google-generative-ai"]);
const MODEL_FIELDS = new Set(["id", "name", "reasoning", "input", "contextWindow", "maxTokens", "thinkingLevelMap", "compat"]);
const PROVIDER_FIELDS = new Set(["id", "name", "baseUrl", "api", "credentialEnv", "authHeader", "models"]);
const THINKING_LEVELS = new Set(["off", "minimal", "low", "medium", "high", "xhigh", "max"]);
const COMPAT_FIELDS = new Set(["supportsDeveloperRole", "supportsReasoningEffort", "supportsStore", "supportsUsageInStreaming", "supportsStrictMode"]);

function plainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value) && Object.getPrototypeOf(value) === Object.prototype;
}

function exactFields(value, allowed, label) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) throw new Error(`${label} contains unsupported field ${key}`);
  }
}

function boundedLabel(value, fallback, maximum, label) {
	if (value !== undefined && typeof value !== "string") throw new Error(`${label} is invalid`);
	const result = value === undefined ? fallback : value.trim();
  if (!result || result.length > maximum || /[\u0000-\u001f\u007f]/u.test(result)) throw new Error(`${label} is invalid`);
  return result;
}

function managedBaseUrl(value) {
	if (typeof value !== "string") throw new Error("Managed provider baseUrl is invalid");
  let parsed;
  try {
	parsed = new URL(value);
  } catch {
    throw new Error("Managed provider baseUrl is invalid");
  }
  const local = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1" || parsed.hostname === "[::1]";
  if ((parsed.protocol !== "https:" && !(parsed.protocol === "http:" && local)) || parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw new Error("Managed provider baseUrl must use HTTPS, or HTTP on localhost, without credentials, query, or fragment");
  }
  return parsed.toString().replace(/\/$/u, "");
}

function managedModel(value, providerId, seen) {
  if (!plainObject(value)) throw new Error(`Managed provider ${providerId} contains an invalid model`);
  exactFields(value, MODEL_FIELDS, `Managed provider ${providerId} model`);
  const id = boundedLabel(value.id, "", 192, `Managed provider ${providerId} model id`);
  if (!MODEL_ID.test(id) || seen.has(id)) throw new Error(`Managed provider ${providerId} model id is invalid or duplicated: ${id}`);
  seen.add(id);
  const contextWindow = value.contextWindow === undefined ? 128000 : value.contextWindow;
  const maxTokens = value.maxTokens === undefined ? 16384 : value.maxTokens;
  if (!Number.isInteger(contextWindow) || contextWindow < 1024 || contextWindow > 4_000_000 || !Number.isInteger(maxTokens) || maxTokens < 128 || maxTokens > 131_072 || maxTokens > contextWindow) {
    throw new Error(`Managed provider ${providerId} model ${id} has invalid token limits`);
  }
  const input = value.input === undefined ? ["text"] : value.input;
  if (!Array.isArray(input) || input.length < 1 || input.length > 2 || input[0] !== "text" || input.some((kind) => !["text", "image"].includes(kind)) || new Set(input).size !== input.length) {
    throw new Error(`Managed provider ${providerId} model ${id} has invalid input modes`);
  }
  if (value.reasoning !== undefined && typeof value.reasoning !== "boolean") throw new Error(`Managed provider ${providerId} model ${id} has invalid reasoning metadata`);
  let thinkingLevelMap;
  if (value.thinkingLevelMap !== undefined) {
    if (!plainObject(value.thinkingLevelMap)) throw new Error(`Managed provider ${providerId} model ${id} has invalid thinkingLevelMap`);
    thinkingLevelMap = {};
    for (const [level, mapping] of Object.entries(value.thinkingLevelMap)) {
      if (!THINKING_LEVELS.has(level) || (mapping !== null && (typeof mapping !== "string" || !mapping || mapping.length > 32))) throw new Error(`Managed provider ${providerId} model ${id} has invalid thinking level ${level}`);
      thinkingLevelMap[level] = mapping;
    }
  }
  let compat;
  if (value.compat !== undefined) {
    if (!plainObject(value.compat)) throw new Error(`Managed provider ${providerId} model ${id} has invalid compatibility metadata`);
    exactFields(value.compat, COMPAT_FIELDS, `Managed provider ${providerId} model ${id} compatibility`);
    compat = {};
    for (const [name, enabled] of Object.entries(value.compat)) {
      if (typeof enabled !== "boolean") throw new Error(`Managed provider ${providerId} model ${id} compatibility ${name} must be boolean`);
      compat[name] = enabled;
    }
  }
  return {
    id,
    name: boundedLabel(value.name, id, 120, `Managed provider ${providerId} model name`),
    reasoning: value.reasoning === true,
    input,
    contextWindow,
    maxTokens,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    ...(thinkingLevelMap ? { thinkingLevelMap } : {}),
    ...(compat ? { compat } : {}),
  };
}

function readPrivateProviderConfig(path) {
  let info;
  try {
    info = lstatSync(path);
  } catch (error) {
    if (error?.code === "ENOENT") return undefined;
    throw error;
  }
  const uid = process.getuid?.();
  if (!info.isFile() || info.isSymbolicLink() || info.size <= 0 || info.size > MAX_CONFIG_BYTES || (info.mode & 0o077) !== 0 || (uid !== undefined && info.uid !== uid)) {
    throw new Error("Managed provider config must be a private regular file owned by the current user");
  }
  const raw = readFileSync(path);
  if (raw.byteLength !== info.size) throw new Error("Managed provider config changed while being read");
  return parseStrictJSON(raw.toString("utf8"), "Managed provider config");
}

export function modelEgressSupportsManagedAPI(api) {
	return api === "anthropic-messages" || api === "openai-completions" || api === "openai-responses";
}

export function loadManagedProviders(env = process.env, providerKeys = {}, options = {}) {
  const path = resolveUserPaths(env).managedProviderConfig;
  const document = readPrivateProviderConfig(path);
  if (document === undefined) return { path, providers: [], diagnostics: [] };
  if (!plainObject(document) || document.schemaVersion !== 1 || !Array.isArray(document.providers) || Object.keys(document).some((key) => !["schemaVersion", "providers"].includes(key)) || document.providers.length > MAX_PROVIDERS) {
    throw new Error("Managed provider config must use schemaVersion 1 and a bounded providers array");
  }
  const seen = new Set();
  const providers = [];
  const diagnostics = [];
  for (const value of document.providers) {
    if (!plainObject(value)) throw new Error("Managed provider entry is invalid");
    exactFields(value, PROVIDER_FIELDS, "Managed provider");
    const id = boundedLabel(value.id, "", 64, "Managed provider id");
    if (!PROVIDER_ID.test(id) || id === "drobotics" || seen.has(id)) throw new Error(`Managed provider id is invalid, reserved, or duplicated: ${id}`);
    seen.add(id);
    if (!APIS.has(value.api)) throw new Error(`Managed provider ${id} uses unsupported API ${value.api}`);
    if (!CREDENTIAL_ENV.test(value.credentialEnv || "")) throw new Error(`Managed provider ${id} must reference a HOBOT_CODE_PROVIDER_KEY_* credential`);
    if (!Array.isArray(value.models) || value.models.length < 1 || value.models.length > MAX_MODELS) throw new Error(`Managed provider ${id} has an invalid model list`);
    if (value.authHeader !== undefined && typeof value.authHeader !== "boolean") throw new Error(`Managed provider ${id} has invalid authHeader metadata`);
			const token = providerKeys[value.credentialEnv];
			const proxied = Boolean(options.modelEgressSocket)
				&& options.modelEgressProviders instanceof Set
				&& options.modelEgressProviders.has(id)
				&& modelEgressSupportsManagedAPI(value.api);
		if (!token && !proxied) {
      diagnostics.push({ id, status: "missing-credential", credentialEnv: value.credentialEnv });
      continue;
    }
    const modelIds = new Set();
    providers.push({
      id,
      name: boundedLabel(value.name, id, 120, `Managed provider ${id} name`),
      baseUrl: managedBaseUrl(value.baseUrl),
      api: value.api,
			apiKey: token || "hobot-model-egress",
			modelEgress: proxied,
	  ...(value.authHeader !== undefined ? {authHeader: value.authHeader} : {}),
      models: value.models.map((model) => managedModel(model, id, modelIds)),
    });
    diagnostics.push({ id, status: "configured", credentialEnv: value.credentialEnv });
  }
  return { path, providers, diagnostics };
}

export function registerManagedProviders(pi, providerKeys, env = process.env, options = {}) {
	const catalog = loadManagedProviders(env, providerKeys, options);
	for (const provider of catalog.providers) {
		const { id, modelEgress, ...config } = provider;
		if (modelEgress) {
			const streamSimple = options.createModelEgressStream?.(id, config.api, options.modelEgressSocket);
			if (!streamSimple) throw new Error(`Managed provider ${id} cannot use the configured model egress broker`);
			config.streamSimple = streamSimple;
		}
		pi.registerProvider(id, config);
  }
  return catalog;
}
