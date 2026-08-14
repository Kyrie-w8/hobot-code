#!/usr/bin/env node

import { createHash, randomBytes } from "node:crypto";
import { constants } from "node:fs";
import { lstat, mkdir, open, realpath, rename, unlink } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const MAX_RESPONSE_BYTES = 8 * 1024 * 1024;
const MAX_ERROR_BYTES = 32 * 1024;
const MAX_REPORT_BYTES = 4 * 1024 * 1024;
const LIVE_TASK_STATES = new Set(["queued", "starting", "idle", "running", "waiting", "stopping"]);
const TASK_STATES = new Set([...LIVE_TASK_STATES, "stopped", "completed", "failed", "interrupted"]);
const TARGETS = {
  x5: { boardId: "x5", releases: new Set(["3.5.0"]) },
  s100: { boardId: "s100", releases: new Set(["4.0.5", "4.0.5-Beta"]) },
  s600: { boardId: "s600", releases: new Set(["5.1.0"]) },
};
const RELEASE_CAPABILITIES = [
  "build.identity.v1", "diagnostics.inspect.v1", "diagnostics.repair.v1", "events.items.v1", "events.normalized.v4", "events.page", "events.retention.v1", "models.conformance.v1", "models.runtime-probe.v1", "pi.compatibility.v1",
  "support.bundle.v1", "support.bundle.v2", "system.snapshot", "tasks.failure.v1", "tasks.lifecycle", "tasks.page", "tasks.queue.v1",
  "tasks.sandbox.v1", "tasks.turn-evidence.v1", "workspaces.changes.v1", "workspaces.isolation.v1",
  "workspaces.write-leases.v1",
];

export function parseDuration(value, allowZero = false) {
  const match = /^(0|[1-9]\d*)(ms|s|m|h)?$/u.exec(value);
  if (!match) throw new Error(`invalid duration: ${value}`);
  const multiplier = { ms: 1, s: 1000, m: 60_000, h: 3_600_000 }[match[2] ?? "ms"];
  const result = Number(match[1]) * multiplier;
  if (!Number.isSafeInteger(result) || result < (allowZero ? 0 : 1)) throw new Error(`invalid duration: ${value}`);
  return result;
}

export function parseBoard(value) {
  const match = /^([a-z][a-z0-9-]{0,31})=([a-z_][a-z0-9_-]{0,31})@([a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?)(?::([1-9]\d{0,4}))?$/iu.exec(value);
  if (!match) throw new Error(`invalid board: ${value}`);
  const port = match[4] ? Number(match[4]) : 22;
  if (port > 65535) throw new Error(`invalid board port: ${value}`);
  return { label: match[1].toLowerCase(), user: match[2], host: match[3], port };
}

export function parseArguments(args) {
  const options = {
    boards: [], samples: 3, intervalMs: 0, timeoutMs: 10_000, output: "", resume: false,
    restartIdle: false, expectedVersion: "", sshBinary: "ssh",
  };
  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index];
    const next = () => {
      const value = args[index + 1];
      if (!value) throw new Error(`${argument} requires a value`);
      index += 1;
      return value;
    };
    switch (argument) {
    case "--board": options.boards.push(parseBoard(next())); break;
    case "--samples": options.samples = Number(next()); break;
    case "--interval": options.intervalMs = parseDuration(next(), true); break;
    case "--timeout": options.timeoutMs = parseDuration(next()); break;
    case "--output": options.output = resolve(next()); break;
    case "--resume": options.resume = true; break;
    case "--restart-idle": options.restartIdle = true; break;
    case "--expected-version": options.expectedVersion = next(); break;
    case "--ssh": options.sshBinary = next(); break;
    case "--help": options.help = true; break;
    default: throw new Error(`unknown option: ${argument}`);
    }
  }
  if (options.help) return options;
  if (options.boards.length === 0) throw new Error("at least one --board is required");
  if (!Number.isInteger(options.samples) || options.samples < 1 || options.samples > 10_000) throw new Error("--samples must be between 1 and 10000");
  if (options.intervalMs > 0 && options.intervalMs < 1000) throw new Error("--interval must be 0 or at least 1s");
  if (options.timeoutMs < 1000 || options.timeoutMs > 60_000) throw new Error("--timeout must be between 1s and 60s");
  if (new Set(options.boards.map((board) => board.label)).size !== options.boards.length) throw new Error("board labels must be unique");
  if (!/^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/u.test(options.expectedVersion)) {
    throw new Error("--expected-version must be strict SemVer");
  }
  if (!options.output && (options.samples > 3 || options.intervalMs > 0 || options.resume)) throw new Error("long or resumable runs require --output");
  if (options.resume && !options.output) throw new Error("--resume requires --output");
  return options;
}

