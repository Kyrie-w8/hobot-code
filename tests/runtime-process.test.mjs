import assert from "node:assert/strict";
import { access, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { HOOK_MAX_INPUT_BYTES, runHooks } from "../extensions/rdk/hook-runner.ts";
import {
  LSP_MAX_DIAGNOSTIC_BYTES,
  LSP_MAX_DIAGNOSTIC_ENTRIES,
  LSP_MAX_DOCUMENT_BYTES,
  LSP_MAX_HEADER_BYTES,
  LSP_MAX_MESSAGE_BYTES,
  LSP_MAX_OPEN_DOCUMENTS,
  LSP_SHUTDOWN_TIMEOUT_MS,
  LspManager,
} from "../extensions/rdk/lsp-manager.ts";

const fixtureRoot = dirname(fileURLToPath(import.meta.url));
const lspFixture = resolve(fixtureRoot, "fixtures", "lsp-server-requests.mjs");

function hookConfig(command, overrides = {}) {
  return {
    schemaVersion: 1,
    enabled: true,
    failurePolicy: "warn",
    timeoutMs: 2000,
    maxOutputChars: 1000,
    allowProjectHooks: false,
    hooks: [{ name: "fixture", event: "PreToolUse", tool: "bash", command }],
    ...overrides,
  };
}

function lspConfig(command, overrides = {}) {
  return {
    schemaVersion: 1,
    enabled: true,
    maxProcesses: 1,
    maxMemoryMiB: 256,
    idleTimeoutMs: 5000,
    requestTimeoutMs: 1500,
    diagnosticsWaitMs: 0,
    servers: [{ id: "fixture", extensions: [".ts"], languageId: "typescript", command }],
    ...overrides,
  };
}

async function missing(path) {
  try {
    await access(path);
    return false;
  } catch {
    return true;
  }
}

test("hooks do not spawn after pre-abort or oversized input", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-bounds-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const marker = join(root, "spawned");
  const command = [
    process.execPath,
    "-e",
    `require("node:fs").writeFileSync(${JSON.stringify(marker)}, "spawned")`,
  ];

  const controller = new AbortController();
  controller.abort();
  const aborted = await runHooks({
    config: hookConfig(command),
    event: "PreToolUse",
    toolName: "bash",
    toolCallId: "abort",
    cwd: root,
    input: { command: "true" },
    auditPath: join(root, "abort.jsonl"),
    signal: controller.signal,
  });
  assert.match(aborted.warnings[0], /aborted before start/);
  assert.equal(await missing(marker), true);

  const oversized = await runHooks({
    config: hookConfig(command),
    event: "PreToolUse",
    toolName: "bash",
    toolCallId: "oversized",
    cwd: root,
    input: { value: "x".repeat(HOOK_MAX_INPUT_BYTES + 1) },
    auditPath: join(root, "oversized.jsonl"),
  });
  assert.match(oversized.warnings[0], /input exceeds/);
  assert.equal(await missing(marker), true);
});

test("hook stdin EPIPE is reported instead of crashing the agent", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-epipe-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const command = [
    process.execPath,
    "-e",
    "require('node:fs').closeSync(0); setTimeout(() => process.exit(0), 100)",
  ];
  const result = await runHooks({
    config: hookConfig(command),
    event: "PreToolUse",
    toolName: "bash",
    toolCallId: "epipe",
    cwd: root,
    input: { value: "x".repeat(512_000) },
    auditPath: join(root, "audit.jsonl"),
  });
  assert.equal(result.blocked, false);
  assert.match(result.warnings[0], /stdin failed/);
});

test("hooks discard interpreter injection variables but retain PATH", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-env-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const injectionNames = [
    "CDPATH",
    "PERL5LIB",
    "PERLLIB",
    "PYTHONHOME",
    "PYTHONPATH",
    "PYTHONSTARTUP",
    "RUBYLIB",
    "ZDOTDIR",
  ];
  const previous = new Map(injectionNames.map((name) => [name, process.env[name]]));
  for (const name of injectionNames) process.env[name] = "/tmp/untrusted-runtime-path";
  try {
    const command = [
      process.execPath,
      "-e",
      `const names=${JSON.stringify(injectionNames)}; console.log(JSON.stringify({appendText: JSON.stringify({injected: names.some((name) => process.env[name] !== undefined), hasPath: Boolean(process.env.PATH)})}))`,
    ];
    const result = await runHooks({
      config: hookConfig(command),
      event: "PreToolUse",
      toolName: "bash",
      toolCallId: "env",
      cwd: root,
      input: { command: "true" },
      auditPath: join(root, "audit.jsonl"),
    });
    assert.deepEqual(JSON.parse(result.appendText), { injected: false, hasPath: true });
  } finally {
    for (const [name, value] of previous) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
  }
});

