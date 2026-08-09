import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  DEFAULT_POLICY,
  describeToolCall,
  fingerprintWorkspace,
  initializeProject,
  parsePolicy,
  parseQualityConfig,
  resolveToolAction,
  setPolicyRule,
} from "../extensions/rdk/control-plane.mjs";

const snapshot = {
  board: "D-Robotics RDK S600",
  boardId: "s600",
  rdkOsVersion: "5.1.0",
  architecture: "arm64",
};

test("permission rules cover built-in, RDK, MCP, and fallback tools", () => {
  assert.equal(resolveToolAction(DEFAULT_POLICY, "read"), "allow");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "system_snapshot"), "allow");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "quality_gate"), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "mcp__git__status", true), "ask");
  assert.equal(resolveToolAction(DEFAULT_POLICY, "unknown_plugin"), "ask");

  const denied = setPolicyRule(DEFAULT_POLICY, "mcp:*", "deny");
  assert.equal(resolveToolAction(denied, "mcp__git__status", true), "deny");
  assert.equal(resolveToolAction(denied, "read"), "allow");
});

test("invalid policies and quality configs fail closed", () => {
  assert.throws(() => parsePolicy({ schemaVersion: 1, default: "yes", rules: [] }), /default/);
  assert.throws(
    () => parseQualityConfig({ schemaVersion: 1, timeoutMs: 10, commands: ["make check"] }),
    /timeoutMs/,
  );
  assert.throws(
    () => parseQualityConfig({ schemaVersion: 1, timeoutMs: 1000, commands: ["bad\ncommand"] }),
    /invalid/,
  );
});

test("approval descriptions redact credentials", () => {
  const description = describeToolCall("bash", { command: "curl -H 'Authorization: Bearer secret-value' sk-private123" });
  assert.doesNotMatch(description, /secret-value|sk-private123/);
  assert.match(description, /REDACTED/);
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