function sshArguments(board, command = "hobot bridge --stdio") {
  return [
    "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15",
    "-o", "ServerAliveCountMax=3", "-o", "StrictHostKeyChecking=accept-new", "-p", String(board.port),
    `${board.user}@${board.host}`, command,
  ];
}

function runProcess(binary, args, input, timeoutMs, completeOnLine = false) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(binary, args, { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = Buffer.alloc(0);
    let stderr = Buffer.alloc(0);
    let finished = false;
    let timer;
    const finish = (error, result) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      if (error) rejectPromise(error); else resolvePromise(result);
    };
    const append = (current, chunk, maximum) => {
      if (current.length + chunk.length > maximum) {
        child.kill("SIGKILL");
        finish(new Error("remote-output-limit"));
        return current;
      }
      return Buffer.concat([current, chunk]);
    };
    child.stdout.on("data", (chunk) => {
      stdout = append(stdout, chunk, MAX_RESPONSE_BYTES);
      if (!completeOnLine || finished) return;
      const lineEnd = stdout.indexOf(0x0a);
      if (lineEnd < 0) return;
      const response = stdout.subarray(0, lineEnd + 1);
      const trailing = stdout.subarray(lineEnd + 1).toString("utf8").trim();
      if (trailing) finish(new Error("protocol-response-invalid"));
      else finish(null, response);
      child.kill("SIGTERM");
    });
    child.stderr.on("data", (chunk) => { stderr = append(stderr, chunk, MAX_ERROR_BYTES); });
    child.on("error", () => finish(new Error("transport-start-failed")));
    child.on("close", (code) => {
      if (code !== 0) finish(new Error(classifyTransportError(stderr.toString("utf8"))));
      else finish(null, stdout);
    });
    timer = setTimeout(() => {
      child.kill("SIGKILL");
      finish(new Error("transport-timeout"));
    }, timeoutMs);
    child.stdin.end(input);
  });
}

export function classifyTransportError(value) {
  const message = value.toLowerCase();
  if (message.includes("permission denied")) return "authentication-failed";
  if (message.includes("host key verification")) return "host-key-failed";
  if (message.includes("connection timed out") || message.includes("operation timed out")) return "transport-timeout";
  if (message.includes("connection refused")) return "connection-refused";
  if (message.includes("no route") || message.includes("network is unreachable")) return "network-unreachable";
  return "transport-failed";
}

async function rpcCall(options, board, method, params = {}) {
  const id = createHash("sha256").update(`${method}\0${Date.now()}\0${Math.random()}`).digest("hex").slice(0, 24);
  const request = `${JSON.stringify({ protocol: 1, id, method, params })}\n`;
  const started = performance.now();
  const stdout = await runProcess(options.sshBinary, sshArguments(board), request, options.timeoutMs, true);
  const latencyMs = Math.round(performance.now() - started);
  const lines = stdout.toString("utf8").trim().split(/\r?\n/u).filter(Boolean);
  if (lines.length !== 1 || Buffer.byteLength(lines[0]) > MAX_RESPONSE_BYTES) throw new Error("protocol-response-invalid");
  let response;
  try { response = JSON.parse(lines[0]); } catch { throw new Error("protocol-response-invalid"); }
  if (response.protocol !== 1 || response.id !== id || response.ok !== true) {
    const code = typeof response?.error?.code === "string" && /^[a-z0-9_.-]{1,64}$/iu.test(response.error.code) ? response.error.code.toLowerCase() : "request-failed";
    throw new Error(response?.error ? `rpc-${code}` : "protocol-response-invalid");
  }
  return { result: response.result, latencyMs };
}

function countTaskStates(tasks) {
  const counts = {};
  for (const task of tasks) {
    const status = TASK_STATES.has(task?.status) ? task.status : "unknown";
    counts[status] = (counts[status] ?? 0) + 1;
  }
  return Object.fromEntries(Object.entries(counts).sort(([left], [right]) => left.localeCompare(right)));
}

