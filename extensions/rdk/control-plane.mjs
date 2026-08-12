import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { access, chmod, lstat, mkdir, opendir, readFile, readlink, rename, writeFile } from "node:fs/promises";
import { basename, extname, join, resolve } from "node:path";

export const DEFAULT_POLICY = Object.freeze({
  schemaVersion: 2,
  rootMode: "confirm",
  default: "ask",
  rules: Object.freeze([
    Object.freeze({ tool: "read", action: "allow" }),
    Object.freeze({ tool: "write", action: "ask" }),
    Object.freeze({ tool: "edit", action: "ask" }),
    Object.freeze({ tool: "bash", action: "ask" }),
    Object.freeze({ tool: "system_snapshot", action: "allow" }),
    Object.freeze({ tool: "rdk_docs_search", action: "allow" }),
    Object.freeze({ tool: "quality_gate", action: "ask" }),
    Object.freeze({ tool: "memory_search", action: "allow" }),
    Object.freeze({ tool: "memory_save", action: "ask" }),
    Object.freeze({ tool: "goal_status", action: "allow" }),
    Object.freeze({ tool: "goal_progress", action: "allow" }),
    Object.freeze({ tool: "goal_complete", action: "ask" }),
    Object.freeze({ tool: "lsp", action: "allow" }),
    Object.freeze({ tool: "mcp:*", action: "ask" }),
  ]),
});

export const DEVELOPER_POLICY = Object.freeze({
  schemaVersion: 2,
  rootMode: "policy",
  default: "ask",
  rules: Object.freeze([
    Object.freeze({ tool: "read", action: "allow" }),
    Object.freeze({ tool: "ls", action: "allow" }),
    Object.freeze({ tool: "find", action: "allow" }),
    Object.freeze({ tool: "grep", action: "allow" }),
    Object.freeze({ tool: "write", action: "allow" }),
    Object.freeze({ tool: "edit", action: "allow" }),
    Object.freeze({ tool: "bash", action: "allow" }),
    Object.freeze({ tool: "system_snapshot", action: "allow" }),
    Object.freeze({ tool: "rdk_docs_search", action: "allow" }),
    Object.freeze({ tool: "memory_search", action: "allow" }),
    Object.freeze({ tool: "goal_status", action: "allow" }),
    Object.freeze({ tool: "goal_progress", action: "allow" }),
    Object.freeze({ tool: "lsp", action: "allow" }),
    Object.freeze({ tool: "quality_gate", action: "ask" }),
    Object.freeze({ tool: "memory_save", action: "ask" }),
    Object.freeze({ tool: "goal_complete", action: "ask" }),
    Object.freeze({ tool: "mcp:*", action: "ask" }),
  ]),
});

export const MEMORY_SCOPES = Object.freeze(["user", "project", "board", "session"]);
export const MEMORY_KINDS = Object.freeze(["preference", "decision", "fact", "fix", "instruction", "note"]);

export const DEFAULT_MEMORY_CONFIG = Object.freeze({
  schemaVersion: 1,
  enabled: true,
  autoRecall: true,
  maxInjected: 6,
  maxSearchResults: 10,
  maxContentChars: 4000,
  defaultExpiresDays: null,
});

export const DEFAULT_GOAL_CONFIG = Object.freeze({
  schemaVersion: 1,
  enabled: true,
  defaultTurnBudget: 50,
  defaultTokenBudget: null,
});

export const DEFAULT_HOOK_CONFIG = Object.freeze({
  schemaVersion: 1,
  enabled: true,
  failurePolicy: "block",
  timeoutMs: 5000,
  maxOutputChars: 4000,
  allowProjectHooks: false,
  hooks: Object.freeze([]),
});

export const DEFAULT_NOTIFICATION_CONFIG = Object.freeze({
  schemaVersion: 1,
  enabled: true,
  allowLocal: false,
  bell: true,
  protocol: "osc9",
  onApproval: true,
  onComplete: true,
  onFailure: true,
  minDurationMs: 5000,
});

export const DEFAULT_LSP_CONFIG = Object.freeze({
  schemaVersion: 1,
  enabled: true,
  maxProcesses: 1,
  maxMemoryMiB: 256,
  idleTimeoutMs: 60_000,
  requestTimeoutMs: 10_000,
  diagnosticsWaitMs: 500,
  servers: Object.freeze([
    Object.freeze({ id: "clangd", extensions: Object.freeze([".c", ".cc", ".cpp", ".cxx", ".h", ".hpp"]), languageId: "cpp", command: Object.freeze(["clangd", "--background-index=false"]) }),
    Object.freeze({ id: "pylsp", extensions: Object.freeze([".py"]), languageId: "python", command: Object.freeze(["pylsp"]) }),
    Object.freeze({ id: "typescript", extensions: Object.freeze([".ts", ".tsx", ".js", ".jsx"]), languageId: "typescript", command: Object.freeze(["typescript-language-server", "--stdio"]) }),
    Object.freeze({ id: "gopls", extensions: Object.freeze([".go"]), languageId: "go", command: Object.freeze(["gopls"]) }),
    Object.freeze({ id: "rust-analyzer", extensions: Object.freeze([".rs"]), languageId: "rust", command: Object.freeze(["rust-analyzer"]) }),
  ]),
});

