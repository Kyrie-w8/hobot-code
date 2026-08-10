const SESSION_VERSION = 3;
const MAX_STREAM_TEXT_CHARS = 200_000;
const MAX_TOOL_EVENTS = 100;

export function selectSideAgentParentEntries({ currentEntries, settledEntries, parentRunActive, runtimeIdle }) {
  if (!Array.isArray(currentEntries) || !Array.isArray(settledEntries)) {
    throw new Error("side session branches must be arrays");
  }
  const leaf = currentEntries.at(-1);
  const leafRole = leaf?.type === "message" ? leaf.message?.role : undefined;
  const structurallyInFlight = leafRole === "user"
    || leafRole === "toolResult"
    || (leafRole === "assistant" && leaf.message?.stopReason === "toolUse");
  if (structurallyInFlight) {
    const safeLeafId = sideAgentLeafBeforeRun(currentEntries);
    if (!safeLeafId) return [];
    const safeLeafIndex = currentEntries.findIndex((entry) => entry?.id === safeLeafId);
    return safeLeafIndex >= 0 ? currentEntries.slice(0, safeLeafIndex + 1) : [];
  }
  return parentRunActive || !runtimeIdle ? settledEntries : currentEntries;
}

export function sideAgentLeafBeforeRun(entries) {
  if (!Array.isArray(entries)) throw new Error("side session branch must be an array");
  const leaf = entries.at(-1);
  if (!leaf || typeof leaf !== "object") return undefined;

  const leafIsUnfinished = leaf.type === "custom_message"
    || (leaf.type === "message" && (
      leaf.message?.role === "user"
      || (leaf.message?.role === "assistant" && leaf.message?.stopReason === "toolUse")
      || leaf.message?.role === "toolResult"
    ));
  if (!leafIsUnfinished) return typeof leaf.id === "string" ? leaf.id : undefined;

  let boundary;
  for (let index = entries.length - 1; index >= 0; index -= 1) {
    const entry = entries[index];
    if (!entry || typeof entry !== "object") continue;
    const isRunPrompt = entry.type === "custom_message"
      || (entry.type === "message" && entry.message?.role === "user");
    if (isRunPrompt) boundary = entry.parentId;

    const isSettledAssistant = entry.type === "message"
      && entry.message?.role === "assistant"
      && entry.message?.stopReason !== "toolUse";
    if (isSettledAssistant) break;
  }
  return typeof boundary === "string" ? boundary : undefined;
}

export function notifySideAgentListeners(listeners) {
  for (const listener of [...listeners]) {
    try {
      listener();
    } catch {
      listeners.delete?.(listener);
    }
  }
}

export function resolveSideAgentUiTimeout(requestedTimeout, maximumTimeout) {
  const requested = Number(requestedTimeout);
  const maximum = Number(maximumTimeout);
  if (!Number.isFinite(maximum) || maximum <= 0) throw new Error("side UI timeout maximum must be positive");
  if (Number.isFinite(requested) && requested > 0 && requested <= maximum) {
    return { timeout: requested, rpcOwnsTimeout: true };
  }
  return { timeout: maximum, rpcOwnsTimeout: false };
}

export function sideAgentPhaseAfterEvent(phase, event) {
  if (event?.type === "agent_start") return "running";
  if (event?.type === "agent_settled") return "idle";
  return phase;
}

export function sideAgentCommandResponseMatches(event, id, command) {
  return Boolean(
    id
    && event?.type === "response"
    && event.id === id
    && event.command === command,
  );
}

export function enqueueSideAgentUiRequest(requests, request) {
  if (requests.some((candidate) => candidate.id === request.id)) return requests;
  return [...requests, request];
}

export function removeSideAgentUiRequest(requests, id) {
  return requests.filter((request) => request.id !== id);
}

export function parseSideAgentLimit(value, fallback = 2) {
  const normalized = String(value ?? "").trim();
  const parsed = /^\d+$/.test(normalized) ? Number(normalized) : Number.NaN;
  const boundedFallback = Math.min(8, Math.max(1, Number.isInteger(fallback) ? fallback : 2));
  return Number.isSafeInteger(parsed) ? Math.min(8, Math.max(1, parsed)) : boundedFallback;
}

export function sideAgentPanelLayout(width, rows) {
  const panelWidth = Number.isFinite(width) ? Math.max(1, Math.floor(width)) : 1;
  const panelRows = Number.isFinite(rows) ? Math.max(1, Math.floor(rows)) : 1;
  const compact = panelWidth < 4 || panelRows < 5;
  return {
    panelWidth,
    panelRows,
    compact,
    innerWidth: compact ? panelWidth : panelWidth - 2,
    contentRows: compact ? 0 : panelRows - 5,
  };
}

function boundedTail(value, limit = MAX_STREAM_TEXT_CHARS) {
  if (value.length <= limit) return value;
  return `[Earlier output omitted]\n${value.slice(-limit)}`;
}

function boundedTokenCount(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return 0;
  return Math.min(Number.MAX_SAFE_INTEGER, Math.round(parsed));
}

