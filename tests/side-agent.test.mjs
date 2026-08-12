import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { acquireSideAgentLease } from "../extensions/rdk/side-agent-lease.mjs";
import {
  applySideAgentEvent,
  buildSideAgentArgs,
  buildSideSessionSnapshot,
  createSideAgentEventState,
  enqueueSideAgentUiRequest,
  notifySideAgentListeners,
  parseSideAgentLimit,
  parseSideAgentEvent,
  removeSideAgentUiRequest,
  resolveSideAgentUiTimeout,
  selectSideAgentParentEntries,
  sideAgentCommandResponseMatches,
  sideAgentLeafBeforeRun,
  sideAgentPanelLayout,
  sideAgentPhaseAfterEvent,
  sideAgentPointerFocusTarget,
} from "../extensions/rdk/side-agent-session.mjs";

test("side session snapshot serializes the captured branch exactly", () => {
  const snapshot = buildSideSessionSnapshot({
    header: { type: "session", version: 3, id: "parent", timestamp: "old", cwd: "/old" },
    entries: [
      { type: "model_change", id: "model-1", parentId: null, timestamp: "now", provider: "test", modelId: "model" },
      {
        type: "message",
        id: "user-1",
        parentId: "model-1",
        timestamp: "now",
        message: { role: "user", content: [{ type: "text", text: "settled request" }], timestamp: 1 },
      },
      {
        type: "message",
        id: "assistant-1",
        parentId: "user-1",
        timestamp: "now",
        message: { role: "assistant", content: [{ type: "text", text: "settled answer" }], stopReason: "stop" },
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
  assert.deepEqual(lines.slice(1).map((entry) => entry.id), ["model-1", "user-1", "assistant-1"]);
});

test("side agent forks from the settled branch while the parent is running", () => {
  const settledEntries = [{ id: "assistant-1", type: "message" }];
  const currentEntries = [...settledEntries, { id: "user-2", type: "message" }];
  assert.equal(selectSideAgentParentEntries({
    currentEntries,
    settledEntries,
    parentRunActive: true,
    runtimeIdle: false,
  }), settledEntries);
  assert.equal(selectSideAgentParentEntries({
    currentEntries,
    settledEntries,
    parentRunActive: false,
    runtimeIdle: false,
  }), settledEntries);
});

test("side agent rejects an incomplete parent tool turn even when runtime state reports idle", () => {
  const currentEntries = [
    { id: "model-1", parentId: null, type: "model_change" },
    { id: "user-1", parentId: "model-1", type: "message", message: { role: "user" } },
    {
      id: "assistant-1",
      parentId: "user-1",
      type: "message",
      message: { role: "assistant", stopReason: "toolUse" },
    },
  ];
  assert.deepEqual(selectSideAgentParentEntries({
    currentEntries,
    settledEntries: currentEntries,
    parentRunActive: false,
    runtimeIdle: true,
  }), [currentEntries[0]]);
});

test("side agent keeps the full current branch while the parent is idle", () => {
  const settledEntries = [{ id: "assistant-1", type: "message" }];
  const currentEntries = [...settledEntries, { id: "custom-1", type: "custom_message" }];
  assert.equal(selectSideAgentParentEntries({
    currentEntries,
    settledEntries,
    parentRunActive: false,
    runtimeIdle: true,
  }), currentEntries);
});

test("side agent captures the leaf before a newly started user or custom turn", () => {
  const settled = { id: "assistant-1", parentId: "user-1", type: "message", message: { role: "assistant" } };
  assert.equal(sideAgentLeafBeforeRun([settled]), "assistant-1");
  assert.equal(sideAgentLeafBeforeRun([
    settled,
    { id: "user-2", parentId: "assistant-1", type: "message", message: { role: "user" } },
  ]), "assistant-1");
  assert.equal(sideAgentLeafBeforeRun([
    settled,
    { id: "custom-1", parentId: "assistant-1", type: "custom_message" },
  ]), "assistant-1");

  assert.equal(sideAgentLeafBeforeRun([
    settled,
    { id: "user-2", parentId: "assistant-1", type: "message", message: { role: "user" } },
    {
      id: "assistant-2",
      parentId: "user-2",
      type: "message",
      message: { role: "assistant", stopReason: "toolUse" },
    },
  ]), "assistant-1");

  assert.equal(sideAgentLeafBeforeRun([
    { id: "model-1", parentId: null, type: "model_change" },
    { id: "user-1", parentId: "model-1", type: "message", message: { role: "user" } },
    {
      id: "assistant-1",
      parentId: "user-1",
      type: "message",
      message: { role: "assistant", stopReason: "toolUse" },
    },
    { id: "tool-1", parentId: "assistant-1", type: "message", message: { role: "toolResult" } },
    { id: "user-2", parentId: "tool-1", type: "message", message: { role: "user" } },
  ]), "model-1");
});

test("side agent listener failures cannot block lifecycle listeners", () => {
  const calls = [];
  const broken = () => {
    calls.push("broken");
    throw new Error("render failed");
  };
  const healthy = () => calls.push("healthy");
  const listeners = new Set([broken, healthy]);
  notifySideAgentListeners(listeners);
  assert.deepEqual(calls, ["broken", "healthy"]);
  assert.equal(listeners.has(broken), false);
  assert.equal(listeners.has(healthy), true);
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

test("side agent usage ignores invalid and negative counters", () => {
  const state = applySideAgentEvent(createSideAgentEventState(), {
    type: "message_end",
    message: {
      role: "assistant",
      content: [],
      usage: { input: "invalid", output: Number.POSITIVE_INFINITY, cacheRead: -1, cacheWrite: 2.4 },
    },
  });
  assert.equal(state.inputTokens, 0);
  assert.equal(state.outputTokens, 0);
  assert.equal(state.cacheReadTokens, 0);
  assert.equal(state.cacheWriteTokens, 2);
});

test("side agent event parser ignores non-JSON output", () => {
  assert.equal(parseSideAgentEvent("not json"), undefined);
  assert.deepEqual(parseSideAgentEvent('{"type":"agent_end"}'), { type: "agent_end" });
});

test("side agent stays busy until the RPC agent_settled barrier", () => {
  let phase = "running";
  phase = sideAgentPhaseAfterEvent(phase, { type: "agent_end", willRetry: true });
  assert.equal(phase, "running");
  phase = sideAgentPhaseAfterEvent(phase, { type: "auto_retry_start" });
  assert.equal(phase, "running");
  phase = sideAgentPhaseAfterEvent(phase, { type: "agent_start" });
  assert.equal(phase, "running");
  phase = sideAgentPhaseAfterEvent(phase, { type: "agent_end", willRetry: false });
  assert.equal(phase, "running");
  phase = sideAgentPhaseAfterEvent(phase, { type: "agent_settled" });
  assert.equal(phase, "idle");
});

test("side agent command failures only match the active RPC request", () => {
  const event = { type: "response", id: "btw_2", command: "prompt", success: false };
  assert.equal(sideAgentCommandResponseMatches(event, "btw_1", "prompt"), false);
  assert.equal(sideAgentCommandResponseMatches(event, "btw_2", "abort"), false);
  assert.equal(sideAgentCommandResponseMatches(event, "btw_2", "prompt"), true);
});

test("side agent UI requests remain FIFO and do not overwrite concurrent approvals", () => {
  const first = { id: "approval-1", method: "confirm" };
  const second = { id: "approval-2", method: "confirm" };
  let queue = enqueueSideAgentUiRequest([], first);
  queue = enqueueSideAgentUiRequest(queue, second);
  queue = enqueueSideAgentUiRequest(queue, first);
  assert.deepEqual(queue.map((request) => request.id), ["approval-1", "approval-2"]);
  queue = removeSideAgentUiRequest(queue, "approval-1");
  assert.equal(queue[0].id, "approval-2");
});

test("side agent UI timeout policy bounds missing and oversized dialogs", () => {
  assert.deepEqual(resolveSideAgentUiTimeout(5_000, 120_000), {
    timeout: 5_000,
    rpcOwnsTimeout: true,
  });
  assert.deepEqual(resolveSideAgentUiTimeout(undefined, 120_000), {
    timeout: 120_000,
    rpcOwnsTimeout: false,
  });
  assert.deepEqual(resolveSideAgentUiTimeout(300_000, 120_000), {
    timeout: 120_000,
    rpcOwnsTimeout: false,
  });
});

test("side agent per-user limit defaults to two and remains bounded", () => {
  assert.equal(parseSideAgentLimit(undefined), 2);
  assert.equal(parseSideAgentLimit("3"), 3);
  assert.equal(parseSideAgentLimit("0"), 1);
  assert.equal(parseSideAgentLimit("99"), 8);
  assert.equal(parseSideAgentLimit("invalid"), 2);
  assert.equal(parseSideAgentLimit("3agents"), 2);
  assert.equal(parseSideAgentLimit(" 3 "), 3);
});

test("side agent panel layout stays within narrow terminal bounds", () => {
  assert.deepEqual(sideAgentPanelLayout(80, 24), {
    panelWidth: 80,
    panelRows: 24,
    compact: false,
    innerWidth: 78,
    contentRows: 19,
  });
  const narrow = sideAgentPanelLayout(20, 7);
  assert.equal(narrow.innerWidth + 2, narrow.panelWidth);
  assert.equal(narrow.contentRows + 5, narrow.panelRows);
  assert.equal(narrow.compact, false);
  assert.deepEqual(sideAgentPanelLayout(2, 4), {
    panelWidth: 2,
    panelRows: 4,
    compact: true,
    innerWidth: 2,
    contentRows: 0,
  });
});

test("side agent keeps main input active and exposes pointer-routed scrolling", async () => {
  const source = await readFile(new URL("../extensions/rdk/side-agent.ts", import.meta.url), "utf8");
  const settings = JSON.parse(await readFile(new URL("../packaging/pi/settings.json", import.meta.url), "utf8"));
  assert.equal(settings.tuiMode, "fullscreen", "new installs must enable pointer-routed split panes by default");
  assert.match(source, /new HStack\(\[/, "fullscreen /btw must be mounted beside the main layout");
  assert.match(source, /basis:\s*mainWidth[\s\S]*basis:\s*sideWidth/, "the initial split must be exactly equal");
  assert.match(source, /new SideAgentCustomHost\(pane\)/, "the hidden custom host must not share side-pane focus identity");
  assert.match(source, /new ScrollView\(/, "the side transcript must participate in TUI wheel routing");
  assert.match(source, /nonCapturing:\s*true/, "the compatibility overlay must not steal main input focus");
  assert.match(source, /registerShortcut\("ctrl\+shift\+right"/, "main-to-side focus switching must remain available");
  assert.match(source, /matchesKey\(data, "ctrl\+shift\+left"\)/, "side-to-main focus switching must remain available");
  assert.match(source, /prependTuiInputListener\(viewport/, "pointer focus must run before Pi consumes mouse input");
  assert.match(source, /Reflect\.ownKeys\(tui\)/, "bundled runtimes may minify Pi's private listener field");
  assert.match(source, /value instanceof Set && value\.has\(listener\)/, "listener discovery must identify its owning set by identity");
  assert.doesNotMatch(source, /onHandle:\s*\(handle\)\s*=>\s*handle\.focus\(\)/);
});

test("primary pointer presses focus the clicked half without intercepting other mouse events", () => {
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;1;1M", 120), "main");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;60;20M", 120), "main");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;61;20M", 120), "side");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<4;62;20M", 121), "side", "modifier bits remain valid");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;61;20M", 121), "main", "odd widths favor main");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;62;20M", 121), "side");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;61;20m", 120), undefined, "release does not refocus");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<32;61;20M", 120), undefined, "drag does not refocus");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<64;61;20M", 120), undefined, "wheel does not refocus");
  assert.equal(sideAgentPointerFocusTarget("\x1b[<0;121;20M", 120), undefined, "stale coordinates are ignored");
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
  assert.equal((await readdir(registryDir)).includes("claim-404-stale"), false);
  await lease.release();
});