test("combined hook text stays within the configured output budget", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-output-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const command = [
    process.execPath,
    "-e",
    "console.log(JSON.stringify({appendText: 'x'.repeat(300)}))",
  ];
  const config = hookConfig(command, {
    maxOutputChars: 400,
    hooks: [
      { name: "one", event: "PreToolUse", tool: "bash", command },
      { name: "two", event: "PreToolUse", tool: "bash", command },
    ],
  });
  const result = await runHooks({
    config,
    event: "PreToolUse",
    toolName: "bash",
    toolCallId: "output",
    cwd: root,
    input: { command: "true" },
    auditPath: join(root, "audit.jsonl"),
  });
  assert.equal(result.appendText?.length, config.maxOutputChars);
});

test("hooks serialize cyclic and bigint plugin details without crashing", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-details-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const command = [
    process.execPath,
    "-e",
    "let input=''; process.stdin.on('data', (chunk) => input += chunk); process.stdin.on('end', () => { const value=JSON.parse(input); console.log(JSON.stringify({appendText: JSON.stringify(value.result.details)})); });",
  ];
  const details = { counter: 1n };
  details.self = details;
  const result = await runHooks({
    config: hookConfig(command, {
      hooks: [{ name: "fixture", event: "PostToolUse", tool: "custom", command }],
    }),
    event: "PostToolUse",
    toolName: "custom",
    toolCallId: "details",
    cwd: root,
    input: {},
    result: { content: [], details, isError: false },
    auditPath: join(root, "audit.jsonl"),
  });
  assert.deepEqual(JSON.parse(result.appendText), { counter: "1", self: "[Circular]" });

  const throwingDetails = {};
  Object.defineProperty(throwingDetails, "value", {
    enumerable: true,
    get() { throw new Error("getter failed"); },
  });
  const serializationFailure = await runHooks({
    config: hookConfig(command),
    event: "PreToolUse",
    toolName: "bash",
    toolCallId: "throwing-details",
    cwd: root,
    input: throwingDetails,
    auditPath: join(root, "serialization-audit.jsonl"),
  });
  assert.equal(serializationFailure.blocked, false);
  assert.match(serializationFailure.warnings[0], /serialization failed: getter failed/);
});

test("hook stdout and stderr share one output budget", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-streams-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const maxOutputChars = 400;
  const command = [
    process.execPath,
    "-e",
    "process.stdout.write('x'.repeat(300)); process.stderr.write('y'.repeat(300))",
  ];
  const result = await runHooks({
    config: hookConfig(command, { maxOutputChars }),
    event: "PreToolUse",
    toolName: "bash",
    toolCallId: "streams",
    cwd: root,
    input: { command: "true" },
    auditPath: join(root, "audit.jsonl"),
  });
  assert.equal(result.blocked, false);
  assert.match(result.warnings[0], /output exceeds 400/);
  assert.ok(result.warnings.join("").length <= maxOutputChars);
});

test("LSP initialization is shared and server requests are answered", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-ready-"));
  const manager = new LspManager(lspConfig([process.execPath, lspFixture]));
  t.after(async () => {
    await manager.stopAll();
    await rm(root, { recursive: true, force: true });
  });
  await writeFile(join(root, "sample.ts"), "export const value = 1;\n");

  const results = await Promise.all(Array.from({ length: 24 }, (_, index) => manager.query({
    action: index % 2 === 0 ? "hover" : "symbols",
    path: "sample.ts",
    root,
    line: 1,
    column: 14,
  })));
  for (const result of results) {
    assert.equal(result.initializeCount, 1);
    assert.equal(result.openedDocuments, 1);
    assert.equal(result.duplicateDidOpen, 0);
  }
  assert.equal(manager.status().running.length, 1);
});

test("LSP serializes capacity accounting across distinct concurrent documents", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-open-capacity-"));
  const manager = new LspManager(lspConfig([process.execPath, lspFixture]));
  t.after(async () => {
    await manager.stopAll();
    await rm(root, { recursive: true, force: true });
  });
  const paths = Array.from({ length: LSP_MAX_OPEN_DOCUMENTS + 1 }, (_, index) => `sample-${index}.ts`);
  await Promise.all(paths.map((path) => writeFile(join(root, path), "")));

  const outcomes = await Promise.allSettled(paths.map((path) => manager.query({ action: "hover", path, root })));
  const fulfilled = outcomes.filter((outcome) => outcome.status === "fulfilled");
  const rejected = outcomes.filter((outcome) => outcome.status === "rejected");
  assert.equal(fulfilled.length, LSP_MAX_OPEN_DOCUMENTS);
  assert.equal(rejected.length, 1);
  assert.match(String(rejected[0].reason), /open documents exceed/);
  const [status] = manager.status().running;
  assert.equal(status.openedDocuments, LSP_MAX_OPEN_DOCUMENTS);
});

