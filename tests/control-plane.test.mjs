import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { destructiveShellReasons, inspectResolvedPath, sanitizedChildEnv } from "../extensions/rdk/runtime-safety.mjs";

import {
  DEFAULT_POLICY,
  describeToolCall,
  fingerprintWorkspace,
  initializeProject,
  knowledgeQueryTerms,
  loadHookConfig,
  loadPolicy,
  memoryMatchQuery,
  parseGoalConfig,
  parseHookConfig,
  parseLspConfig,
  parseMemoryConfig,
  parseNotificationConfig,
  parsePolicy,
  parseQualityConfig,
  reconcileToolVisibility,
  requiresRootToolApproval,
  resolveToolAction,
  sensitiveMemoryReasons,
  setPolicyRule,
  validateMemoryInput,
} from "../extensions/rdk/control-plane.mjs";

const snapshot = {
  board: "D-Robotics RDK S600",
  boardId: "s600",
  rdkOsVersion: "5.1.0",
  architecture: "arm64",
};

test("permission rules cover built-in, RDK, MCP, and fallback tools", () => {
  assert.equal(resolveToolAction(DEFAULT_POLICY, "read"), "allow");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "write"), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "edit"), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "bash"), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "system_snapshot"), "allow");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "quality_gate"), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "mcp__git__status", true), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "unknown_plugin"), "ask");

  const denied = setPolicyRule(DEFAULT_POLICY, "mcp:*", "deny");
  assert.equal(resolveToolAction(denied, "mcp__git__status", true), "deny");
  assert.equal(resolveToolAction(denied, "read"), "allow");
});

test("root policy mode honors explicit tool rules without weakening hard guards", () => {
  const legacyShape = parsePolicy({
    schemaVersion: 2,
    default: "ask",
    rules: [{ tool: "bash", action: "allow" }],
  });
  assert.equal(legacyShape.rootMode, "confirm");
  assert.equal(requiresRootToolApproval(legacyShape, true, "bash"), true);

  const trusted = parsePolicy({ ...legacyShape, rootMode: "policy" });
  assert.equal(resolveToolAction(trusted, "bash"), "allow");
  assert.equal(requiresRootToolApproval(trusted, true, "bash"), false);
  assert.equal(requiresRootToolApproval(trusted, true, "write"), false);
  assert.equal(requiresRootToolApproval(trusted, true, "read"), false);
  assert.ok(destructiveShellReasons("rm -rf ./build").length > 0);
});

test("permission changes restore only tools hidden by the permission layer", () => {
  const initiallyRestricted = reconcileToolVisibility(
    ["read", "bash", "write"],
    ["read"],
    new Set(),
    ["bash"],
  );
  assert.deepEqual(initiallyRestricted.activeTools, ["read"]);
  assert.deepEqual([...initiallyRestricted.hiddenTools], []);

  const hiddenByPolicy = reconcileToolVisibility(
    ["read", "bash", "write"],
    ["read", "bash"],
    new Set(),
    ["bash"],
  );
  assert.deepEqual(hiddenByPolicy.activeTools, ["read"]);
  assert.deepEqual([...hiddenByPolicy.hiddenTools], ["bash"]);

  const restored = reconcileToolVisibility(
    ["read", "bash", "write"],
    hiddenByPolicy.activeTools,
    hiddenByPolicy.hiddenTools,
    [],
  );
  assert.deepEqual(restored.activeTools, ["read", "bash"]);
  assert.deepEqual([...restored.hiddenTools], []);
});

test("child processes do not inherit credentials or runtime injection variables", () => {
  const env = sanitizedChildEnv({
    PATH: "/usr/bin",
    LANG: "C.UTF-8",
    ANTHROPIC_AUTH_TOKEN: "secret",
    OPENAI_API_KEY: "secret",
    NODE_OPTIONS: "--require=/tmp/inject.js",
    NODE_PATH: "/tmp/node-inject",
    LD_PRELOAD: "/tmp/inject.so",
    LD_LIBRARY_PATH: "/tmp/linker-inject",
    PYTHONPATH: "/tmp/python-inject",
    RUBYLIB: "/tmp/ruby-inject",
  });
  assert.deepEqual(env, { PATH: "/usr/bin", LANG: "C.UTF-8" });
});