function percentage(available, total) {
  return Number.isFinite(available) && Number.isFinite(total) && total > 0 ? Number(((available / total) * 100).toFixed(2)) : undefined;
}

function boundedInteger(value) {
  return Number.isSafeInteger(value) && value >= 0 ? value : undefined;
}

function timestamp(value) {
  if (typeof value !== "string") return undefined;
  const parsed = new Date(value);
  return Number.isNaN(parsed.valueOf()) ? undefined : parsed.toISOString();
}

function semanticVersion(value) {
  return typeof value === "string" && /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?$/u.test(value) ? value : undefined;
}

function buildIdentity(value) {
  const build = value && typeof value === "object" ? value : {};
  const statuses = new Set(["verified", "invalid", "unavailable"]);
  const reasons = new Set(["executable-unavailable", "binary-unavailable", "metadata-missing", "metadata-invalid", "metadata-mismatch", "not-reported"]);
  return {
    status: statuses.has(build.status) ? build.status : "unavailable",
    reason: reasons.has(build.reason) ? build.reason : undefined,
    commit: typeof build.commit === "string" && /^[0-9a-f]{40}$/u.test(build.commit) ? build.commit : undefined,
    dirty: typeof build.dirty === "boolean" ? build.dirty : undefined,
    builtAt: timestamp(build.builtAt),
    target: build.target === "linux-arm64" ? build.target : undefined,
    binarySha256: typeof build.binarySha256 === "string" && /^[0-9a-f]{64}$/u.test(build.binarySha256) ? build.binarySha256 : undefined,
    piVersion: semanticVersion(build.piVersion),
    piCommit: typeof build.piCommit === "string" && /^[0-9a-f]{40}$/u.test(build.piCommit) ? build.piCommit : undefined,
    piCompatibilitySha256: typeof build.piCompatibilitySha256 === "string" && /^[0-9a-f]{64}$/u.test(build.piCompatibilitySha256) ? build.piCompatibilitySha256 : undefined,
  };
}

function sandboxBoundary(value) {
  const sandbox = value && typeof value === "object" ? value : {};
  return {
    available: sandbox.available === true,
    backend: sandbox.backend === "bubblewrap" ? "bubblewrap" : undefined,
    filesystemWritesRestricted: sandbox.filesystemWritesRestricted === true,
    devicesRestricted: sandbox.devicesRestricted === true,
    capabilitiesDropped: sandbox.capabilitiesDropped === true,
    networkRestricted: sandbox.networkRestricted === true,
  };
}

function targetIdentity(snapshot) {
  const boardId = ["x5", "s100", "s600"].includes(snapshot?.boardId) ? snapshot.boardId : "unknown";
  const names = { x5: "RDK X5", s100: "RDK S100", s600: "RDK S600", unknown: "Unknown RDK" };
  const release = typeof snapshot?.rdkOsVersion === "string" && /^\d+(?:\.\d+){1,3}(?:-[0-9A-Za-z.-]+)?$/u.test(snapshot.rdkOsVersion) ? snapshot.rdkOsVersion : undefined;
  const architecture = ["arm64", "aarch64"].includes(snapshot?.architecture) ? snapshot.architecture : undefined;
  return { board: names[boardId], boardId, rdkOsVersion: release, architecture };
}

