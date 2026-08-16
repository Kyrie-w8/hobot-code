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

export function buildHostStatePath(agentDirectory = process.env.HOBOT_CODING_AGENT_DIR) {
  const root = String(agentDirectory ?? "").trim();
  if (!root || !root.startsWith("/")) throw new Error("HOBOT_CODING_AGENT_DIR must be an absolute path");
  return resolve(root, "openexplorer-build-host.json");
}

export async function loadSelectedBuildHost(path = buildHostStatePath()) {
  try {
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0 || info.size <= 0 || info.size > 4096) {
      throw new Error("OpenExplorer build host state failed private-file checks");
    }
    const value = JSON.parse(await readFile(path, "utf8"));
    if (value?.schemaVersion !== 1 || Object.keys(value).some((key) => !["schemaVersion", "target", "updatedAt"].includes(key))) {
      throw new Error("OpenExplorer build host state has an unsupported format");
    }
    return normalizeBuildHostTarget(value.target);
  } catch (error) {
    if (error?.code === "ENOENT") return undefined;
    throw error;
  }
}

export async function saveSelectedBuildHost(target, path = buildHostStatePath()) {
  const normalized = normalizeBuildHostTarget(target);
  const directory = dirname(path);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await access(directory, constants.R_OK | constants.W_OK);
  const temporary = `${path}.new.${process.pid}`;
  const value = { schemaVersion: 1, target: normalized, updatedAt: new Date().toISOString() };
  try {
    await writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600, flag: "wx" });
    await chmod(temporary, 0o600);
    await rename(temporary, path);
  } catch (error) {
    await rm(temporary, { force: true }).catch(() => undefined);
    throw error;
  }
  return normalized;
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
