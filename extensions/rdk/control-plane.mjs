import { createHash } from "node:crypto";
import { access, chmod, lstat, mkdir, readFile, readdir, rename, writeFile } from "node:fs/promises";
import { basename, join, resolve } from "node:path";

export const DEFAULT_POLICY = Object.freeze({
  schemaVersion: 1,
  default: "ask",
  rules: Object.freeze([
    Object.freeze({ tool: "read", action: "allow" }),
    Object.freeze({ tool: "write", action: "allow" }),
    Object.freeze({ tool: "edit", action: "allow" }),
    Object.freeze({ tool: "bash", action: "allow" }),
    Object.freeze({ tool: "system_snapshot", action: "allow" }),
    Object.freeze({ tool: "rdk_docs_search", action: "allow" }),
    Object.freeze({ tool: "quality_gate", action: "ask" }),
    Object.freeze({ tool: "memory_search", action: "allow" }),
    Object.freeze({ tool: "memory_save", action: "ask" }),
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

const permissionActions = new Set(["allow", "ask", "deny"]);
const fingerprintExcludedDirectories = new Set([".git", ".cache", "build", "dist", "node_modules"]);

function cloneDefaultPolicy() {
  return {
    schemaVersion: DEFAULT_POLICY.schemaVersion,
    default: DEFAULT_POLICY.default,
    rules: DEFAULT_POLICY.rules.map((rule) => ({ ...rule })),
  };
}

export function parsePolicy(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("permission policy must be a JSON object");
  }
  if (value.schemaVersion !== 1) throw new Error("permission policy schemaVersion must be 1");
  if (!permissionActions.has(value.default)) {
    throw new Error("permission policy default must be allow, ask, or deny");
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
    return { tool, action: rule.action };
  });

  return { schemaVersion: 1, default: value.default, rules };
}

export async function loadPolicy(path) {
  try {
    return { policy: parsePolicy(JSON.parse(await readFile(path, "utf8"))) };
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
  await writeFile(temporary, `${JSON.stringify(normalized, null, 2)}\n`, { mode: 0o640 });
  await chmod(temporary, 0o640);
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
    if (rule.tool.toLowerCase() === "mcp:*") {
      if (mcp) return rule.action;
      continue;
    }
    if (wildcardMatches(toolName, rule.tool)) return rule.action;
  }
  return policy.default;
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

export function redactSensitiveText(value, limit = 800) {
  const text = String(value ?? "")
    .replace(/-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----[\s\S]*?(?:-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|$)/gi, "[REDACTED PRIVATE KEY]")
    .replace(/\b(?:sk|ak)-[A-Za-z0-9_-]{8,}\b/g, "[REDACTED]")
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
  let hashedBytes = 0;

  async function visit(directory, relativeDirectory = "") {
    const entries = await readdir(directory, { withFileTypes: true });
    entries.sort((left, right) => left.name.localeCompare(right.name));
    for (const entry of entries) {
      if (entry.isDirectory() && fingerprintExcludedDirectories.has(entry.name)) continue;
      const path = join(directory, entry.name);
      const relativePath = join(relativeDirectory, entry.name);
      const info = await lstat(path);
      hash.update(`${relativePath}\0${info.mode}\0${info.size}\0${info.mtimeMs}\0`);
      if (entry.isSymbolicLink()) {
        files += 1;
      } else if (entry.isDirectory()) {
        await visit(path, relativePath);
      } else if (entry.isFile()) {
        files += 1;
        if (files > 20_000) throw new Error("workspace fingerprint exceeds 20000 files");
        if (hashedBytes < 32 * 1024 * 1024 && info.size <= 4 * 1024 * 1024) {
          const content = await readFile(path);
          hash.update(content);
          hashedBytes += content.length;
        }
      }
    }
  }

  await visit(root);
  return { digest: hash.digest("hex"), files, hashedBytes };
}