export function sanitizeSample(ping, snapshot, taskPage, latencyMs) {
  const capabilities = Array.isArray(ping?.capabilities?.capabilities)
    ? [...new Set(ping.capabilities.capabilities.filter((value) => typeof value === "string" && /^[a-z0-9_.-]{1,64}$/u.test(value)))].sort().slice(0, 256)
    : [];
  const temperatures = Array.isArray(snapshot?.thermalZones) ? snapshot.thermalZones.map((zone) => Number(zone?.celsius)).filter(Number.isFinite) : [];
  const loadAverage = Array.isArray(snapshot?.loadAverage) ? snapshot.loadAverage.map(Number).filter(Number.isFinite).slice(0, 3) : [];
  return {
    capturedAt: new Date().toISOString(), latencyMs,
    service: {
      version: semanticVersion(ping?.version), protocol: boundedInteger(ping?.protocol), pid: boundedInteger(ping?.pid), startedAt: timestamp(ping?.startedAt),
      activeTasks: boundedInteger(ping?.activeTasks) ?? 0, queuedTasks: boundedInteger(ping?.queuedTasks) ?? 0,
      configurationCurrent: typeof ping?.configurationCurrent === "boolean" ? ping.configurationCurrent : undefined,
      eventSchema: boundedInteger(ping?.capabilities?.eventSchema),
      capabilities, capabilityDigest: createHash("sha256").update(`${capabilities.join("\n")}\n`).digest("hex"),
      sandbox: sandboxBoundary(ping?.capabilities?.sandbox),
      build: buildIdentity(ping?.build ?? { status: "unavailable", reason: "not-reported" }),
    },
    target: targetIdentity(snapshot),
    resources: {
      cpuCores: boundedInteger(snapshot?.cpuCores), loadAverage,
      memoryAvailablePercent: percentage(snapshot?.memory?.availableBytes, snapshot?.memory?.totalBytes),
      diskAvailablePercent: percentage(snapshot?.disk?.availableBytes, snapshot?.disk?.totalBytes),
      maximumTemperatureC: temperatures.length > 0 ? Math.max(...temperatures) : undefined,
      bpuTelemetry: ["available", "unavailable", "degraded"].includes(snapshot?.bpuTelemetry?.status) ? snapshot.bpuTelemetry.status : undefined,
    },
    taskStatusCounts: countTaskStates(Array.isArray(taskPage?.tasks) ? taskPage.tasks : []),
  };
}

export function assessBoard(label, samples, expectedVersion) {
  const checks = [];
  const add = (name, status, summary) => checks.push({ name, status, summary });
  if (samples.length === 0) return [{ name: "connectivity", status: "fail", summary: "No successful sample was collected." }];
  const latest = samples.at(-1);
  add("connectivity", "pass", `${samples.length} independent SSH samples completed.`);
  add("version", latest.service.version === expectedVersion ? "pass" : "fail", `Board reports ${latest.service.version ?? "unknown"}; expected ${expectedVersion}.`);
  const target = TARGETS[label];
  if (!target) add("target", "warn", `No validation target is defined for ${label}.`);
  else if (latest.target.boardId !== target.boardId) add("target", "fail", `Detected ${latest.target.boardId ?? "unknown"}; expected ${target.boardId}.`);
  else add("target", target.releases.has(latest.target.rdkOsVersion) ? "pass" : "warn", `Detected ${latest.target.boardId} on RDK OS ${latest.target.rdkOsVersion ?? "unknown"}.`);
  const missing = RELEASE_CAPABILITIES.filter((capability) => !latest.service.capabilities.includes(capability));
  add("release-capabilities", missing.length === 0 ? "pass" : "fail", missing.length === 0 ? "Current release capability contract is complete." : `Missing ${missing.join(", ")}.`);
  const buildStatus = latest.service.build?.status;
  add("build-identity", buildStatus === "verified" && latest.service.build?.dirty !== true ? "pass" : "fail", buildStatus === "verified" ? `Build ${latest.service.build.commit?.slice(0, 12) ?? "verified"}${latest.service.build.dirty ? " is modified" : " is clean"}.` : `Build identity is ${buildStatus ?? "missing"}.`);
  add("pi-compatibility", latest.service.build?.piCompatibilitySha256 ? "pass" : "fail", latest.service.build?.piCompatibilitySha256 ? `Pi capability contract ${latest.service.build.piCompatibilitySha256.slice(0, 12)} is bound to this build.` : "The build does not report a Pi capability contract digest.");
  add("configuration", latest.service.configurationCurrent === false ? "fail" : "pass", latest.service.configurationCurrent === false ? "Model configuration changed after agentd started." : "Daemon configuration is current or no drift was reported.");
  const sandbox = latest.service.sandbox;
  add("sandbox", sandbox?.available && sandbox?.filesystemWritesRestricted && sandbox?.devicesRestricted && sandbox?.capabilitiesDropped ? "pass" : "fail", sandbox?.available ? "File, device, and capability isolation is available." : `Sandbox unavailable: ${sandbox?.reason ?? "not reported"}.`);
  add("network-boundary", sandbox?.networkRestricted ? "pass" : "warn", sandbox?.networkRestricted ? "Agent network access is restricted." : "Agent network access still shares the host network boundary.");
  const identities = new Set(samples.map((sample) => `${sample.service.version}|${sample.service.eventSchema}|${sample.service.capabilityDigest}|${sample.service.build?.binarySha256 ?? ""}`));
  add("sample-stability", identities.size === 1 ? "pass" : "fail", identities.size === 1 ? "Version, schema, capabilities, and binary identity stayed stable." : "Service identity changed during sampling.");
  const daemonStarts = new Set(samples.map((sample) => `${sample.service.pid}|${sample.service.startedAt}`));
  add("daemon-continuity", daemonStarts.size === 1 ? "pass" : "fail", daemonStarts.size === 1 ? "The board service did not restart during sampling." : "The board service restarted during sampling.");
  const worstLatency = Math.max(...samples.flatMap((sample) => Object.values(sample.latencyMs ?? {}).filter(Number.isFinite)), 0);
  add("control-plane-latency", worstLatency <= 5_000 ? "pass" : "warn", `Maximum SSH/RPC latency was ${worstLatency} ms.`);
  const memory = samples.map((sample) => sample.resources?.memoryAvailablePercent).filter(Number.isFinite);
  const disk = samples.map((sample) => sample.resources?.diskAvailablePercent).filter(Number.isFinite);
  const temperature = samples.map((sample) => sample.resources?.maximumTemperatureC).filter(Number.isFinite);
  const lowMemory = memory.length > 0 && Math.min(...memory) < 10;
  const lowDisk = disk.length > 0 && Math.min(...disk) < 10;
  const highTemperature = temperature.length > 0 && Math.max(...temperature) >= 90;
  const resourceEvidence = memory.length > 0 && disk.length > 0 && temperature.length > 0;
  const resourceWarning = !resourceEvidence || lowMemory || lowDisk || highTemperature;
  add("resource-headroom", resourceWarning ? "warn" : "pass", resourceEvidence
    ? `Minimum available memory ${Math.min(...memory).toFixed(2)}%, disk ${Math.min(...disk).toFixed(2)}%; maximum temperature ${Math.max(...temperature).toFixed(2)} C.`
    : "Memory, disk, or temperature evidence is incomplete.");
  return checks;
}