const permissionActions = new Set(["allow", "ask", "deny"]);
const rootPermissionModes = new Set(["confirm", "policy"]);
const fingerprintExcludedDirectories = new Set([
  ".cache",
  ".git",
  ".mypy_cache",
  ".next",
  ".pytest_cache",
  ".ruff_cache",
  ".tox",
  ".venv",
  "__pycache__",
  "build",
  "coverage",
  "dist",
  "node_modules",
  "out",
  "target",
  "venv",
]);
const fingerprintMetadataOnlyExtensions = new Set([
  ".a", ".bin", ".db", ".gz", ".hbm", ".jpeg", ".jpg", ".mp4", ".npy", ".npz",
  ".o", ".onnx", ".parquet", ".png", ".so", ".sqlite", ".tar", ".wasm", ".zip",
]);
const MAX_FINGERPRINT_FILE_BYTES = 8 * 1024 * 1024;
const MAX_FINGERPRINT_TOTAL_BYTES = 128 * 1024 * 1024;

function cloneDefaultPolicy() {
  return {
    schemaVersion: DEFAULT_POLICY.schemaVersion,
    rootMode: DEFAULT_POLICY.rootMode,
    default: DEFAULT_POLICY.default,
    rules: DEFAULT_POLICY.rules.map((rule) => ({ ...rule })),
  };
}

function isLegacyDeveloperPolicy(policy) {
  if (policy.rootMode !== "confirm" || policy.default !== DEVELOPER_POLICY.default) return false;
  const broadRules = policy.rules.filter((rule) => !rule.targetHash);
  return broadRules.length === DEVELOPER_POLICY.rules.length
    && broadRules.every((rule, index) => rule.tool === DEVELOPER_POLICY.rules[index].tool
      && rule.action === DEVELOPER_POLICY.rules[index].action);
}

export function parsePolicy(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("permission policy must be a JSON object");
  }
  if (value.schemaVersion !== 1 && value.schemaVersion !== 2) {
    throw new Error("permission policy schemaVersion must be 1 or 2");
  }
  if (!permissionActions.has(value.default)) {
    throw new Error("permission policy default must be allow, ask, or deny");
  }
  const rootMode = value.schemaVersion === 1 ? "confirm" : (value.rootMode ?? "confirm");
  if (!rootPermissionModes.has(rootMode)) {
    throw new Error("permission policy rootMode must be confirm or policy");
  }
  if (!Array.isArray(value.rules) || value.rules.length > 128) {
    throw new Error("permission policy rules must be an array with at most 128 items");
  }

  const rules = value.rules.map((rule, index) => {
    if (!rule || typeof rule !== "object" || Array.isArray(rule)) {
      throw new Error(`permission rule ${index + 1} must be an object`);
    }
    const tool = typeof rule.tool === "string" ? rule.tool.trim() : "";
    if (!tool || tool.length > 128 || /[\r\n]/.test(tool)) {
      throw new Error(`permission rule ${index + 1} has an invalid tool pattern`);
    }
    if (!permissionActions.has(rule.action)) {
      throw new Error(`permission rule ${index + 1} action must be allow, ask, or deny`);
    }
    const matchesMutation = ["bash", "write", "edit"].some((name) => wildcardMatches(name, tool));
    const action = value.schemaVersion === 1 && rule.action === "allow" && matchesMutation
      ? "ask"
      : rule.action;
    const targetHash = rule.targetHash === undefined ? undefined : String(rule.targetHash).trim().toLowerCase();
    if (targetHash !== undefined && !/^[a-f0-9]{64}$/.test(targetHash)) {
      throw new Error(`permission rule ${index + 1} has an invalid targetHash`);
    }
    return targetHash ? { tool, action, targetHash } : { tool, action };
  });

  return {
    schemaVersion: 2,
    rootMode,
    default: value.schemaVersion === 1 && value.default === "allow" ? "ask" : value.default,
    rules,
  };
}