test("LSP retains bounded diagnostics only for opened workspace documents", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-diagnostics-"));
  const manager = new LspManager(lspConfig(
    [process.execPath, lspFixture, "--diagnostics"],
    { diagnosticsWaitMs: 100 },
  ));
  t.after(async () => {
    await manager.stopAll();
    await rm(root, { recursive: true, force: true });
  });
  await writeFile(join(root, "sample.ts"), "export const value = 1;\n");

  const diagnostics = await manager.query({ action: "diagnostics", path: "sample.ts", root });
  assert.deepEqual(diagnostics, [{ message: "fixture diagnostic", severity: 2 }]);
  const [status] = manager.status().running;
  assert.equal(status.diagnosticDocuments, 1);
  assert.equal(status.diagnosticEntries, 1);
  assert.ok(status.diagnosticBytes > 0);
});

test("LSP terminates servers that exceed diagnostic entry or text limits", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-diagnostic-bounds-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(join(root, "sample.ts"), "export const value = 1;\n");

  for (const [argument, expected] of [
    [`--diagnostic-count=${LSP_MAX_DIAGNOSTIC_ENTRIES + 1}`, /diagnostics exceed .* entries/],
    [`--diagnostic-message-bytes=${LSP_MAX_DIAGNOSTIC_BYTES + 1}`, /diagnostics exceed .* bytes/],
  ]) {
    const manager = new LspManager(lspConfig(
      [process.execPath, lspFixture, argument],
      { diagnosticsWaitMs: 100 },
    ));
    assert.deepEqual(await manager.query({ action: "diagnostics", path: "sample.ts", root }), []);
    const status = manager.status();
    assert.equal(status.running.length, 0);
    assert.match(status.lastFailure, expected);
    await manager.stopAll();
  }
});

test("LSP shutdown uses a short timeout independent of request timeout", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-shutdown-"));
  const manager = new LspManager(lspConfig(
    [process.execPath, lspFixture, "--ignore-shutdown"],
    { requestTimeoutMs: 120_000 },
  ));
  t.after(async () => {
    await manager.stopAll();
    await rm(root, { recursive: true, force: true });
  });
  await writeFile(join(root, "sample.ts"), "export const value = 1;\n");
  await manager.query({ action: "hover", path: "sample.ts", root });

  const startedAt = Date.now();
  await manager.stopAll();
  const elapsedMs = Date.now() - startedAt;
  assert.ok(elapsedMs < LSP_SHUTDOWN_TIMEOUT_MS + 2000, `shutdown took ${elapsedMs} ms`);
  assert.equal(manager.status().running.length, 0);
});

test("LSP rejects oversized headers, messages, and source files", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-bounds-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(join(root, "sample.ts"), "export const value = 1;\n");

  const headerManager = new LspManager(lspConfig([
    process.execPath,
    "-e",
    `process.stdout.write("x".repeat(${LSP_MAX_HEADER_BYTES + 1})); setTimeout(() => {}, 2000)`,
  ]));
  await assert.rejects(
    headerManager.query({ action: "hover", path: "sample.ts", root }),
    /header exceeds/,
  );
  await headerManager.stopAll();

  const messageManager = new LspManager(lspConfig([
    process.execPath,
    "-e",
    `process.stdout.write("Content-Length: ${LSP_MAX_MESSAGE_BYTES + 1}\\r\\n\\r\\n"); setTimeout(() => {}, 2000)`,
  ]));
  await assert.rejects(
    messageManager.query({ action: "hover", path: "sample.ts", root }),
    /message exceeds/,
  );
  await messageManager.stopAll();

  const documentManager = new LspManager(lspConfig([process.execPath, lspFixture]));
  await writeFile(join(root, "large.ts"), "x".repeat(LSP_MAX_DOCUMENT_BYTES + 1));
  await assert.rejects(
    documentManager.query({ action: "hover", path: "large.ts", root }),
    /document exceeds/,
  );
  await documentManager.stopAll();
});

test("LSP stdin EPIPE rejects the request without an unhandled error", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-lsp-epipe-"));
  const manager = new LspManager(lspConfig([
    process.execPath,
    "-e",
    "require('node:fs').closeSync(0); setTimeout(() => process.exit(0), 100)",
  ]));
  t.after(async () => {
    await manager.stopAll();
    await rm(root, { recursive: true, force: true });
  });
  await writeFile(join(root, "sample.ts"), "export const value = 1;\n");
  await assert.rejects(
    manager.query({ action: "hover", path: "sample.ts", root }),
    /stdin failed|exited/,
  );
});