export function buildSideSessionSnapshot({ header, entries, id, timestamp, cwd, parentSession }) {
  if (!id || !timestamp || !cwd) throw new Error("side session metadata is incomplete");
  if (!Array.isArray(entries)) throw new Error("side session entries must be an array");

  const snapshotHeader = {
    ...(header?.type === "session" ? header : {}),
    type: "session",
    version: Number.isInteger(header?.version) ? header.version : SESSION_VERSION,
    id,
    timestamp,
    cwd,
    ...(parentSession ? { parentSession } : {}),
  };
  return `${[snapshotHeader, ...entries].map((entry) => JSON.stringify(entry)).join("\n")}\n`;
}

export function buildSideAgentArgs({
  sessionPath,
  sessionDir,
  systemPromptPath,
  model,
  thinkingLevel,
  tools,
  projectTrusted,
}) {
  const args = [
    "--mode",
    "rpc",
    "--session",
    sessionPath,
    "--session-dir",
    sessionDir,
    "--system-prompt",
    systemPromptPath,
    "--no-context-files",
    "--no-skills",
    projectTrusted ? "--approve" : "--no-approve",
  ];
  if (model?.provider && model?.id) args.push("--model", `${model.provider}/${model.id}`);
  if (thinkingLevel) args.push("--thinking", thinkingLevel);
  if (Array.isArray(tools) && tools.length > 0) args.push("--tools", tools.join(","));
  else args.push("--no-tools");
  return args;
}

export function createSideAgentEventState() {
  return {
    streamingText: "",
    finalText: "",
    thinkingText: "",
    finalThinking: "",
    thinkingChars: 0,
    tools: [],
    turns: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    stopReason: undefined,
    errorMessage: undefined,
  };
}

function assistantText(message) {
  if (message?.role !== "assistant" || !Array.isArray(message.content)) return "";
  return message.content
    .filter((part) => part?.type === "text" && typeof part.text === "string")
    .map((part) => part.text)
    .join("\n");
}

function assistantThinking(message) {
  if (message?.role !== "assistant" || !Array.isArray(message.content)) return "";
  return message.content
    .filter((part) => part?.type === "thinking" && typeof part.thinking === "string")
    .map((part) => part.thinking)
    .join("\n");
}

function toolTarget(toolName, args) {
  if (!args || typeof args !== "object") return "";
  if (toolName === "bash") return typeof args.command === "string" ? args.command : "";
  if (["read", "write", "edit", "ls", "find", "grep", "lsp"].includes(toolName)) {
    const target = args.path ?? args.file_path ?? args.pattern ?? args.action;
    return typeof target === "string" ? target : "";
  }
  if (toolName === "rdk_docs_search") return typeof args.query === "string" ? args.query : "";
  return "";
}

export function applySideAgentEvent(state, event, redact = (value) => value) {
  if (!event || typeof event !== "object") return state;
  const next = { ...state, tools: [...state.tools] };

  if (event.type === "message_update") {
    const update = event.assistantMessageEvent;
    if (update?.type === "text_delta" && typeof update.delta === "string") {
      next.streamingText = boundedTail(next.streamingText + update.delta);
    } else if (update?.type === "thinking_delta" && typeof update.delta === "string") {
      next.thinkingChars += update.delta.length;
      next.thinkingText = boundedTail(next.thinkingText + update.delta);
    }
  } else if (event.type === "message_end" && event.message?.role === "assistant") {
    const text = assistantText(event.message);
    const thinking = assistantThinking(event.message);
    if (text) next.finalText = boundedTail(text);
    if (thinking) next.finalThinking = boundedTail(thinking);
    next.streamingText = "";
    next.thinkingText = "";
    next.turns += 1;
    next.stopReason = event.message.stopReason;
    next.errorMessage = event.message.errorMessage;
    const usage = event.message.usage ?? {};
    next.inputTokens += boundedTokenCount(usage.input);
    next.outputTokens += boundedTokenCount(usage.output);
    next.cacheReadTokens += boundedTokenCount(usage.cacheRead);
    next.cacheWriteTokens += boundedTokenCount(usage.cacheWrite);
  } else if (event.type === "tool_execution_start") {
    const rawTarget = toolTarget(String(event.toolName ?? "tool"), event.args);
    const target = redact(rawTarget).replace(/\s+/g, " ").slice(0, 160);
    next.tools.push({
      id: String(event.toolCallId ?? next.tools.length),
      name: String(event.toolName ?? "tool"),
      target,
      status: "running",
    });
    next.tools = next.tools.slice(-MAX_TOOL_EVENTS);
  } else if (event.type === "tool_execution_end") {
    const id = String(event.toolCallId ?? "");
    const index = next.tools.findIndex((tool) => tool.id === id);
    if (index >= 0) next.tools[index] = { ...next.tools[index], status: event.isError ? "failed" : "done" };
  }
  return next;
}

export function parseSideAgentEvent(line) {
  if (!line.trim()) return undefined;
  try {
    return JSON.parse(line);
  } catch {
    return undefined;
  }
}
