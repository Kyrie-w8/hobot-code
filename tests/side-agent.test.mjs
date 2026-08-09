import assert from "node:assert/strict";
import { mkdir, mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { acquireSideAgentLease } from "../extensions/rdk/side-agent-lease.mjs";
import {
  applySideAgentEvent,
  buildSideAgentArgs,
  buildSideSessionSnapshot,
  createSideAgentEventState,
  parseSideAgentLimit,
  parseSideAgentEvent,
} from "../extensions/rdk/side-agent-session.mjs";

test("side session snapshot includes in-memory branch entries without retaining the parent id", () => {
  const snapshot = buildSideSessionSnapshot({
    header: { type: "session", version: 3, id: "parent", timestamp: "old", cwd: "/old" },
    entries: [
      {
        type: "message",
        id: "user-1",
        parentId: null,
        timestamp: "now",
        message: { role: "user", content: [{ type: "text", text: "current request" }], timestamp: 1 },
      },
    ],
    id: "side-1",
    timestamp: "2026-08-09T00:00:00.000Z",
    cwd: "/workspace",
    parentSession: "/state/main.jsonl",
  });
  const lines = snapshot.trim().split("\n").map(JSON.parse);
  assert.equal(lines[0].id, "side-1");
  assert.equal(lines[0].cwd, "/workspace");
  assert.equal(lines[0].parentSession, "/state/main.jsonl");
  assert.equal(lines[1].message.content[0].text, "current request");
});

test("multi-turn side agent invocation uses RPC and inherits model, thinking, tools, and project trust", () => {
  const args = buildSideAgentArgs({
    sessionPath: "/tmp/session.jsonl",
    sessionDir: "/tmp/side",
    systemPromptPath: "/tmp/system.md",
    model: { provider: "drobotics", id: "kimi-k3" },
    thinkingLevel: "max",
    tools: ["read", "bash", "system_snapshot"],
    projectTrusted: true,
  });
  assert.deepEqual(args.slice(0, 2), ["--mode", "rpc"]);
  assert.ok(!args.includes("--print"));
  assert.ok(args.includes("--approve"));
  assert.ok(args.includes("drobotics/kimi-k3"));
  assert.ok(args.includes("max"));
  assert.ok(args.includes("read,bash,system_snapshot"));
});

test("side agent with no active tools remains tool-free", () => {
  const args = buildSideAgentArgs({
    sessionPath: "/tmp/session.jsonl",
    sessionDir: "/tmp/side",
    systemPromptPath: "/tmp/system.md",
    tools: [],
    projectTrusted: false,
  });
  assert.ok(args.includes("--no-tools"));
  assert.ok(args.includes("--no-approve"));
});

test("side agent event reducer tracks streamed text, tool state, and usage", () => {
  let state = createSideAgentEventState();
  state = applySideAgentEvent(state, {
    type: "message_update",
    assistantMessageEvent: { type: "thinking_delta", delta: "Checking context" },
  });
  state = applySideAgentEvent(state, {
    type: "message_update",
    assistantMessageEvent: { type: "text_delta", delta: "Inspecting" },
  });
  state = applySideAgentEvent(state, {
    type: "tool_execution_start",
    toolCallId: "tool-1",
    toolName: "bash",
    args: { command: "curl -H 'Authorization: Bearer secret' example.test" },
  }, () => "curl [REDACTED]");
  state = applySideAgentEvent(state, {
    type: "tool_execution_end",
    toolCallId: "tool-1",
    toolName: "bash",
    isError: false,
  });
  state = applySideAgentEvent(state, {
    type: "message_end",
    message: {
      role: "assistant",
      content: [
        { type: "thinking", thinking: "Checking context" },
        { type: "text", text: "Done" },
      ],
      stopReason: "stop",
      usage: { input: 120, output: 30, cacheRead: 80, cacheWrite: 0 },
    },
  });
  assert.equal(state.finalText, "Done");
  assert.equal(state.finalThinking, "Checking context");
  assert.equal(state.thinkingText, "");
  assert.equal(state.thinkingChars, 16);
  assert.equal(state.streamingText, "");
  assert.equal(state.tools[0].status, "done");
  assert.equal(state.tools[0].target, "curl [REDACTED]");
  assert.equal(state.inputTokens, 120);
  assert.equal(state.cacheReadTokens, 80);
});

test("side agent event parser ignores non-JSON output", () => {
  assert.equal(parseSideAgentEvent("not json"), undefined);
  assert.deepEqual(parseSideAgentEvent('{"type":"agent_end"}'), { type: "agent_end" });
});

test("side agent board limit defaults to two and remains bounded", () => {
  assert.equal(parseSideAgentLimit(undefined), 2);
  assert.equal(parseSideAgentLimit("3"), 3);
  assert.equal(parseSideAgentLimit("0"), 1);
  assert.equal(parseSideAgentLimit("99"), 8);
  assert.equal(parseSideAgentLimit("invalid"), 2);
});

test("side agent leases reject excess agents and release capacity", async (t) => {
  const registryDir = await mkdtemp(join(tmpdir(), "hobot-side-agent-lease-test-"));
  t.after(() => rm(registryDir, { recursive: true, force: true }));
  const options = { registryDir, limitValue: "1", uid: "test", ownerIsAlive: () => true };
  const first = await acquireSideAgentLease({ ...options, pid: 101 });
  assert.equal(first.activeCount, 1);
  await assert.rejects(acquireSideAgentLease({ ...options, pid: 102 }), /already has 1 side agents/);
  await first.release();
  const replacement = await acquireSideAgentLease({ ...options, pid: 103 });
  assert.equal(replacement.activeCount, 1);
  await replacement.release();
});

test("side agent leases reclaim claims from dead processes", async (t) => {
  const registryDir = await mkdtemp(join(tmpdir(), "hobot-side-agent-stale-test-"));
  t.after(() => rm(registryDir, { recursive: true, force: true }));
  await mkdir(join(registryDir, "claim-404-stale"));
  const lease = await acquireSideAgentLease({
    registryDir,
    limitValue: "1",
    pid: 105,
    uid: "test",
    ownerIsAlive: (pid) => pid !== 404,
  });
  assert.equal(lease.activeCount, 1);
  assert.deepEqual((await readdir(registryDir)).filter((entry) => entry.includes("404")), []);
  await lease.release();
});
