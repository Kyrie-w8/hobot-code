const SESSION_VERSION = 3;
const MAX_STREAM_TEXT_CHARS = 200_000;
const MAX_TOOL_EVENTS = 100;

function boundedTail(value, limit = MAX_STREAM_TEXT_CHARS) {
  if (value.length <= limit) return value;
  return `[Earlier output omitted]\n${value.slice(-limit)}`;
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
    "json",
    "--print",
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
    }
  } else if (event.type === "message_end" && event.message?.role === "assistant") {
    const text = assistantText(event.message);
    if (text) next.finalText = boundedTail(text);
    next.streamingText = "";
    next.turns += 1;
    next.stopReason = event.message.stopReason;
    next.errorMessage = event.message.errorMessage;
    const usage = event.message.usage ?? {};
    next.inputTokens += Number(usage.input ?? 0);
    next.outputTokens += Number(usage.output ?? 0);
    next.cacheReadTokens += Number(usage.cacheRead ?? 0);
    next.cacheWriteTokens += Number(usage.cacheWrite ?? 0);
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
