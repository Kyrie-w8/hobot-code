import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { access, chmod, lstat, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const MAX_REMOTE_COMMAND_CHARS = 64 * 1024;
const MAX_REMOTE_OUTPUT_BYTES = 1024 * 1024;
const buildHostTargetPattern = /^[a-zA-Z0-9][a-zA-Z0-9._%-]*(?:@[a-zA-Z0-9][a-zA-Z0-9.-]*)?$/;

export function normalizeBuildHostTarget(value) {
  const target = String(value ?? "").trim();
  if (!target || target.length > 253 || target.startsWith("-") || !buildHostTargetPattern.test(target)) {
    throw new Error("build host must be an SSH alias, hostname, or user@hostname without spaces or command options");
  }
  return target;
}

export function normalizeRemoteBuildCommand(value) {
  const command = String(value ?? "");
  if (!command.trim() || command.length > MAX_REMOTE_COMMAND_CHARS || command.includes("\0")) {
    throw new Error(`remote build command must contain 1-${MAX_REMOTE_COMMAND_CHARS} characters without NUL bytes`);
  }
  return command;
}

export function shellUsesSSHHost(command, target) {
  const normalized = normalizeBuildHostTarget(target);
  const escaped = normalized.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(
    `(?:^|[;&|()\\n]\\s*|\\s)(?:sudo\\s+)?(?:\\/[^\\s;|]+\\/)?ssh\\b[^;&|\\n]*?(?:^|\\s)["']?${escaped}["']?(?=\\s|[;&|)\\n]|$)`,
    "i",
  );
  return pattern.test(String(command ?? ""));
}

export function buildHostStatePath(agentDirectory = process.env.HOBOT_CODING_AGENT_DIR) {
  const root = String(agentDirectory ?? "").trim();
  if (!root || !root.startsWith("/")) throw new Error("HOBOT_CODING_AGENT_DIR must be an absolute path");
  return resolve(root, "openexplorer-build-host.json");
}

export async function loadBuildHostState(path = buildHostStatePath()) {
  try {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0 || info.size <= 0 || info.size > 4096) {
      throw new Error("OpenExplorer build host state failed private-file checks");
    }
    const value = JSON.parse(await readFile(path, "utf8"));
    const allowedKeys = value?.schemaVersion === 1
      ? ["schemaVersion", "target", "updatedAt"]
      : value?.schemaVersion === 2
        ? ["schemaVersion", "target", "updatedAt", "verifiedAt"]
        : ["schemaVersion", "target", "updatedAt", "verifiedAt", "trustedAt"];
    if (![1, 2, 3].includes(value?.schemaVersion) || Object.keys(value).some((key) => !allowedKeys.includes(key))) {
      throw new Error("OpenExplorer build host state has an unsupported format");
    }
    const target = normalizeBuildHostTarget(value.target);
    const verifiedAt = value.schemaVersion >= 2 && value.verifiedAt !== undefined
      ? String(value.verifiedAt)
      : undefined;
    if (verifiedAt !== undefined && !Number.isFinite(Date.parse(verifiedAt))) {
      throw new Error("OpenExplorer build host state has an invalid verification timestamp");
    }
    // Schema 2 represented a successful probe as implicit task trust. Preserve
    // that meaning when upgrading an existing task state.
    const trustedAt = value.schemaVersion === 2
      ? verifiedAt
      : value.schemaVersion === 3 && value.trustedAt !== undefined
        ? String(value.trustedAt)
        : undefined;
    if (trustedAt !== undefined && !Number.isFinite(Date.parse(trustedAt))) {
      throw new Error("OpenExplorer build host state has an invalid trust timestamp");
    }
    return { target, verifiedAt, trustedAt };
  } catch (error) {
    if (error?.code === "ENOENT") return undefined;
    throw error;
  }
}

export async function loadSelectedBuildHost(path = buildHostStatePath()) {
  return (await loadBuildHostState(path))?.target;
}

async function writeBuildHostState(value, path) {
  const directory = dirname(path);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await access(directory, constants.R_OK | constants.W_OK);
  const temporary = `${path}.new.${process.pid}`;
  try {
    await writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600, flag: "wx" });
    await chmod(temporary, 0o600);
    await rename(temporary, path);
  } catch (error) {
    await rm(temporary, { force: true }).catch(() => undefined);
    throw error;
  }
}

export async function saveSelectedBuildHost(target, path = buildHostStatePath()) {
  const normalized = normalizeBuildHostTarget(target);
  const current = await loadBuildHostState(path);
  await writeBuildHostState({
    schemaVersion: 3,
    target: normalized,
    updatedAt: new Date().toISOString(),
    ...(current?.target === normalized && current.verifiedAt ? { verifiedAt: current.verifiedAt } : {}),
    ...(current?.target === normalized && current.trustedAt ? { trustedAt: current.trustedAt } : {}),
  }, path);
  return normalized;
}