export async function loadPolicy(path) {
  try {
    const raw = JSON.parse(await readFile(path, "utf8"));
    let policy = parsePolicy(raw);
    const legacyDeveloper = raw.schemaVersion === 2 && isLegacyDeveloperPolicy(policy);
    if (legacyDeveloper) policy = parsePolicy({ ...policy, rootMode: "policy" });
    if (raw.schemaVersion === 1 || legacyDeveloper) {
      try {
        await writePolicy(path, policy);
        return { policy, migrated: true };
      } catch (error) {
        return {
          policy,
          migrated: true,
          error: `legacy permission policy is safe in memory but could not be persisted: ${error instanceof Error ? error.message : String(error)}`,
        };
      }
    }
    return { policy };
  } catch (error) {
    if (error?.code === "ENOENT") return { policy: cloneDefaultPolicy(), error: "permission policy file is missing" };
    return {
      policy: cloneDefaultPolicy(),
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

export async function writePolicy(path, policy) {
  const normalized = parsePolicy(policy);
  const temporary = `${path}.new.${process.pid}`;
  await mkdir(resolve(path, ".."), { recursive: true, mode: 0o750 });
  await writeFile(temporary, `${JSON.stringify(normalized, null, 2)}\n`, { mode: 0o600 });
  await chmod(temporary, 0o600);
  await rename(temporary, path);
  return normalized;
}

function wildcardMatches(value, pattern) {
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*");
  return new RegExp(`^${escaped}$`, "i").test(value);
}

export function isMcpTool(tool) {
  if (typeof tool === "string") return /^mcp(?:__|_|:)/i.test(tool);
  const source = [tool?.sourceInfo?.source, tool?.sourceInfo?.path, tool?.sourceInfo?.origin]
    .filter((item) => typeof item === "string")
    .join(" ");
  return /^mcp(?:__|_|:)/i.test(String(tool?.name ?? "")) || /\bmcp\b/i.test(source);
}

export function resolveToolAction(policy, toolName, mcp = false) {
  for (const rule of policy.rules) {
    if (rule.targetHash) continue;
    if (rule.tool.toLowerCase() === "mcp:*") {
      if (mcp) return rule.action;
      continue;
    }
    if (wildcardMatches(toolName, rule.tool)) return rule.action;
  }
  return policy.default;
}

function stableJson(value) {
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableJson(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value) ?? "null";
}

export function permissionTargetHash(toolName, input) {
  return createHash("sha256")
    .update(`${String(toolName).toLowerCase()}\0${stableJson(input ?? {})}`)
    .digest("hex");
}

export function resolveToolCallAction(policy, toolName, input, mcp = false) {
  const broadAction = resolveToolAction(policy, toolName, mcp);
  if (broadAction === "deny") return "deny";
  const targetHash = permissionTargetHash(toolName, input);
  for (const rule of policy.rules) {
    if (rule.targetHash === targetHash && wildcardMatches(toolName, rule.tool)) return rule.action;
  }
  return broadAction;
}

export function hasAllowedToolCall(policy, toolName, input) {
  const targetHash = permissionTargetHash(toolName, input);
  return policy.rules.some((rule) => rule.action === "allow"
    && rule.targetHash === targetHash && wildcardMatches(toolName, rule.tool));
}

export function requiresRootToolApproval(policy, runningAsRoot, toolName) {
  return Boolean(runningAsRoot && policy.rootMode === "confirm"
    && ["bash", "write", "edit"].includes(toolName));
}

export function reconcileToolVisibility(allTools, activeTools, hiddenTools, deniedTools) {
  const available = new Set(allTools);
  const active = new Set(activeTools);
  const hidden = new Set([...hiddenTools].filter((name) => available.has(name)));
  const denied = new Set(deniedTools);

  for (const name of [...hidden]) {
    if (denied.has(name)) continue;
    active.add(name);
    hidden.delete(name);
  }
  for (const name of denied) {
    if (active.delete(name)) hidden.add(name);
  }
  return { activeTools: [...active], hiddenTools: hidden };
}

export function setPolicyRule(policy, tool, action) {
  const pattern = String(tool ?? "").trim();
  if (!pattern || pattern.length > 128 || /[\r\n]/.test(pattern)) {
    throw new Error("tool pattern must be 1-128 characters without newlines");
  }
  if (!permissionActions.has(action)) throw new Error("action must be allow, ask, or deny");
  return parsePolicy({
    ...policy,
    rules: [{ tool: pattern, action }, ...policy.rules.filter((rule) => rule.tool !== pattern)],
  });
}

export function setPolicyCallRule(policy, tool, input, action) {
  const pattern = String(tool ?? "").trim();
  if (!pattern || pattern.length > 128 || /[\r\n]/.test(pattern)) {
    throw new Error("tool pattern must be 1-128 characters without newlines");
  }
  if (!permissionActions.has(action)) throw new Error("action must be allow, ask, or deny");
  const targetHash = permissionTargetHash(pattern, input);
  const broadRules = policy.rules.filter((rule) => !rule.targetHash);
  const maximumCallRules = Math.min(64, 128 - broadRules.length);
  if (maximumCallRules < 1) throw new Error("permission policy has no room for a remembered call");
  const retainedCallRules = policy.rules
    .filter((rule) => rule.targetHash && !(rule.tool === pattern && rule.targetHash === targetHash))
    .slice(0, maximumCallRules - 1);
  return parsePolicy({
    ...policy,
    rules: [
      { tool: pattern, action, targetHash },
      ...retainedCallRules,
      ...broadRules,
    ],
  });
}

export function applyPermissionPreset(name) {
  if (name !== "developer") throw new Error("permission preset must be developer");
  return parsePolicy({
    ...DEVELOPER_POLICY,
    rules: DEVELOPER_POLICY.rules.map((rule) => ({ ...rule })),
  });
}

export function redactSensitiveText(value, limit = 800) {
  const text = String(value ?? "")
    .replace(/-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----[\s\S]*?(?:-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|$)/gi, "[REDACTED PRIVATE KEY]")
    .replace(/\b(?:sk|ak)-[A-Za-z0-9_-]{8,}\b/g, "[REDACTED]")
    .replace(/\bgh[pousr]_[A-Za-z0-9]{20,}\b/g, "[REDACTED GITHUB TOKEN]")
    .replace(/\bxox(?:a|b|p|r|s)-[A-Za-z0-9-]{10,}\b/g, "[REDACTED SLACK TOKEN]")
    .replace(/\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/g, "[REDACTED JWT]")
    .replace(/(https?:\/\/)[^\s/:@]+:[^\s/@]+@/gi, "$1[REDACTED]@")
    .replace(/\bBearer\s+[^\s,;]+/gi, "Bearer [REDACTED]")
    .replace(/((?:TOKEN|SECRET|PASSWORD|PASSWD|API_KEY)\s*[=:]\s*)[^\s,;]+/gi, "$1[REDACTED]")
    .replace(/\b(?:\d[ -]*?){13,19}\b/g, "[REDACTED NUMBER]");
  return text.length > limit ? `${text.slice(0, limit - 3)}...` : text;
}

export function describeToolCall(toolName, input, qualityCommands = []) {
  const data = input && typeof input === "object" ? input : {};
  let target;
  if (["read", "write", "edit"].includes(toolName)) target = data.path;
  else if (toolName === "bash") target = data.command;
  else if (toolName === "quality_gate" && data.action === "run") target = qualityCommands.join("\n");
  else if (toolName === "memory_save") target = `${data.scope ?? ""}/${data.kind ?? ""}: ${data.content ?? ""}`;
  else if (toolName === "memory_search") target = data.query;
  else if (toolName === "goal_complete") target = data.outcome;
  else target = JSON.stringify(data);

  const risk = toolName === "read"
    ? "Reads local data and may expose file contents to the selected model."
    : ["write", "edit"].includes(toolName)
      ? "Modifies files on the board."
      : toolName === "bash"
        ? "Executes a shell command with the current user privileges."
      : toolName === "quality_gate"
          ? "Executes the configured verification commands."
          : toolName === "memory_save"
            ? "Persists information and may expose it to models in future sessions."
            : toolName === "memory_search"
              ? "Retrieves persistent information and exposes matching results to the selected model."
            : toolName === "goal_complete"
              ? "Marks the current user-created persistent goal complete."
          : isMcpTool(toolName)
            ? "Calls an external MCP tool which may read or change external state."
            : "Calls an extension or plugin tool.";
  return `Tool: ${toolName}\nRisk: ${risk}\nTarget:\n${redactSensitiveText(target || "(no target)")}`;
}

export function parseMemoryConfig(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("memory config must be a JSON object");
  }
  if (value.schemaVersion !== 1) throw new Error("memory config schemaVersion must be 1");
  if (typeof value.enabled !== "boolean" || typeof value.autoRecall !== "boolean") {
    throw new Error("memory enabled and autoRecall must be booleans");
  }
  const maxInjected = Number(value.maxInjected);
  const maxSearchResults = Number(value.maxSearchResults);
  const maxContentChars = Number(value.maxContentChars);
  if (!Number.isInteger(maxInjected) || maxInjected < 0 || maxInjected > 20) {
    throw new Error("memory maxInjected must be between 0 and 20");
  }
  if (!Number.isInteger(maxSearchResults) || maxSearchResults < 1 || maxSearchResults > 50) {
    throw new Error("memory maxSearchResults must be between 1 and 50");
  }
  if (!Number.isInteger(maxContentChars) || maxContentChars < 100 || maxContentChars > 20_000) {
    throw new Error("memory maxContentChars must be between 100 and 20000");
  }
  const defaultExpiresDays = value.defaultExpiresDays === null || value.defaultExpiresDays === undefined
    ? null
    : Number(value.defaultExpiresDays);
  if (defaultExpiresDays !== null
    && (!Number.isInteger(defaultExpiresDays) || defaultExpiresDays < 1 || defaultExpiresDays > 3650)) {
    throw new Error("memory defaultExpiresDays must be null or between 1 and 3650");
  }
  return {
    schemaVersion: 1,
    enabled: value.enabled,
    autoRecall: value.autoRecall,
    maxInjected,
    maxSearchResults,
    maxContentChars,
    defaultExpiresDays,
  };
}

export async function loadMemoryConfig(path) {
  try {
    return { config: parseMemoryConfig(JSON.parse(await readFile(path, "utf8"))) };
  } catch (error) {
    if (error?.code === "ENOENT") {
      return { config: { ...DEFAULT_MEMORY_CONFIG }, error: "memory config file is missing" };
    }
    return {
      config: { ...DEFAULT_MEMORY_CONFIG },
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

function boundedInteger(value, label, minimum, maximum) {
  const normalized = Number(value);
  if (!Number.isInteger(normalized) || normalized < minimum || normalized > maximum) {
    throw new Error(`${label} must be between ${minimum} and ${maximum}`);
  }
  return normalized;
}

export function parseGoalConfig(value) {
  if (!value || typeof value !== "object" || Array.isArray(value) || value.schemaVersion !== 1) {
    throw new Error("goal config must be a schemaVersion 1 JSON object");
  }
  if (typeof value.enabled !== "boolean") throw new Error("goal enabled must be boolean");
  const defaultTurnBudget = boundedInteger(value.defaultTurnBudget, "goal defaultTurnBudget", 1, 10_000);
  const defaultTokenBudget = value.defaultTokenBudget === null || value.defaultTokenBudget === undefined
    ? null
    : boundedInteger(value.defaultTokenBudget, "goal defaultTokenBudget", 1000, 1_000_000_000);
  return { schemaVersion: 1, enabled: value.enabled, defaultTurnBudget, defaultTokenBudget };
}

export async function loadGoalConfig(path) {
  try {
    return { config: parseGoalConfig(JSON.parse(await readFile(path, "utf8"))) };
  } catch (error) {
    return {
      config: { ...DEFAULT_GOAL_CONFIG },
      error: error?.code === "ENOENT" ? "goal config file is missing" : error instanceof Error ? error.message : String(error),
    };
  }
}

export function parseHookConfig(value) {
  if (!value || typeof value !== "object" || Array.isArray(value) || value.schemaVersion !== 1) {
    throw new Error("hook config must be a schemaVersion 1 JSON object");
  }
  if (typeof value.enabled !== "boolean" || typeof value.allowProjectHooks !== "boolean") {
    throw new Error("hook enabled and allowProjectHooks must be booleans");
  }
  if (!["block", "warn"].includes(value.failurePolicy)) throw new Error("hook failurePolicy must be block or warn");
  const timeoutMs = boundedInteger(value.timeoutMs, "hook timeoutMs", 100, 120_000);
  const maxOutputChars = boundedInteger(value.maxOutputChars, "hook maxOutputChars", 100, 20_000);
  if (!Array.isArray(value.hooks) || value.hooks.length > 64) throw new Error("hooks must contain at most 64 entries");
  const hooks = value.hooks.map((hook, index) => {
    if (!hook || typeof hook !== "object" || Array.isArray(hook)) throw new Error(`hook ${index + 1} must be an object`);
    const name = typeof hook.name === "string" ? hook.name.trim() : "";
    const tool = typeof hook.tool === "string" ? hook.tool.trim() : "";
    if (!name || name.length > 80 || !tool || tool.length > 128) throw new Error(`hook ${index + 1} name or tool is invalid`);
    if (!["PreToolUse", "PostToolUse"].includes(hook.event)) throw new Error(`hook ${index + 1} event is invalid`);
    if (!Array.isArray(hook.command) || hook.command.length < 1 || hook.command.length > 32
      || hook.command.some((part) => typeof part !== "string" || !part || part.length > 1000 || /[\r\n\0]/.test(part))) {
      throw new Error(`hook ${index + 1} command must be a non-empty string array`);
    }
    if (hook.failurePolicy !== undefined && !["block", "warn"].includes(hook.failurePolicy)) {
      throw new Error(`hook ${index + 1} failurePolicy is invalid`);
    }
    return {
      name,
      event: hook.event,
      tool,
      command: [...hook.command],
      ...(hook.timeoutMs === undefined ? {} : { timeoutMs: boundedInteger(hook.timeoutMs, `hook ${index + 1} timeoutMs`, 100, 120_000) }),
      ...(hook.failurePolicy === undefined ? {} : { failurePolicy: hook.failurePolicy }),
    };
  });
  return {
    schemaVersion: 1,
    enabled: value.enabled,
    failurePolicy: value.failurePolicy,
    timeoutMs,
    maxOutputChars,
    allowProjectHooks: value.allowProjectHooks,
    hooks,
  };
}

export async function loadHookConfig(path, projectPath, projectTrusted = true) {
  let config;
  let error;
  try {
    config = parseHookConfig(JSON.parse(await readFile(path, "utf8")));
  } catch (caught) {
    config = { ...DEFAULT_HOOK_CONFIG, hooks: [] };
    error = caught?.code === "ENOENT" ? "hook config file is missing" : caught instanceof Error ? caught.message : String(caught);
  }
  if (config.allowProjectHooks && projectPath && !projectTrusted) {
    error = "project hooks ignored until the current project is trusted";
  } else if (config.allowProjectHooks && projectPath) {
    try {
      const project = parseHookConfig(JSON.parse(await readFile(projectPath, "utf8")));
      config = { ...config, hooks: [...config.hooks, ...project.hooks] };
    } catch (caught) {
      if (caught?.code !== "ENOENT") error = `project hooks ignored: ${caught instanceof Error ? caught.message : String(caught)}`;
    }
  }
  return { config, error };
}

export function parseNotificationConfig(value) {
  if (!value || typeof value !== "object" || Array.isArray(value) || value.schemaVersion !== 1) {
    throw new Error("notification config must be a schemaVersion 1 JSON object");
  }
  for (const key of ["enabled", "allowLocal", "bell", "onApproval", "onComplete", "onFailure"]) {
    if (typeof value[key] !== "boolean") throw new Error(`notification ${key} must be boolean`);
  }
  if (!["osc9", "osc777", "both"].includes(value.protocol)) throw new Error("notification protocol is invalid");
  return {
    schemaVersion: 1,
    enabled: value.enabled,
    allowLocal: value.allowLocal,
    bell: value.bell,
    protocol: value.protocol,
    onApproval: value.onApproval,
    onComplete: value.onComplete,
    onFailure: value.onFailure,
    minDurationMs: boundedInteger(value.minDurationMs, "notification minDurationMs", 0, 3_600_000),
  };
}

export async function loadNotificationConfig(path) {
  try {
    return { config: parseNotificationConfig(JSON.parse(await readFile(path, "utf8"))) };
  } catch (error) {
    return {
      config: { ...DEFAULT_NOTIFICATION_CONFIG },
      error: error?.code === "ENOENT" ? "notification config file is missing" : error instanceof Error ? error.message : String(error),
    };
  }
}

export async function writeNotificationConfig(path, value) {
  const config = parseNotificationConfig(value);
  const temporary = `${path}.new.${process.pid}`;
  await mkdir(resolve(path, ".."), { recursive: true, mode: 0o750 });
  await writeFile(temporary, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
  await chmod(temporary, 0o600);
  await rename(temporary, path);
  return config;
}

export function parseLspConfig(value) {
  if (!value || typeof value !== "object" || Array.isArray(value) || value.schemaVersion !== 1) {
    throw new Error("LSP config must be a schemaVersion 1 JSON object");
  }
  if (typeof value.enabled !== "boolean") throw new Error("LSP enabled must be boolean");
  const servers = Array.isArray(value.servers) ? value.servers : [];
  if (servers.length > 32) throw new Error("LSP servers must contain at most 32 entries");
  const normalizedServers = servers.map((server, index) => {
    if (!server || typeof server !== "object" || Array.isArray(server)) throw new Error(`LSP server ${index + 1} must be an object`);
    const id = typeof server.id === "string" ? server.id.trim() : "";
    const languageId = typeof server.languageId === "string" ? server.languageId.trim() : "";
    if (!id || id.length > 80 || !languageId || languageId.length > 80) throw new Error(`LSP server ${index + 1} id or languageId is invalid`);
    if (!Array.isArray(server.extensions) || server.extensions.length < 1 || server.extensions.length > 32
      || server.extensions.some((extension) => typeof extension !== "string" || !/^\.[a-z0-9+_-]{1,12}$/i.test(extension))) {
      throw new Error(`LSP server ${index + 1} extensions are invalid`);
    }
    if (!Array.isArray(server.command) || server.command.length < 1 || server.command.length > 32
      || server.command.some((part) => typeof part !== "string" || !part || part.length > 1000 || /[\r\n\0]/.test(part))) {
      throw new Error(`LSP server ${index + 1} command is invalid`);
    }
    return {
      id,
      languageId,
      extensions: [...new Set(server.extensions.map((extension) => extension.toLowerCase()))],
      command: [...server.command],
      ...(server.initializationOptions === undefined ? {} : { initializationOptions: server.initializationOptions }),
    };
  });
  return {
    schemaVersion: 1,
    enabled: value.enabled,
    maxProcesses: boundedInteger(value.maxProcesses, "LSP maxProcesses", 1, 4),
    maxMemoryMiB: boundedInteger(value.maxMemoryMiB, "LSP maxMemoryMiB", 32, 2048),
    idleTimeoutMs: boundedInteger(value.idleTimeoutMs, "LSP idleTimeoutMs", 5000, 3_600_000),
    requestTimeoutMs: boundedInteger(value.requestTimeoutMs, "LSP requestTimeoutMs", 500, 120_000),
    diagnosticsWaitMs: boundedInteger(value.diagnosticsWaitMs, "LSP diagnosticsWaitMs", 0, 10_000),
    servers: normalizedServers,
  };
}

export async function loadLspConfig(path) {
  try {
    return { config: parseLspConfig(JSON.parse(await readFile(path, "utf8"))) };
  } catch (error) {
    return {
      config: parseLspConfig({ ...DEFAULT_LSP_CONFIG, servers: DEFAULT_LSP_CONFIG.servers.map((server) => ({ ...server, extensions: [...server.extensions], command: [...server.command] })) }),
      error: error?.code === "ENOENT" ? "LSP config file is missing" : error instanceof Error ? error.message : String(error),
    };
  }
}

export function normalizeMemoryContent(value, maxLength = DEFAULT_MEMORY_CONFIG.maxContentChars) {
  const content = String(value ?? "").replace(/\0/g, "").replace(/\s+/g, " ").trim();
  if (!content) throw new Error("memory content must not be empty");
  if (content.length > maxLength) throw new Error(`memory content exceeds ${maxLength} characters`);
  return content;
}

export function sensitiveMemoryReasons(value) {
  const content = String(value ?? "");
  const checks = [
    [/(?:-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----)/i, "private key"],
    [/\bBearer\s+[^\s,;]{8,}/i, "bearer token"],
    [/\b(?:sk|ak)-[A-Za-z0-9_-]{8,}\b/i, "API token"],
    [/\bAKIA[0-9A-Z]{16}\b/, "AWS access key"],
    [/\bgh[pousr]_[A-Za-z0-9]{20,}\b/, "GitHub token"],
    [/\bxox(?:a|b|p|r|s)-[A-Za-z0-9-]{10,}\b/, "Slack token"],
    [/\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/, "JWT"],
    [/https?:\/\/[^\s/:@]+:[^\s/@]+@/i, "URL credential"],
    [/(?:TOKEN|SECRET|PASSWORD|PASSWD|API_KEY)\s*[=:]\s*[^\s,;]{4,}/i, "secret assignment"],
    [/\b(?:\d[ -]*?){13,19}\b/, "payment-card-like number"],
  ];
  return checks.filter(([pattern]) => pattern.test(content)).map(([_pattern, reason]) => reason);
}

export function validateMemoryInput(scope, kind, content, maxLength = DEFAULT_MEMORY_CONFIG.maxContentChars) {
  if (!MEMORY_SCOPES.includes(scope)) throw new Error(`memory scope must be one of: ${MEMORY_SCOPES.join(", ")}`);
  if (!MEMORY_KINDS.includes(kind)) throw new Error(`memory kind must be one of: ${MEMORY_KINDS.join(", ")}`);
  const normalized = normalizeMemoryContent(content, maxLength);
  const sensitive = sensitiveMemoryReasons(normalized);
  if (sensitive.length > 0) {
    throw new Error(`memory rejected because it may contain sensitive data: ${sensitive.join(", ")}`);
  }
  return { scope, kind, content: normalized };
}

export function memoryMatchQuery(value) {
  const terms = String(value ?? "")
    .normalize("NFKC")
    .match(/[\p{L}\p{N}_+.-]+/gu) ?? [];
  return [...new Set(terms.map((term) => term.slice(0, 80)).filter(Boolean))]
    .slice(0, 16)
    .map((term) => `"${term.replaceAll('"', '""')}"`)
    .join(" OR ");
}

export function knowledgeQueryTerms(value) {
  const normalized = String(value ?? "").normalize("NFKC").toLowerCase().trim();
  if (!normalized) return [];
  const terms = [normalized];
  terms.push(...(normalized.match(/[a-z0-9][a-z0-9_.+-]*/g) ?? []));
  for (const segment of normalized.match(/\p{Script=Han}+/gu) ?? []) {
    if (segment.length > 1 && segment.length <= 8) terms.push(segment);
    for (let width = 2; width <= Math.min(4, segment.length); width += 1) {
      for (let index = 0; index + width <= segment.length; index += 1) {
        terms.push(segment.slice(index, index + width));
      }
    }
  }
  return [...new Set(terms.filter((term) => term.length > 1))].slice(0, 48);
}

export function parseQualityConfig(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("quality gate config must be a JSON object");
  }
  if (value.schemaVersion !== 1) throw new Error("quality gate schemaVersion must be 1");
  if (!Array.isArray(value.commands) || value.commands.length > 8) {
    throw new Error("quality gate commands must be an array with at most 8 items");
  }
  const commands = value.commands.map((command, index) => {
    const normalized = typeof command === "string" ? command.trim() : "";
    if (!normalized || normalized.length > 512 || /[\r\n]/.test(normalized)) {
      throw new Error(`quality gate command ${index + 1} is invalid`);
    }
    return normalized;
  });
  const timeoutMs = Number(value.timeoutMs);
  if (!Number.isInteger(timeoutMs) || timeoutMs < 1000 || timeoutMs > 1_800_000) {
    throw new Error("quality gate timeoutMs must be between 1000 and 1800000");
  }
  return { schemaVersion: 1, timeoutMs, commands };
}

export async function loadQualityConfig(cwd) {
  const path = join(resolve(cwd), ".hobot", "quality-gates.json");
  try {
    return { path, config: parseQualityConfig(JSON.parse(await readFile(path, "utf8"))) };
  } catch (error) {
    if (error?.code === "ENOENT") {
      return { path, config: { schemaVersion: 1, timeoutMs: 120_000, commands: [] } };
    }
    return {
      path,
      config: { schemaVersion: 1, timeoutMs: 120_000, commands: [] },
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

async function exists(path) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

export async function detectQualityCommands(cwd) {
  const root = resolve(cwd);
  const commands = [];
  if (await exists(join(root, "Makefile"))) {
    const makefile = await readFile(join(root, "Makefile"), "utf8");
    if (/^check\s*:/m.test(makefile)) commands.push("make check");
    else if (/^test\s*:/m.test(makefile)) commands.push("make test");
  }
  if (await exists(join(root, "package.json"))) {
    try {
      const packageJson = JSON.parse(await readFile(join(root, "package.json"), "utf8"));
      if (packageJson?.scripts?.check) commands.push("npm run check");
      else if (packageJson?.scripts?.test) commands.push("npm test");
    } catch {
      // Project initialization remains useful when package.json is incomplete.
    }
  }
  if (await exists(join(root, "go.mod"))) commands.push("go test ./...");
  if (await exists(join(root, "Cargo.toml"))) commands.push("cargo test");
  if (await exists(join(root, "pyproject.toml")) || await exists(join(root, "pytest.ini"))) {
    commands.push("python3 -m pytest");
  }
  return [...new Set(commands)].slice(0, 8);
}

function agentsDocument(cwd, snapshot, commands) {
  const verification = commands.length > 0
    ? commands.map((command) => `- \`${command}\``).join("\n")
    : "- No automatic command was detected. Add project-specific checks before declaring completion.";
  return `# AGENTS.md\n\n## Scope\n\nThese instructions apply to the ${basename(resolve(cwd))} workspace.\n\n## RDK Target\n\n- Board: ${snapshot.board}\n- Board ID: ${snapshot.boardId}\n- RDK OS: ${snapshot.rdkOsVersion}\n- Architecture: ${snapshot.architecture}\n\n## Working Rules\n\n- Inspect existing code and live board state before changing behavior.\n- Keep changes scoped and preserve user data, active services, and unrelated edits.\n- Use version-matched D-Robotics documentation for BPU, multimedia, TROS, and driver work.\n- Never treat device-node presence as proof that a model is converted or deployable.\n\n## Verification\n\n${verification}\n\nRun the configured Hobot Code quality gate after the final code change. A task is not complete while the gate is missing, stale, or failing.\n\n## Board Safety\n\n- Do not place Hobot Code in motor, CAN, GPIO, emergency-stop, or other hard real-time control loops.\n- Confirm destructive storage, boot, power, driver, firmware, and system-service changes explicitly.\n- Record the exact target board, software version, commands, outputs, and remaining risk in deployment reports.\n`;
}

export async function initializeProject(cwd, snapshot) {
  const root = resolve(cwd);
  const commands = await detectQualityCommands(root);
  const agentsPath = join(root, "AGENTS.md");
  const configDirectory = join(root, ".hobot");
  const qualityPath = join(configDirectory, "quality-gates.json");
  const created = [];
  const preserved = [];

  try {
    await writeFile(agentsPath, agentsDocument(root, snapshot, commands), { flag: "wx", mode: 0o644 });
    created.push(agentsPath);
  } catch (error) {
    if (error?.code !== "EEXIST") throw error;
    preserved.push(agentsPath);
  }

  try {
    const info = await lstat(configDirectory);
    if (info.isSymbolicLink()) throw new Error(`${configDirectory} must not be a symbolic link`);
    if (!info.isDirectory()) throw new Error(`${configDirectory} is not a directory`);
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
    await mkdir(configDirectory, { mode: 0o755 });
  }

  const qualityConfig = { schemaVersion: 1, timeoutMs: 120_000, commands };
  try {
    await writeFile(qualityPath, `${JSON.stringify(qualityConfig, null, 2)}\n`, { flag: "wx", mode: 0o644 });
    created.push(qualityPath);
  } catch (error) {
    if (error?.code !== "EEXIST") throw error;
    preserved.push(qualityPath);
  }

  return { root, commands, created, preserved };
}

export async function fingerprintWorkspace(cwd) {
  const root = resolve(cwd);
  const hash = createHash("sha256");
  let files = 0;
  let entryCount = 0;
  let hashedBytes = 0;

  async function hashFile(path, size) {
    if (fingerprintMetadataOnlyExtensions.has(extname(path).toLowerCase())) return;
    if (size > MAX_FINGERPRINT_FILE_BYTES) {
      throw new Error(`workspace source file exceeds ${MAX_FINGERPRINT_FILE_BYTES} bytes: ${path}`);
    }
    if (hashedBytes + size > MAX_FINGERPRINT_TOTAL_BYTES) {
      throw new Error(`workspace fingerprint exceeds ${MAX_FINGERPRINT_TOTAL_BYTES} content bytes`);
    }
    await new Promise((resolveHash, rejectHash) => {
      const source = createReadStream(path);
      let fileBytes = 0;
      source.on("data", (chunk) => {
        fileBytes += chunk.length;
        if (fileBytes > MAX_FINGERPRINT_FILE_BYTES) {
          source.destroy(new Error(`workspace source file exceeds ${MAX_FINGERPRINT_FILE_BYTES} bytes: ${path}`));
          return;
        }
        if (hashedBytes + chunk.length > MAX_FINGERPRINT_TOTAL_BYTES) {
          source.destroy(new Error(`workspace fingerprint exceeds ${MAX_FINGERPRINT_TOTAL_BYTES} content bytes`));
          return;
        }
        hash.update(chunk);
        hashedBytes += chunk.length;
      });
      source.on("error", rejectHash);
      source.on("end", resolveHash);
    });
  }

  async function visit(directory, relativeDirectory = "") {
    const directoryEntries = [];
    const directoryHandle = await opendir(directory);
    for await (const entry of directoryHandle) {
      entryCount += 1;
      if (entryCount > 20_000) throw new Error("workspace fingerprint exceeds 20000 entries");
      directoryEntries.push(entry);
    }
    directoryEntries.sort((left, right) => left.name.localeCompare(right.name));
    for (const entry of directoryEntries) {
      if (entry.isDirectory() && fingerprintExcludedDirectories.has(entry.name)) continue;
      const path = join(directory, entry.name);
      const relativePath = join(relativeDirectory, entry.name);
      const info = await lstat(path);
      hash.update(`${relativePath}\0${info.mode}\0${info.size}\0${info.mtimeMs}\0`);
      if (entry.isSymbolicLink()) {
        files += 1;
        hash.update(await readlink(path));
      } else if (entry.isDirectory()) {
        await visit(path, relativePath);
      } else if (entry.isFile()) {
        files += 1;
        await hashFile(path, info.size);
      }
    }
  }

  await visit(root);
  return { digest: hash.digest("hex"), files, hashedBytes };
}