function assessFleet(report) {
  const latest = report.boards.map((board) => board.samples.at(-1)).filter(Boolean);
  if (latest.length !== report.boards.length) {
    return [{ name: "fleet-build-consistency", status: "fail", summary: "Not every board produced release identity evidence." }];
  }
  if (latest.some((sample) => sample.service.build?.status !== "verified" || sample.service.build?.dirty !== false || !sample.service.build?.commit || !sample.service.build?.binarySha256 || !sample.service.build?.piVersion || !sample.service.build?.piCommit || !sample.service.build?.piCompatibilitySha256)) {
    return [{ name: "fleet-build-consistency", status: "fail", summary: "Not every board reports a verified, clean, and complete release identity." }];
  }
  const identities = new Set(latest.map((sample) => [
    sample.service.version, sample.service.build?.commit, sample.service.build?.binarySha256,
    sample.service.build?.piVersion, sample.service.build?.piCommit,
    sample.service.build?.piCompatibilitySha256,
  ].join("|")));
  return [{
    name: "fleet-build-consistency", status: identities.size === 1 ? "pass" : "fail",
    summary: identities.size === 1 ? "Every board reports the same Hobot Code and Pi build identity." : "Boards with the same product version do not report one identical release build.",
  }];
}

function resultStatus(checks) {
  if (checks.some((check) => check.status === "fail")) return "fail";
  if (checks.some((check) => check.status === "warn")) return "warn";
  return "pass";
}

async function collectSample(options, board) {
  const ping = await rpcCall(options, board, "ping");
  const snapshot = await rpcCall(options, board, "system.snapshot");
  const tasks = await rpcCall(options, board, "task.page", { limit: 200, includeArchived: false });
  return sanitizeSample(ping.result, snapshot.result, tasks.result, {
    ping: ping.latencyMs, systemSnapshot: snapshot.latencyMs, taskPage: tasks.latencyMs,
  });
}