export async function markBuildHostVerified(target, path = buildHostStatePath()) {
  const normalized = normalizeBuildHostTarget(target);
  const current = await loadBuildHostState(path);
  if (!current || current.target !== normalized) {
    throw new Error("OpenExplorer build host changed before verification completed");
  }
  const verifiedAt = new Date().toISOString();
  await writeBuildHostState({
    schemaVersion: 3,
    target: normalized,
    updatedAt: verifiedAt,
    verifiedAt,
    ...(current.trustedAt ? { trustedAt: current.trustedAt } : {}),
  }, path);
  return verifiedAt;
}

export async function isBuildHostVerified(target, path = buildHostStatePath()) {
  if (!target) return false;
  const normalized = normalizeBuildHostTarget(target);
  const current = await loadBuildHostState(path);
  return Boolean(current?.target === normalized && current.verifiedAt);
}

export async function markBuildHostTrusted(target, path = buildHostStatePath()) {
  const normalized = normalizeBuildHostTarget(target);
  const current = await loadBuildHostState(path);
  if (!current || current.target !== normalized || !current.verifiedAt) {
    throw new Error("OpenExplorer build host must pass verification before it can be trusted");
  }
  const trustedAt = new Date().toISOString();
  await writeBuildHostState({
    schemaVersion: 3,
    target: normalized,
    updatedAt: trustedAt,
    verifiedAt: current.verifiedAt,
    trustedAt,
  }, path);
  return trustedAt;
}

export async function isBuildHostTrusted(target, path = buildHostStatePath()) {
  if (!target) return false;
  const normalized = normalizeBuildHostTarget(target);
  const current = await loadBuildHostState(path);
  return Boolean(current?.target === normalized && current.trustedAt);
}

function sshArguments(target) {
  return [
    "-T",
    "-o", "BatchMode=yes",
    "-o", "ClearAllForwardings=yes",
    "-o", "ConnectTimeout=10",
    "-o", "ServerAliveInterval=15",
    "-o", "ServerAliveCountMax=3",
    normalizeBuildHostTarget(target),
  ];
}

async function executeSSH(target, remoteCommand, options = {}) {
  const timeoutMs = Math.min(Math.max(Number(options.timeoutMs) || 30_000, 1_000), 30 * 60 * 1000);
  try {
    const result = await execFileAsync("ssh", [...sshArguments(target), remoteCommand], {
      encoding: "utf8",
      maxBuffer: MAX_REMOTE_OUTPUT_BYTES,
      timeout: timeoutMs,
      signal: options.signal,
      windowsHide: true,
    });
    return { ok: true, target: normalizeBuildHostTarget(target), stdout: result.stdout, stderr: result.stderr, exitCode: 0 };
  } catch (error) {
    return {
      ok: false,
      target: normalizeBuildHostTarget(target),
      stdout: String(error?.stdout ?? ""),
      stderr: String(error?.stderr ?? error?.message ?? error),
      exitCode: Number.isInteger(error?.code) ? error.code : null,
      timedOut: Boolean(error?.killed),
    };
  }
}

export async function probeOpenExplorerBuildHost(target, options = {}) {
  const command = [
    "printf 'architecture=%s\\n' \"$(uname -m)\"",
    "printf 'hostname=%s\\n' \"$(hostname)\"",
    "if command -v nvidia-smi >/dev/null 2>&1; then printf 'cuda_host=yes\\n'; nvidia-smi --query-gpu=name,driver_version --format=csv,noheader | head -1; else printf 'cuda_host=no\\n'; fi",
  ].join("; ");
  const result = await executeSSH(target, command, { ...options, timeoutMs: 20_000 });
  const architecture = /^architecture=(.+)$/m.exec(result.stdout)?.[1]?.trim() ?? "unknown";
  const cuda = /^cuda_host=yes$/m.test(result.stdout);
  return { ...result, architecture, cuda, compatible: result.ok && architecture === "x86_64" };
}

export async function runOpenExplorerRemoteCommand(target, command, options = {}) {
  const normalizedCommand = normalizeRemoteBuildCommand(command);
  const probe = await probeOpenExplorerBuildHost(target, options);
  if (!probe.ok) return { ...probe, stage: "probe" };
  if (!probe.compatible) {
    return { ...probe, ok: false, stage: "probe", stderr: `OpenExplorer build host must be x86_64; detected ${probe.architecture}` };
  }
  if (options.requiresCUDA && !probe.cuda) {
    return { ...probe, ok: false, stage: "probe", stderr: "This OpenExplorer workflow requires an NVIDIA CUDA build host, but nvidia-smi is unavailable" };
  }
  const encoded = Buffer.from(normalizedCommand, "utf8").toString("base64");
  const remoteCommand = `printf %s '${encoded}' | base64 -d | sh`;
  return { ...(await executeSSH(target, remoteCommand, options)), stage: "command", architecture: probe.architecture, cuda: probe.cuda };
}
