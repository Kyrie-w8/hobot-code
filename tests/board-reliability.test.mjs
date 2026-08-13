import assert from "node:assert/strict";
import { chmod, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { assessBoard, classifyTransportError, parseArguments, parseBoard, parseDuration, runReliability, sanitizeSample } from "../scripts/board-reliability.mjs";

const RELEASE_CAPABILITIES = [
  "build.identity.v1", "events.items.v1", "events.normalized.v4", "events.page", "models.conformance.v1",
  "support.bundle.v1", "system.snapshot", "tasks.failure.v1", "tasks.lifecycle", "tasks.page", "tasks.queue.v1",
  "tasks.sandbox.v1", "tasks.turn-evidence.v1", "workspaces.changes.v1", "workspaces.isolation.v1",
  "workspaces.write-leases.v1",
];

test("reliability CLI parses bounded board and soak settings", () => {
  assert.deepEqual(parseBoard("s100=root@10.0.0.2:2222"), { label: "s100", user: "root", host: "10.0.0.2", port: 2222 });
  assert.equal(parseDuration("5m"), 300_000);
  assert.throws(() => parseBoard("s100=root@host;touch_/tmp/x"), /invalid board/);
  assert.throws(() => parseArguments(["--board", "x5=root@host", "--expected-version", "0.26.0", "--samples", "4"]), /require --output/);
});

test("samples retain operational evidence without host, task, or workspace identity", () => {
  const sample = sanitizeSample({
    version: "0.26.0", protocol: 1, pid: 42, startedAt: "2026-08-13T00:00:00Z", activeTasks: 1,
    capabilities: { eventSchema: 4, capabilities: ["tasks.lifecycle", "build.identity.v1"], sandbox: { available: true } },
    build: { status: "verified", commit: "a".repeat(40), binarySha256: "b".repeat(64), piVersion: "0.84.1" },
  }, {
    board: "RDK S100", boardId: "s100", hostname: "private-host", rdkOsVersion: "4.0.5-Beta", architecture: "arm64",
    cpuCores: 8, memory: { totalBytes: 100, availableBytes: 25 }, disk: { totalBytes: 100, availableBytes: 50 },
    thermalZones: [{ name: "soc", celsius: 48 }], bpuTelemetry: { status: "available" },
  }, { tasks: [{ id: "private-task", status: "running", cwd: "/private/project", name: "secret prompt" }] }, { ping: 1, systemSnapshot: 2, taskPage: 3 });
  const encoded = JSON.stringify(sample);
  assert.equal(sample.taskStatusCounts.running, 1);
  assert.equal(sample.resources.memoryAvailablePercent, 25);
  assert.doesNotMatch(encoded, /private-host|private-task|private\/project|secret prompt/);
});

test("release assessment distinguishes daily connectivity from reproducible release readiness", () => {
  const sample = {
    service: {
      version: "0.26.0", eventSchema: 4, capabilities: RELEASE_CAPABILITIES, capabilityDigest: "digest",
      configurationCurrent: true, build: { status: "verified", commit: "a".repeat(40), dirty: false, binarySha256: "b".repeat(64) },
      sandbox: { available: true, filesystemWritesRestricted: true, devicesRestricted: true, capabilitiesDropped: true, networkRestricted: false },
    },
    target: { boardId: "s100", rdkOsVersion: "4.0.5-Beta" }, taskStatusCounts: {},
  };
  const checks = assessBoard("s100", [sample, structuredClone(sample)], "0.26.0");
  assert.equal(checks.find((check) => check.name === "connectivity").status, "pass");
  assert.equal(checks.find((check) => check.name === "release-capabilities").status, "pass");
  assert.equal(checks.find((check) => check.name === "network-boundary").status, "warn");
  const old = structuredClone(sample);
  old.service.capabilities = ["tasks.lifecycle"];
  assert.equal(assessBoard("s100", [old], "0.26.0").find((check) => check.name === "release-capabilities").status, "fail");
});

test("SSH failures are categorized without retaining remote diagnostics", () => {
  assert.equal(classifyTransportError("root@host: Permission denied (publickey)"), "authentication-failed");
  assert.equal(classifyTransportError("ssh: connect to host secret port 22: No route to host"), "network-unreachable");
  assert.equal(classifyTransportError("unexpected token=secret"), "transport-failed");
});

test("private reliability reports resume by attempt count without retaining remote identity", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "hobot-reliability-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const ssh = join(directory, "fake-ssh.mjs");
  const reportPath = join(directory, "report.json");
  await writeFile(ssh, `#!/usr/bin/env node
let input = '';
for await (const chunk of process.stdin) input += chunk;
const request = JSON.parse(input);
const capabilities = ${JSON.stringify(RELEASE_CAPABILITIES)};
const result = request.method === 'ping' ? {
  version: '0.26.0', protocol: 1, pid: 42, startedAt: '2026-08-13T00:00:00Z', activeTasks: 0, queuedTasks: 0,
  configurationCurrent: true,
  capabilities: {eventSchema: 4, capabilities, sandbox: {available: true, backend: 'bubblewrap', filesystemWritesRestricted: true, devicesRestricted: true, capabilitiesDropped: true, networkRestricted: true}},
  build: {status: 'verified', commit: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', dirty: false, builtAt: '2026-08-13T00:00:00Z', target: 'linux-arm64', binarySha256: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', piVersion: '0.84.1', piCommit: 'cccccccccccccccccccccccccccccccccccccccc'},
} : request.method === 'system.snapshot' ? {
  board: 'untrusted remote text', boardId: 's100', hostname: 'private-host', rdkOsVersion: '4.0.5-Beta', architecture: 'arm64', cpuCores: 8,
  loadAverage: [0.1, 0.2, 0.3], memory: {totalBytes: 100, availableBytes: 80}, disk: {totalBytes: 100, availableBytes: 70},
  thermalZones: [{name: 'secret-zone', celsius: 45}], bpuTelemetry: {status: 'available'},
} : {tasks: [{id: 'private-task', status: 'stopped', name: 'secret prompt', cwd: '/private/project'}]};
process.stdout.write(JSON.stringify({protocol: 1, id: request.id, ok: true, result}) + '\\n');
setInterval(() => {}, 1000);
`);
  await chmod(ssh, 0o755);
  const initial = parseArguments(["--board", "s100=root@private-host", "--expected-version", "0.26.0", "--samples", "1", "--output", reportPath, "--ssh", ssh]);
  const first = await runReliability(initial);
  assert.equal(first.boards[0].attempts, 1);
  const resumed = parseArguments(["--board", "s100=root@private-host", "--expected-version", "0.26.0", "--samples", "2", "--output", reportPath, "--resume", "--ssh", ssh]);
  const second = await runReliability(resumed);
  assert.equal(second.boards[0].attempts, 2);
  assert.equal(second.boards[0].samples.length, 2);
  assert.equal((await stat(reportPath)).mode & 0o077, 0);
  const encoded = await readFile(reportPath, "utf8");
  assert.doesNotMatch(encoded, /private-host|private-task|private\/project|secret prompt|untrusted remote text|secret-zone/);
});