test("resolved path checks reject symlink escapes and destructive commands", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-path-test-"));
  try {
    await symlink("/etc", join(root, "system"));
    const inspected = await inspectResolvedPath(root, "system/hosts");
    assert.equal(inspected.withinWorkspace, false);
    assert.equal(inspected.criticalRoot, "/etc");
    await symlink("/etc/hobot-does-not-exist", join(root, "broken-system"));
    const broken = await inspectResolvedPath(root, "broken-system");
    assert.equal(broken.withinWorkspace, false);
    assert.equal(broken.criticalRoot, "/etc");
    assert.ok(destructiveShellReasons("busybox rm -rf ./cache").length > 0);
    assert.ok(destructiveShellReasons("find . -type f -delete").length > 0);
    assert.ok(destructiveShellReasons("tee /etc/systemd/system/demo.service").length > 0);
    const readOnlyStatus = 'cd /root/ssd/yolo_bench && tail -5 progress.log 2>/dev/null; echo ===; wc -l results.csv 2>/dev/null; ps aux | grep -E "run_bench|hrt_model" | grep -v grep | head -3';
    assert.deepEqual(destructiveShellReasons(readOnlyStatus), []);
    assert.deepEqual(destructiveShellReasons("tail progress.log >/dev/null"), []);
    assert.deepEqual(destructiveShellReasons("tail progress.log 2>>'/dev/null'"), []);
    assert.ok(destructiveShellReasons("tail progress.log | tee /dev/null /etc/output.log").length > 0);
    assert.ok(destructiveShellReasons("echo x >/dev/sda").length > 0);
    assert.ok(destructiveShellReasons("echo x >/dev/null/child").length > 0);
    assert.ok(destructiveShellReasons("echo x >/dev/null-device").length > 0);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("resolved path checks fail closed on symbolic link cycles", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-path-cycle-"));
  try {
    await symlink("cycle-b", join(root, "cycle-a"));
    await symlink("cycle-a", join(root, "cycle-b"));
    await assert.rejects(
      () => inspectResolvedPath(root, "cycle-a/file.txt"),
      (error) => error?.code === "ELOOP",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("invalid policies and quality configs fail closed", () => {
  assert.throws(() => parsePolicy({ schemaVersion: 1, default: "yes", rules: [] }), /default/);
  assert.throws(
    () => parsePolicy({ schemaVersion: 2, rootMode: "unrestricted", default: "ask", rules: [] }),
    /rootMode/,
  );
  assert.throws(
    () => parseQualityConfig({ schemaVersion: 1, timeoutMs: 10, commands: ["make check"] }),
    /timeoutMs/,
  );
  assert.throws(
    () => parseQualityConfig({ schemaVersion: 1, timeoutMs: 1000, commands: ["bad\ncommand"] }),
    /invalid/,
  );
});

test("legacy and invalid permission policies cannot retain silent mutation access", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-policy-test-"));
  const path = join(root, "permissions.json");
  try {
    await writeFile(path, JSON.stringify({
      schemaVersion: 1,
      default: "allow",
      rules: [{ tool: "*", action: "allow" }],
    }));
    const migrated = await loadPolicy(path);
    assert.equal(migrated.migrated, true);
    assert.equal(resolveToolAction(migrated.policy, "bash"), "ask");
    assert.equal(resolveToolAction(migrated.policy, "write"), "ask");
    assert.equal(migrated.policy.rootMode, "confirm");
    assert.equal(JSON.parse(await readFile(path, "utf8")).schemaVersion, 2);

    await writeFile(path, "not json");
    const fallback = await loadPolicy(path);
    assert.equal(resolveToolAction(fallback.policy, "bash"), "ask");
    assert.ok(fallback.error);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("RDK knowledge queries segment natural Chinese text and board identifiers", () => {
  const terms = knowledgeQueryTerms("在 S600 上怎么部署模型？");
  assert.ok(terms.includes("s600"));
  assert.ok(terms.includes("部署"));
  assert.ok(terms.includes("模型"));
});

test("approval descriptions redact credentials", () => {
  const description = describeToolCall("bash", { command: "curl -H 'Authorization: Bearer secret-value' sk-private123" });
  assert.doesNotMatch(description, /secret-value|sk-private123/);
  assert.match(description, /REDACTED/);
  assert.doesNotMatch(
    describeToolCall("memory_save", {
      scope: "project",
      kind: "fact",
      content: "-----BEGIN PRIVATE KEY-----\nvery-secret-material\n-----END PRIVATE KEY-----",
    }),
    /very-secret-material/,
  );
  assert.doesNotMatch(describeToolCall("bash", { command: "curl https://user:password@example.com ghp_abcdefghijklmnopqrstuvwxyz1234" }), /password|ghp_/);
});

test("project initialization creates defaults and never overwrites AGENTS.md", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-init-test-"));
  try {
    await writeFile(join(root, "Makefile"), "check:\n\t@true\n");
    const first = await initializeProject(root, snapshot);
    assert.equal(first.created.length, 2);
    assert.deepEqual(first.commands, ["make check"]);
    const original = await readFile(join(root, "AGENTS.md"), "utf8");
    assert.match(original, /RDK S600/);
    assert.match(original, /make check/);

    await writeFile(join(root, "AGENTS.md"), "user-owned\n");
    const second = await initializeProject(root, snapshot);
    assert.equal(second.created.length, 0);
    assert.equal(second.preserved.length, 2);
    assert.equal(await readFile(join(root, "AGENTS.md"), "utf8"), "user-owned\n");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("workspace fingerprint changes after a source edit", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-fingerprint-test-"));
  try {
    await writeFile(join(root, "source.txt"), "one\n");
    const before = await fingerprintWorkspace(root);
    await writeFile(join(root, "source.txt"), "two\n");
    const after = await fingerprintWorkspace(root);
    assert.notEqual(before.digest, after.digest);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("workspace fingerprint ignores generated trees and rejects oversized source files", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-fingerprint-limits-"));
  try {
    await mkdir(join(root, "dist"));
    await writeFile(join(root, "dist", "generated.js"), "one\n");
    const before = await fingerprintWorkspace(root);
    await writeFile(join(root, "dist", "generated.js"), "two\n");
    const after = await fingerprintWorkspace(root);
    assert.equal(before.digest, after.digest);

    await writeFile(join(root, "oversized.ts"), Buffer.alloc(8 * 1024 * 1024 + 1));
    await assert.rejects(() => fingerprintWorkspace(root), /source file exceeds/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("memory config and scope validation are bounded", () => {
  assert.deepEqual(parseMemoryConfig({
    schemaVersion: 1,
    enabled: true,
    autoRecall: true,
    maxInjected: 6,
    maxSearchResults: 10,
    maxContentChars: 4000,
    defaultExpiresDays: null,
  }).maxInjected, 6);
  assert.equal(validateMemoryInput("project", "decision", "Use make check").content, "Use make check");
  assert.throws(() => validateMemoryInput("global", "fact", "value"), /scope/);
});

test("memory rejects secrets and builds a bounded FTS query", () => {
  assert.deepEqual(sensitiveMemoryReasons("ANTHROPIC_AUTH_TOKEN=secret-value"), ["secret assignment"]);
  assert.deepEqual(sensitiveMemoryReasons("ghp_abcdefghijklmnopqrstuvwxyz1234"), ["GitHub token"]);
  assert.deepEqual(sensitiveMemoryReasons("https://user:password@example.com/private"), ["URL credential"]);
  assert.throws(
    () => validateMemoryInput("user", "preference", "Use token sk-private123"),
    /sensitive data/,
  );
  assert.equal(memoryMatchQuery("S600 部署 S600"), '"S600" OR "部署"');
});

test("persistent goal and notification configs enforce budgets and bounded timing", () => {
  assert.equal(parseGoalConfig({
    schemaVersion: 1,
    enabled: true,
    defaultTurnBudget: 50,
    defaultTokenBudget: null,
  }).defaultTurnBudget, 50);
  assert.throws(() => parseGoalConfig({
    schemaVersion: 1,
    enabled: true,
    defaultTurnBudget: 0,
    defaultTokenBudget: null,
  }), /defaultTurnBudget/);
  assert.equal(parseNotificationConfig({
    schemaVersion: 1,
    enabled: true,
    allowLocal: false,
    bell: true,
    protocol: "osc9",
    onApproval: true,
    onComplete: true,
    onFailure: true,
    minDurationMs: 5000,
  }).protocol, "osc9");
});

test("hook config uses structured commands and explicit failure policy", () => {
  const config = parseHookConfig({
    schemaVersion: 1,
    enabled: true,
    failurePolicy: "block",
    timeoutMs: 1000,
    maxOutputChars: 1000,
    allowProjectHooks: false,
    hooks: [{ name: "guard", event: "PreToolUse", tool: "bash", command: ["/usr/local/bin/guard"] }],
  });
  assert.deepEqual(config.hooks[0].command, ["/usr/local/bin/guard"]);
  assert.throws(() => parseHookConfig({ ...config, hooks: [{ ...config.hooks[0], command: "guard" }] }), /string array/);
});

test("project hooks remain disabled until the current project is trusted", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-hook-trust-"));
  const globalPath = join(root, "hooks.json");
  const projectPath = join(root, "project-hooks.json");
  const base = {
    schemaVersion: 1,
    enabled: true,
    failurePolicy: "block",
    timeoutMs: 5000,
    maxOutputChars: 4000,
    allowProjectHooks: true,
    hooks: [],
  };
  try {
    await writeFile(globalPath, JSON.stringify(base));
    await writeFile(projectPath, JSON.stringify({
      ...base,
      allowProjectHooks: false,
      hooks: [{ name: "project", event: "PreToolUse", tool: "bash", command: ["/bin/true"] }],
    }));
    const untrusted = await loadHookConfig(globalPath, projectPath, false);
    assert.equal(untrusted.config.hooks.length, 0);
    assert.match(untrusted.error, /trusted/);
    const trusted = await loadHookConfig(globalPath, projectPath, true);
    assert.equal(trusted.config.hooks.length, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("LSP config limits processes and rejects malformed extensions", () => {
  const base = {
    schemaVersion: 1,
    enabled: true,
    maxProcesses: 1,
    maxMemoryMiB: 256,
    idleTimeoutMs: 60000,
    requestTimeoutMs: 10000,
    diagnosticsWaitMs: 500,
    servers: [{ id: "clangd", extensions: [".cpp"], languageId: "cpp", command: ["clangd"] }],
  };
  assert.equal(parseLspConfig(base).maxProcesses, 1);
  assert.throws(() => parseLspConfig({ ...base, maxProcesses: 8 }), /maxProcesses/);
  assert.throws(() => parseLspConfig({ ...base, servers: [{ ...base.servers[0], extensions: ["cpp"] }] }), /extensions/);
});