function resumableReport(options, existing = null) {
  if (existing) {
    if (existing.schema !== 1 || existing.kind !== "three-board-reliability" || existing.expectedVersion !== options.expectedVersion || !Array.isArray(existing.boards)) {
      throw new Error("resume-report-incompatible");
    }
    const labels = existing.boards.map((board) => board.label).sort().join(",");
    if (labels !== options.boards.map((board) => board.label).sort().join(",")) throw new Error("resume-report-incompatible");
    for (const board of existing.boards) {
      if (!Array.isArray(board.samples) || !Array.isArray(board.failures)) throw new Error("resume-report-incompatible");
      board.attempts = Number.isInteger(board.attempts) ? board.attempts : board.samples.length + board.failures.length;
      if (board.attempts < board.samples.length + board.failures.length || board.attempts > options.samples) throw new Error("resume-report-incompatible");
    }
    existing.requestedSamples = options.samples;
    existing.intervalMs = options.intervalMs;
    existing.resumedAt = new Date().toISOString();
    existing.completedAt = null;
    existing.status = "running";
    return existing;
  }
  return {
    schema: 1, product: "Hobot Code", kind: "three-board-reliability", expectedVersion: options.expectedVersion,
    startedAt: new Date().toISOString(), completedAt: null, requestedSamples: options.samples,
    intervalMs: options.intervalMs, privacy: "No host addresses, project paths, task IDs, prompts, commands, outputs, or credentials are retained.",
    boards: options.boards.map((board) => ({ label: board.label, attempts: 0, samples: [], failures: [], checks: [], status: "pending", restart: { status: "not-requested" } })),
    checks: [], status: "running",
  };
}

async function atomicWrite(path, value) {
  const encoded = Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
  if (encoded.length > MAX_REPORT_BYTES) throw new Error("report-size-limit");
  await mkdir(dirname(path), { recursive: true, mode: 0o700 });
  const temporary = `${path}.tmp.${process.pid}.${randomBytes(6).toString("hex")}`;
  const file = await open(temporary, "wx", 0o600);
  try {
    await file.writeFile(encoded);
    await file.sync();
    await file.close();
    await rename(temporary, path);
  } catch (error) {
    await file.close().catch(() => {});
    await unlink(temporary).catch(() => {});
    throw error;
  }
}

async function readPrivateReport(path) {
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink() || before.size <= 0 || before.size > MAX_REPORT_BYTES || (before.mode & 0o077) !== 0) {
    throw new Error("resume-report-untrusted");
  }
  if (typeof process.getuid === "function" && before.uid !== process.getuid()) throw new Error("resume-report-untrusted");
  const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  try {
    const opened = await file.stat();
    if (opened.dev !== before.dev || opened.ino !== before.ino || opened.size !== before.size) throw new Error("resume-report-untrusted");
    const content = await file.readFile();
    const after = await file.stat();
    if (content.length !== before.size || after.size !== before.size || after.mtimeMs !== before.mtimeMs) throw new Error("resume-report-untrusted");
    return JSON.parse(content.toString("utf8"));
  } finally {
    await file.close();
  }
}

function liveTaskCount(sample) {
  return Object.entries(sample.taskStatusCounts).reduce((total, [status, count]) => total + (LIVE_TASK_STATES.has(status) ? count : 0), 0);
}

async function restartIdleBoard(options, board, boardReport) {
  const latest = boardReport.samples.at(-1);
  if (!latest || latest.service.activeTasks !== 0 || latest.service.queuedTasks !== 0 || liveTaskCount(latest) !== 0) {
    boardReport.restart = { status: "skipped", reason: "active-tasks" };
    return;
  }
  const before = { pid: latest.service.pid, startedAt: latest.service.startedAt, taskStatusCounts: latest.taskStatusCounts };
  try {
    await runProcess(options.sshBinary, sshArguments(board, "hobot daemon restart"), "", options.timeoutMs);
  } catch (error) {
    boardReport.restart = { status: "fail", reason: error instanceof Error ? error.message : "restart-failed" };
    return;
  }
  let after;
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try { after = await collectSample(options, board); break; } catch { await new Promise((resolvePromise) => setTimeout(resolvePromise, 250)); }
  }
  const passed = Boolean(after && after.service.pid !== before.pid && after.service.startedAt !== before.startedAt && JSON.stringify(after.taskStatusCounts) === JSON.stringify(before.taskStatusCounts));
  boardReport.restart = {
    status: passed ? "pass" : "fail", checkedAt: new Date().toISOString(),
    pidChanged: Boolean(after && after.service.pid !== before.pid), tasksPreserved: Boolean(after && JSON.stringify(after.taskStatusCounts) === JSON.stringify(before.taskStatusCounts)),
  };
}

export async function runReliability(options) {
  let existing = null;
  if (options.resume) {
    try { existing = await readPrivateReport(options.output); } catch (error) {
      if (error instanceof Error && error.message === "resume-report-untrusted") throw error;
      throw new Error("resume-report-unavailable");
    }
  }
  const report = resumableReport(options, existing);
  const reportByLabel = new Map(report.boards.map((board) => [board.label, board]));
  while (report.boards.some((board) => board.attempts < options.samples)) {
    const targets = options.boards.filter((board) => reportByLabel.get(board.label).attempts < options.samples);
    const results = await Promise.all(targets.map(async (board) => {
      try { return { board, sample: await collectSample(options, board) }; }
      catch (error) { return { board, error: error instanceof Error ? error.message : "unknown-failure" }; }
    }));
    for (const result of results) {
      const boardReport = reportByLabel.get(result.board.label);
      boardReport.attempts += 1;
      if (result.sample) boardReport.samples.push(result.sample);
      else boardReport.failures.push({ capturedAt: new Date().toISOString(), category: result.error });
    }
    report.updatedAt = new Date().toISOString();
    if (options.output) await atomicWrite(options.output, report);
    if (report.boards.some((board) => board.attempts < options.samples) && options.intervalMs > 0) {
      await new Promise((resolvePromise) => setTimeout(resolvePromise, options.intervalMs));
    }
  }
  if (options.restartIdle) {
    for (const board of options.boards) await restartIdleBoard(options, board, reportByLabel.get(board.label));
  }
  for (const board of report.boards) {
    board.checks = assessBoard(board.label, board.samples, options.expectedVersion);
    if (board.failures.length > 0) board.checks.push({ name: "sampling-failures", status: "fail", summary: `${board.failures.length} sample attempts failed.` });
    if (board.restart.status === "fail") board.checks.push({ name: "daemon-restart", status: "fail", summary: "Idle daemon restart did not preserve identity and task state." });
    if (board.restart.status === "pass") board.checks.push({ name: "daemon-restart", status: "pass", summary: "Idle daemon restarted with task state preserved." });
    if (board.restart.status === "skipped") board.checks.push({ name: "daemon-restart", status: "warn", summary: "Restart was skipped because the board has a live or queued task." });
    board.status = resultStatus(board.checks);
  }
  report.checks = assessFleet(report);
  report.status = resultStatus([...report.checks, ...report.boards.flatMap((board) => board.checks)]);
  report.completedAt = new Date().toISOString();
  if (options.output) await atomicWrite(options.output, report);
  return report;
}

function usage() {
  return `Hobot Code three-board reliability verifier

Usage:
  node scripts/board-reliability.mjs --expected-version VERSION \\
    --board x5=root@HOST --board s100=root@HOST --board s600=root@HOST [options]

Options:
  --samples N          Independent samples per board (default 3, max 10000)
  --interval 5m        Delay between samples; 0 disables delay
  --timeout 10s        Per SSH/RPC operation timeout
  --output FILE        Private resumable JSON report (required for long runs)
  --resume             Continue an existing report
  --restart-idle       Restart agentd only when the board has no active tasks
  --ssh PATH           OpenSSH-compatible executable, useful for tests
`;
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  if (options.help) { process.stdout.write(usage()); return; }
  const report = await runReliability(options);
  process.stdout.write(`${JSON.stringify({ status: report.status, output: options.output || null, boards: report.boards.map((board) => ({ label: board.label, status: board.status, attempts: board.attempts, samples: board.samples.length, failures: board.failures.length, restart: board.restart.status })) }, null, 2)}\n`);
  if (report.status === "fail") process.exitCode = 1;
}

if (process.argv[1] && await realpath(process.argv[1]) === await realpath(fileURLToPath(import.meta.url))) {
  await main().catch((error) => {
    process.stderr.write(`Reliability check failed: ${error instanceof Error ? error.message : "unknown-error"}\n`);
    process.exitCode = 2;
  });
}
