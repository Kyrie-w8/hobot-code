import {
  type Api,
  type AssistantMessage,
  type AssistantMessageEventStream,
  type Context,
  type ImageContent,
  type Message,
  type Model,
  type SimpleStreamOptions,
  type StopReason,
  type TextContent,
  type ThinkingContent,
  type Tool,
  type ToolCall,
  type ToolResultMessage,
  Type,
  calculateCost,
  createAssistantMessageEventStream,
} from "@earendil-works/pi-ai";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { execFile } from "node:child_process";
import { access, readFile, readdir } from "node:fs/promises";
import { cpus, freemem, hostname, loadavg, platform, release, totalmem, uptime } from "node:os";
import { resolve } from "node:path";
import { promisify } from "node:util";

import {
  describeToolCall,
  fingerprintWorkspace,
  initializeProject,
  isMcpTool,
  loadPolicy,
  loadQualityConfig,
  parsePolicy,
  parseQualityConfig,
  redactSensitiveText,
  resolveToolAction,
  setPolicyRule,
  writePolicy,
} from "./control-plane.mjs";

const execFileAsync = promisify(execFile);
const DEFAULT_BASE_URL = "https://ai-api.d-robotics.cc";
const DEFAULT_MODEL = "kimi-k3";
const DEFAULT_EXPERT_PROMPT_PATH = "/usr/local/lib/hobot-code/prompts/rdk-expert.md";
const EXPERT_PROMPT_MARKER = "# Hobot Code 地瓜机器人 RDK 开发专家";

type JsonRecord = Record<string, unknown>;

interface BoardSnapshot {
  board: string;
  boardId: "x5" | "s100" | "s600" | "unknown";
  rdkOsVersion: string;
  documentationTrack: string;
  hostname: string;
  platform: string;
  kernel: string;
  architecture: string;
  cpuCores: number;
  memoryTotalMiB: number;
  memoryFreeMiB: number;
  loadAverage: number[];
  uptimeSeconds: number;
  os: Record<string, string>;
  bpuDevices: string[];
  thermalZones: Array<{ name: string; celsius: number }>;
  rdkUtilities: Record<string, boolean>;
  processes?: string;
}

interface KnowledgeSource {
  title: string;
  url: string;
}

interface KnowledgeDocument {
  id: string;
  title: string;
  file: string;
  boards: string[];
  rdkOs: string[];
  topics: string[];
  sources: KnowledgeSource[];
}

interface KnowledgeManifest {
  schemaVersion: number;
  knowledgeVersion: string;
  updatedAt: string;
  documents: KnowledgeDocument[];
}

interface KnowledgeSearchOptions {
  query: string;
  boardId: BoardSnapshot["boardId"];
  rdkOsVersion: string;
  topic?: string;
  limit?: number;
}

type PermissionAction = "allow" | "ask" | "deny";

interface PermissionRule {
  tool: string;
  action: PermissionAction;
}

interface PermissionPolicy {
  schemaVersion: 1;
  default: PermissionAction;
  rules: PermissionRule[];
}

interface QualityGateResult {
  command: string;
  code: number | null;
  killed: boolean;
  durationMs: number;
  stdout: string;
  stderr: string;
}

interface QualityGateRun {
  passed: boolean;
  startedAt: string;
  durationMs: number;
  workspaceFingerprint?: string;
  results: QualityGateResult[];
}

interface QualityGateState {
  schemaVersion: 1;
  timeoutMs: number;
  commands: string[];
  source: "project" | "session";
  lastRun?: QualityGateRun;
  invalidated?: boolean;
}

type QualityGateStatus = "disabled" | "missing" | "running" | "passed" | "failed" | "stale";

function sanitizeText(value: string): string {
  return value.replace(/[\uD800-\uDFFF]/g, "\uFFFD");
}

async function readText(paths: string[]): Promise<string | undefined> {
  for (const path of paths) {
    try {
      return (await readFile(path, "utf8")).replace(/\0/g, "").trim();
    } catch {
      // Continue with the next board-specific path.
    }
  }
  return undefined;
}

function parseOsRelease(raw: string | undefined): Record<string, string> {
  const result: Record<string, string> = {};
  for (const line of raw?.split("\n") ?? []) {
    const index = line.indexOf("=");
    if (index < 1) continue;
    result[line.slice(0, index)] = line
      .slice(index + 1)
      .replace(/^['\"]|['\"]$/g, "");
  }
  return result;
}

function detectBoardId(board: string | undefined): BoardSnapshot["boardId"] {
  const normalized = board?.toLowerCase() ?? "";
  if (normalized.includes("s600")) return "s600";
  if (normalized.includes("s100")) return "s100";
  if (normalized.includes("x5")) return "x5";
  return "unknown";
}

function documentationTrack(boardId: BoardSnapshot["boardId"], version: string): string {
  if (boardId === "x5") return `RDK X series ${version || "3.x"}`;
  if (boardId === "s100") return `RDK S100 ${version || "4.x"}`;
  if (boardId === "s600") return `RDK S600 ${version || "5.x"}`;
  return "Unmatched RDK documentation track";
}

async function listMatching(directory: string, pattern: RegExp): Promise<string[]> {
  try {
    return (await readdir(directory)).filter((name) => pattern.test(name)).sort();
  } catch {
    return [];
  }
}

async function commandExists(paths: string[]): Promise<boolean> {
  for (const path of paths) {
    try {
      await access(path);
      return true;
    } catch {
      // Keep looking.
    }
  }
  return false;
}

async function readThermals(): Promise<Array<{ name: string; celsius: number }>> {
  const zones = await listMatching("/sys/class/thermal", /^thermal_zone\d+$/);
  const values: Array<{ name: string; celsius: number }> = [];
  for (const zone of zones) {
    const root = `/sys/class/thermal/${zone}`;
    const [name, rawTemperature] = await Promise.all([
      readText([`${root}/type`]),
      readText([`${root}/temp`]),
    ]);
    const temperature = Number(rawTemperature);
    if (Number.isFinite(temperature)) {
      values.push({ name: name || zone, celsius: Math.round((temperature / 1000) * 10) / 10 });
    }
  }
  return values;
}

async function getBoardSnapshot(includeProcesses = false): Promise<BoardSnapshot> {
  const [board, versionFile, osRelease, devEntries, thermalZones, somStatus, modelExec, rdkosInfo] = await Promise.all([
    readText(["/sys/firmware/devicetree/base/model", "/proc/device-tree/model"]),
    readText(["/etc/version"]),
    readText(["/etc/os-release"]),
    listMatching("/dev", /^(bpu|hobot|ion|dnn)/i),
    readThermals(),
    commandExists(["/usr/bin/hrut_somstatus", "/usr/local/bin/hrut_somstatus"]),
    commandExists(["/usr/bin/hrt_model_exec", "/usr/local/bin/hrt_model_exec"]),
    commandExists(["/usr/bin/rdkos_info", "/usr/local/bin/rdkos_info"]),
  ]);

  const os = parseOsRelease(osRelease);
  const boardId = detectBoardId(board);
  const rdkOsVersion = versionFile || os.VERSION_ID?.replace(/^V/i, "") || "unknown";

  let processes: string | undefined;
  if (includeProcesses) {
    try {
      const result = await execFileAsync("ps", ["-eo", "pid,comm,%cpu,%mem", "--sort=-%cpu"], {
        timeout: 2000,
        maxBuffer: 64 * 1024,
      });
      processes = result.stdout.split("\n").slice(0, 12).join("\n").trim();
    } catch {
      processes = "process listing unavailable";
    }
  }

  return {
    board: board || "Unknown ARM64 Linux board",
    boardId,
    rdkOsVersion,
    documentationTrack: documentationTrack(boardId, rdkOsVersion),
    hostname: hostname(),
    platform: platform(),
    kernel: release(),
    architecture: process.arch,
    cpuCores: cpus().length,
    memoryTotalMiB: Math.round(totalmem() / 1024 / 1024),
    memoryFreeMiB: Math.round(freemem() / 1024 / 1024),
    loadAverage: loadavg().map((value) => Math.round(value * 100) / 100),
    uptimeSeconds: Math.round(uptime()),
    os,
    bpuDevices: devEntries.map((name) => `/dev/${name}`),
    thermalZones,
    rdkUtilities: {
      hrut_somstatus: somStatus,
      hrt_model_exec: modelExec,
      rdkos_info: rdkosInfo,
    },
    ...(processes ? { processes } : {}),
  };
}

function compactBoardSummary(snapshot: BoardSnapshot): string {
  const temperature = snapshot.thermalZones[0]?.celsius;
  return [
    `${snapshot.board} | RDK OS ${snapshot.rdkOsVersion}`,
    `${snapshot.cpuCores} CPU`,
    `${snapshot.memoryTotalMiB} MiB RAM`,
    temperature === undefined ? undefined : `${temperature} C`,
  ]
    .filter(Boolean)
    .join(" | ");
}

function wildcardMatches(value: string, pattern: string): boolean {
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*");
  return new RegExp(`^${escaped}$`, "i").test(value);
}

function queryTerms(query: string): string[] {
  const normalized = query.toLowerCase().trim();
  const terms = normalized.match(/[\p{L}\p{N}][\p{L}\p{N}_.+-]*/gu) ?? [];
  return [...new Set([normalized, ...terms].filter((term) => term.length > 1))];
}

function occurrences(haystack: string, needle: string): number {
  if (!needle) return 0;
  let count = 0;
  let offset = 0;
  while ((offset = haystack.indexOf(needle, offset)) >= 0) {
    count += 1;
    offset += needle.length;
  }
  return count;
}

function knowledgeRoot(): string {
  return resolve(
    process.env.HOBOT_CODE_RDK_KNOWLEDGE_DIR
      || "/usr/local/lib/hobot-code/knowledge",
  );
}

function expertPromptPath(): string {
  return resolve(
    process.env.HOBOT_CODE_RDK_EXPERT_PROMPT
      || DEFAULT_EXPERT_PROMPT_PATH,
  );
}

async function renderExpertPrompt(snapshot: BoardSnapshot): Promise<string> {
  let prompt: string;
  try {
    prompt = await readFile(expertPromptPath(), "utf8");
  } catch {
    prompt = `${EXPERT_PROMPT_MARKER}\n\nYou are a D-Robotics RDK embedded AI expert. Use rdk_docs_search for versioned platform knowledge and system_snapshot for live evidence. Do not make specialized claims while the complete expert prompt file is unavailable.`;
  }

  const replacements: Record<string, string> = {
    "{{BOARD_NAME}}": snapshot.board,
    "{{BOARD_ID}}": snapshot.boardId,
    "{{RDK_OS_VERSION}}": snapshot.rdkOsVersion,
    "{{DOCUMENTATION_TRACK}}": snapshot.documentationTrack,
    "{{HOSTNAME}}": snapshot.hostname,
    "{{ARCHITECTURE}}": snapshot.architecture,
  };
  for (const [token, value] of Object.entries(replacements)) {
    prompt = prompt.replaceAll(token, () => sanitizeText(value));
  }
  return prompt.trim();
}

async function loadKnowledgeManifest(root: string): Promise<KnowledgeManifest> {
  const manifest = JSON.parse(await readFile(resolve(root, "manifest.json"), "utf8")) as KnowledgeManifest;
  if (manifest.schemaVersion !== 1 || !Array.isArray(manifest.documents)) {
    throw new Error("Unsupported or invalid RDK knowledge manifest");
  }
  return manifest;
}

function selectSnippet(body: string, terms: string[]): string {
  const paragraphs = body
    .split(/\n\s*\n/)
    .map((paragraph) => paragraph.replace(/\s+/g, " ").trim())
    .filter(Boolean);
  const paragraph = paragraphs
    .map((candidate) => {
      const normalized = candidate.toLowerCase();
      const score = terms.reduce((total, term) => total + occurrences(normalized, term), 0);
      return { candidate, score };
    })
    .sort((left, right) => right.score - left.score || right.candidate.length - left.candidate.length)[0]
    ?.candidate ?? "";
  return paragraph.length > 700 ? `${paragraph.slice(0, 697)}...` : paragraph;
}

async function searchKnowledge(options: KnowledgeSearchOptions): Promise<JsonRecord> {
  const root = knowledgeRoot();
  const manifest = await loadKnowledgeManifest(root);
  const terms = queryTerms(options.query);
  if (terms.length === 0) throw new Error("Knowledge query must not be empty");
  const requestedTopic = options.topic?.toLowerCase().trim();
  const limit = Math.max(1, Math.min(options.limit ?? 4, 8));
  const results: Array<JsonRecord & { score: number }> = [];

  for (const document of manifest.documents) {
    if (!document.boards.includes("all") && !document.boards.includes(options.boardId)) continue;
    if (requestedTopic && !document.topics.some((topic) => topic.toLowerCase() === requestedTopic)) continue;

    const documentPath = resolve(root, document.file);
    if (documentPath !== root && !documentPath.startsWith(`${root}/`)) {
      throw new Error(`Knowledge document escapes its root: ${document.file}`);
    }
    const body = await readFile(documentPath, "utf8");
    const title = document.title.toLowerCase();
    const topics = document.topics.join(" ").toLowerCase();
    const normalizedBody = body.toLowerCase();
    let lexicalScore = 0;
    for (const term of terms) {
      lexicalScore += occurrences(title, term) * 12;
      lexicalScore += occurrences(topics, term) * 8;
      lexicalScore += Math.min(occurrences(normalizedBody, term), 8) * 2;
    }
    if (lexicalScore <= 0) continue;
    let score = lexicalScore;
    if (document.boards.includes(options.boardId)) score += 3;
    const versionMatch = document.rdkOs.some((pattern) => wildcardMatches(options.rdkOsVersion, pattern));
    if (versionMatch) score += 3;
    results.push({
      score,
      id: document.id,
      title: document.title,
      boards: document.boards,
      applicableRdkOs: document.rdkOs,
      detectedRdkOs: options.rdkOsVersion,
      versionMatch,
      topics: document.topics,
      snippet: selectSnippet(body, terms),
      sources: document.sources,
    });
  }

  results.sort((left, right) => right.score - left.score || String(left.id).localeCompare(String(right.id)));
  return {
    knowledgeVersion: manifest.knowledgeVersion,
    updatedAt: manifest.updatedAt,
    detectedBoard: options.boardId,
    detectedRdkOs: options.rdkOsVersion,
    query: options.query,
    results: results.slice(0, limit).map(({ score: _score, ...result }) => result),
  };
}

function convertContentBlocks(content: Array<TextContent | ImageContent>): unknown {
  const blocks = content.map((block) => {
    if (block.type === "text") return { type: "text", text: sanitizeText(block.text) };
    return {
      type: "image",
      source: { type: "base64", media_type: block.mimeType, data: block.data },
    };
  });
  return blocks.length === 1 && blocks[0]?.type === "text" ? blocks[0].text : blocks;
}

function convertMessages(messages: Message[]): JsonRecord[] {
  const converted: JsonRecord[] = [];

  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (message.role === "user") {
      const content =
        typeof message.content === "string"
          ? sanitizeText(message.content)
          : convertContentBlocks(message.content as Array<TextContent | ImageContent>);
      converted.push({ role: "user", content });
      continue;
    }

    if (message.role === "assistant") {
      const content: JsonRecord[] = [];
      for (const block of message.content) {
        if (block.type === "text" && block.text) {
          content.push({ type: "text", text: sanitizeText(block.text) });
        } else if (block.type === "thinking" && block.thinking) {
          content.push({
            type: "thinking",
            thinking: sanitizeText(block.thinking),
            signature: (block as ThinkingContent).thinkingSignature ?? "",
          });
        } else if (block.type === "toolCall") {
          content.push({ type: "tool_use", id: block.id, name: block.name, input: block.arguments });
        }
      }
      if (content.length > 0) converted.push({ role: "assistant", content });
      continue;
    }

    if (message.role === "toolResult") {
      const results: JsonRecord[] = [];
      let current = message as ToolResultMessage;
      while (true) {
        results.push({
          type: "tool_result",
          tool_use_id: current.toolCallId,
          content: convertContentBlocks(current.content),
          is_error: current.isError,
        });
        const next = messages[index + 1];
        if (!next || next.role !== "toolResult") break;
        index += 1;
        current = next as ToolResultMessage;
      }
      converted.push({ role: "user", content: results });
    }
  }

  return converted;
}

function convertTools(tools: Tool[] | undefined): JsonRecord[] | undefined {
  if (!tools?.length) return undefined;
  return tools.map((tool) => ({
    name: tool.name,
    description: tool.description,
    input_schema: tool.parameters,
  }));
}

function mapStopReason(reason: string | undefined): StopReason {
  switch (reason) {
    case "end_turn":
    case "stop_sequence":
      return "stop";
    case "max_tokens":
      return "length";
    case "tool_use":
      return "toolUse";
    default:
      return "stop";
  }
}

function thinkingBudget(level: SimpleStreamOptions["reasoning"], maxTokens: number): number | undefined {
  if (!level || level === "off") return undefined;
  const requested: Record<string, number> = {
    minimal: 1024,
    low: 2048,
    medium: 4096,
    high: 6144,
    xhigh: 6144,
    max: 6144,
  };
  return Math.max(1024, Math.min(requested[level] ?? 4096, maxTokens - 1024));
}

function streamDrobotics(
  model: Model<Api>,
  context: Context,
  options?: SimpleStreamOptions,
): AssistantMessageEventStream {
  const stream = createAssistantMessageEventStream();

  void (async () => {
    const output: AssistantMessage = {
      role: "assistant",
      content: [],
      api: model.api,
      provider: model.provider,
      model: model.id,
      usage: {
        input: 0,
        output: 0,
        cacheRead: 0,
        cacheWrite: 0,
        totalTokens: 0,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
      },
      stopReason: "pending",
      timestamp: Date.now(),
    };

    try {
      if (!options?.apiKey) throw new Error("ANTHROPIC_AUTH_TOKEN is not configured");
      stream.push({ type: "start", partial: output });

      const maxTokens = Math.min(options.maxTokens ?? model.maxTokens, model.maxTokens);
      const budget = thinkingBudget(options.reasoning, maxTokens);
      const body: JsonRecord = {
        model: model.id,
        max_tokens: maxTokens,
        stream: false,
        system: context.systemPrompt ? sanitizeText(context.systemPrompt) : undefined,
        messages: convertMessages(context.messages),
        tools: convertTools(context.tools),
        ...(budget ? { thinking: { type: "enabled", budget_tokens: budget } } : {}),
      };

      const configuredHeaders: Record<string, string> = {};
      for (const [name, value] of Object.entries(options.headers ?? {})) {
        if (typeof value === "string") configuredHeaders[name] = value;
      }
      configuredHeaders.Authorization ||= `Bearer ${options.apiKey}`;

      const response = await fetch(`${(model.baseUrl || DEFAULT_BASE_URL).replace(/\/$/, "")}/v1/messages`, {
        method: "POST",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
          "anthropic-version": "2023-06-01",
          "User-Agent": "hobot-code/0.9",
          ...configuredHeaders,
        },
        body: JSON.stringify(body),
        signal: options.signal,
      });

      if (!response.ok) {
        const detail = (await response.text()).slice(0, 4096);
        throw new Error(`D-Robotics model gateway HTTP ${response.status}: ${detail}`);
      }

      const result = (await response.json()) as {
        content?: Array<{
          type: "thinking" | "reasoning" | "text" | "tool_use";
          thinking?: string;
          text?: string;
          signature?: string;
          id?: string;
          name?: string;
          input?: JsonRecord;
        }>;
        stop_reason?: string;
        usage?: {
          input_tokens?: number;
          output_tokens?: number;
          cache_read_input_tokens?: number;
          cache_creation_input_tokens?: number;
        };
      };

      for (const block of result.content ?? []) {
        const contentIndex = output.content.length;
        if (block.type === "thinking" || block.type === "reasoning") {
          const thinking = block.thinking ?? block.text ?? "";
          output.content.push({
            type: "thinking",
            thinking,
            thinkingSignature: block.signature ?? "",
          });
          stream.push({ type: "thinking_start", contentIndex, partial: output });
          if (thinking) stream.push({ type: "thinking_delta", contentIndex, delta: thinking, partial: output });
          stream.push({ type: "thinking_end", contentIndex, content: thinking, partial: output });
        } else if (block.type === "text") {
          const text = block.text ?? "";
          output.content.push({ type: "text", text });
          stream.push({ type: "text_start", contentIndex, partial: output });
          if (text) stream.push({ type: "text_delta", contentIndex, delta: text, partial: output });
          stream.push({ type: "text_end", contentIndex, content: text, partial: output });
        } else if (block.type === "tool_use" && block.id && block.name) {
          const toolCall: ToolCall = {
            type: "toolCall",
            id: block.id,
            name: block.name,
            arguments: block.input ?? {},
          };
          output.content.push(toolCall);
          stream.push({ type: "toolcall_start", contentIndex, partial: output });
          stream.push({ type: "toolcall_end", contentIndex, toolCall, partial: output });
        }
      }

      output.usage.input = result.usage?.input_tokens ?? 0;
      output.usage.output = result.usage?.output_tokens ?? 0;
      output.usage.cacheRead = result.usage?.cache_read_input_tokens ?? 0;
      output.usage.cacheWrite = result.usage?.cache_creation_input_tokens ?? 0;
      output.usage.totalTokens =
        output.usage.input + output.usage.output + output.usage.cacheRead + output.usage.cacheWrite;
      calculateCost(model, output.usage);
      output.stopReason = mapStopReason(result.stop_reason);
      stream.push({ type: "done", reason: output.stopReason, message: output });
      stream.end();
    } catch (error) {
      output.stopReason = options?.signal?.aborted ? "aborted" : "error";
      output.errorMessage = error instanceof Error ? error.message : String(error);
      stream.push({ type: "error", reason: output.stopReason, error: output });
      stream.end();
    }
  })();

  return stream;
}

const systemSnapshotSchema = Type.Object({
  includeProcesses: Type.Optional(
    Type.Boolean({ description: "Include the highest CPU processes in the snapshot" }),
  ),
});

const knowledgeSearchSchema = Type.Object({
  query: Type.String({ description: "Question or keywords about D-Robotics RDK development" }),
  board: Type.Optional(
    Type.String({ description: "Override detected board: x5, s100, s600, or unknown" }),
  ),
  topic: Type.Optional(
    Type.String({ description: "Optional exact topic filter such as bpu, multimedia, tros, diagnostics, or safety" }),
  ),
  limit: Type.Optional(
    Type.Integer({ minimum: 1, maximum: 8, description: "Maximum number of knowledge documents" }),
  ),
});

const qualityGateSchema = Type.Object({
  action: Type.Union([
    Type.Literal("status"),
    Type.Literal("run"),
  ], { description: "Show quality gate status or run every configured command" }),
});

const criticalVirtualRoots = ["/proc", "/sys", "/dev"];
const destructiveCommandPatterns = [
  /(^|\s)rm\s+[^\n]*(?:-rf|-fr|--recursive)/i,
  /(^|\s)(?:mkfs|fdisk|parted)\b/i,
  /(^|\s)dd\s+[^\n]*\bof=\/dev\//i,
  /(^|\s)(?:reboot|poweroff|halt)\b/i,
  /systemctl\s+(?:reboot|poweroff|halt)/i,
  />\s*\/sys\//i,
];
const mutatingToolNames = new Set(["bash", "edit", "write"]);
const completionAssertionPattern = /(?:已|已经|全部|现已)(?:完成|实现|修复|通过|部署)|(?:implementation|task|work|changes?)\s+(?:is|are)\s+(?:complete|done)|all\s+(?:checks|tests|gates)\s+pass/i;
const qualityGateEntryType = "hobot-quality-gates";

function permissionPolicyPath(): string {
  return resolve(process.env.HOBOT_CODE_PERMISSION_POLICY || "/etc/hobot-code/agent/permissions.json");
}

function outputTail(value: string, maxLength = 4000): string {
  const redacted = redactSensitiveText(value, maxLength * 2);
  return redacted.length > maxLength ? `...${redacted.slice(-maxLength)}` : redacted;
}

function qualityStatusText(status: QualityGateStatus): string {
  switch (status) {
    case "disabled": return "disabled";
    case "missing": return "not run";
    case "running": return "running";
    case "passed": return "passed";
    case "failed": return "failed";
    case "stale": return "stale";
  }
}

function normalizeQualityState(value: unknown): QualityGateState | undefined {
  if (!value || typeof value !== "object") return undefined;
  const candidate = value as Partial<QualityGateState>;
  try {
    const config = parseQualityConfig({
      schemaVersion: candidate.schemaVersion,
      timeoutMs: candidate.timeoutMs,
      commands: candidate.commands,
    });
    return {
      ...config,
      source: candidate.source === "session" ? "session" : "project",
      ...(candidate.lastRun ? { lastRun: candidate.lastRun } : {}),
      ...(candidate.invalidated ? { invalidated: true } : {}),
    };
  } catch {
    return undefined;
  }
}

function gateReport(state: QualityGateState, status: QualityGateStatus): string {
  const commands = state.commands.length > 0
    ? state.commands.map((command, index) => `${index + 1}. ${command}`).join("\n")
    : "(none)";
  const latest = state.lastRun
    ? `${state.lastRun.passed ? "passed" : "failed"} at ${state.lastRun.startedAt} in ${state.lastRun.durationMs} ms`
    : "never";
  return [
    `Status: ${qualityStatusText(status)}`,
    `Source: ${state.source}`,
    `Timeout per command: ${state.timeoutMs} ms`,
    `Last run: ${latest}`,
    "Commands:",
    commands,
  ].join("\n");
}

export default function rdkExtension(pi: ExtensionAPI) {
  const baseUrl = process.env.ANTHROPIC_BASE_URL || DEFAULT_BASE_URL;
  const modelId = process.env.ANTHROPIC_MODEL || DEFAULT_MODEL;
  let currentSnapshot: BoardSnapshot | undefined;
  let permissionPolicy: PermissionPolicy;
  let permissionPolicyError: string | undefined;
  let qualityGateState: QualityGateState = {
    schemaVersion: 1,
    timeoutMs: 120_000,
    commands: [],
    source: "project",
  };
  let qualityGateStatus: QualityGateStatus = "disabled";
  let qualityConfigError: string | undefined;
  const qualityGateBlockedCalls = new Set<string>();

  function toolAction(toolName: string): PermissionAction {
    const info = pi.getAllTools().find((tool) => tool.name === toolName);
    return resolveToolAction(permissionPolicy, toolName, isMcpTool(info ?? toolName)) as PermissionAction;
  }

  function toolIsMcp(toolName: string): boolean {
    const info = pi.getAllTools().find((tool) => tool.name === toolName);
    return isMcpTool(info ?? toolName);
  }

  function applyDeniedTools(): string[] {
    const denied = pi.getAllTools()
      .map((tool) => tool.name)
      .filter((name) => toolAction(name) === "deny");
    if (denied.length > 0) {
      const deniedSet = new Set(denied);
      pi.setActiveTools(pi.getActiveTools().filter((name) => !deniedSet.has(name)));
    }
    return denied;
  }

  function persistQualityState(): void {
    pi.appendEntry(qualityGateEntryType, { ...qualityGateState });
  }

  function setQualityStatus(ctx: { ui: { setStatus: (key: string, value: string) => void } }, status: QualityGateStatus): void {
    qualityGateStatus = status;
    ctx.ui.setStatus("hobot-gates", `gates: ${qualityStatusText(status)}`);
  }

  async function evaluateQualityStatus(cwd: string): Promise<QualityGateStatus> {
    if (qualityGateState.commands.length === 0) return "disabled";
    if (qualityGateStatus === "running") return "running";
    if (!qualityGateState.lastRun) return "missing";
    if (!qualityGateState.lastRun.passed) return "failed";
    if (qualityGateState.invalidated || !qualityGateState.lastRun.workspaceFingerprint) return "stale";
    try {
      const current = await fingerprintWorkspace(cwd);
      return current.digest === qualityGateState.lastRun.workspaceFingerprint ? "passed" : "stale";
    } catch {
      return "stale";
    }
  }

  async function restoreQualityState(ctx: { cwd: string; sessionManager: { getBranch: () => unknown[] } }): Promise<void> {
    let restored: QualityGateState | undefined;
    for (const entry of ctx.sessionManager.getBranch()) {
      const candidate = entry as { type?: string; customType?: string; data?: unknown };
      if (candidate.type === "custom" && candidate.customType === qualityGateEntryType) {
        restored = normalizeQualityState(candidate.data) ?? restored;
      }
    }
    if (restored) {
      qualityGateState = restored;
      qualityConfigError = undefined;
      return;
    }
    const loaded = await loadQualityConfig(ctx.cwd);
    qualityGateState = { ...loaded.config, source: "project" };
    qualityConfigError = loaded.error;
  }

  async function runQualityGates(
    cwd: string,
    signal?: AbortSignal,
    ctx?: { ui: { setStatus: (key: string, value: string) => void } },
  ): Promise<{ text: string; details: JsonRecord }> {
    if (qualityGateState.commands.length === 0) {
      return {
        text: "No quality gates are configured. Run /init or /gate set <command> before claiming completion.",
        details: { status: "disabled" },
      };
    }

    qualityGateStatus = "running";
    ctx?.ui.setStatus("hobot-gates", "gates: running");
    const started = Date.now();
    const results: QualityGateResult[] = [];

    for (const command of qualityGateState.commands) {
      const commandStarted = Date.now();
      try {
        const result = await pi.exec("sh", ["-c", command], {
          timeout: qualityGateState.timeoutMs,
          signal,
        });
        results.push({
          command,
          code: result.code,
          killed: result.killed,
          durationMs: Date.now() - commandStarted,
          stdout: outputTail(result.stdout),
          stderr: outputTail(result.stderr),
        });
        if (result.code !== 0 || result.killed) break;
      } catch (error) {
        results.push({
          command,
          code: null,
          killed: signal?.aborted ?? false,
          durationMs: Date.now() - commandStarted,
          stdout: "",
          stderr: outputTail(error instanceof Error ? error.message : String(error)),
        });
        break;
      }
    }

    let passed = results.length === qualityGateState.commands.length
      && results.every((result) => result.code === 0 && !result.killed);
    let workspaceFingerprint: string | undefined;
    try {
      workspaceFingerprint = (await fingerprintWorkspace(cwd)).digest;
    } catch (error) {
      passed = false;
      results.push({
        command: "workspace fingerprint",
        code: null,
        killed: false,
        durationMs: 0,
        stdout: "",
        stderr: outputTail(error instanceof Error ? error.message : String(error)),
      });
    }

    qualityGateState = {
      ...qualityGateState,
      invalidated: false,
      lastRun: {
        passed,
        startedAt: new Date(started).toISOString(),
        durationMs: Date.now() - started,
        workspaceFingerprint,
        results,
      },
    };
    persistQualityState();
    const status: QualityGateStatus = passed ? "passed" : "failed";
    qualityGateStatus = status;
    ctx?.ui.setStatus("hobot-gates", `gates: ${status}`);

    const lines = results.map((result) => [
      `${result.code === 0 && !result.killed ? "PASS" : "FAIL"} ${result.command} (${result.durationMs} ms)`,
      result.stdout ? `stdout:\n${result.stdout}` : undefined,
      result.stderr ? `stderr:\n${result.stderr}` : undefined,
    ].filter(Boolean).join("\n"));
    return {
      text: [`Quality gates ${status}.`, ...lines].join("\n\n"),
      details: { status, passed, results },
    };
  }

  pi.registerProvider("drobotics", {
    name: "D-Robotics AI Gateway",
    baseUrl,
    apiKey: "$ANTHROPIC_AUTH_TOKEN",
    api: "drobotics-anthropic",
    streamSimple: streamDrobotics,
    models: [
      {
        id: modelId,
        name: `${modelId} (D-Robotics)`,
        reasoning: true,
        thinkingLevelMap: {
          xhigh: "xhigh",
          max: "max",
        },
        input: ["text", "image"],
        contextWindow: 1_000_000,
        maxTokens: 8192,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      },
    ],
  });

  pi.registerTool({
    name: "system_snapshot",
    label: "RDK system snapshot",
    description: "Read live RDK board identity, CPU, memory, load, BPU device nodes, temperatures, and runtime tools.",
    promptSnippet: "Inspect live D-Robotics RDK board resources and BPU runtime availability",
    promptGuidelines: [
      "Use system_snapshot for live board facts instead of assuming capabilities from the board name.",
      "A BPU device node or hrt_model_exec binary does not prove that a specific model is converted or compatible.",
    ],
    parameters: systemSnapshotSchema,
    async execute(_toolCallId, params) {
      const snapshot = await getBoardSnapshot(params.includeProcesses ?? false);
      currentSnapshot = snapshot;
      return {
        content: [{ type: "text", text: JSON.stringify(snapshot, null, 2) }],
        details: snapshot,
      };
    },
  });

  pi.registerTool({
    name: "rdk_docs_search",
    label: "Search RDK board knowledge",
    description: "Search the local, versioned D-Robotics RDK knowledge pack and return concise results with official source URLs and version applicability.",
    promptSnippet: "Search board-specific, version-aware RDK documentation before making specialized platform claims",
    promptGuidelines: [
      "Use rdk_docs_search before advising on BPU conversion, multimedia, TROS, drivers, board interfaces, or RDK-version-specific commands.",
      "Treat search results as documentation and system_snapshot as live evidence; call out a version mismatch instead of silently applying incompatible instructions.",
      "Preserve source URLs in technical answers when they materially support the recommendation.",
    ],
    parameters: knowledgeSearchSchema,
    async execute(_toolCallId, params) {
      const snapshot = currentSnapshot ?? await getBoardSnapshot(false);
      currentSnapshot = snapshot;
      const requestedBoard = String(params.board ?? snapshot.boardId).toLowerCase();
      const boardId: BoardSnapshot["boardId"] = ["x5", "s100", "s600"].includes(requestedBoard)
        ? requestedBoard as BoardSnapshot["boardId"]
        : "unknown";
      const result = await searchKnowledge({
        query: params.query,
        boardId,
        rdkOsVersion: snapshot.rdkOsVersion,
        topic: params.topic,
        limit: params.limit,
      });
      return {
        content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
        details: result,
      };
    },
  });

  pi.registerTool({
    name: "quality_gate",
    label: "Hobot Code quality gate",
    description: "Inspect or run the verification commands configured for this session. A passing result is tied to the current workspace fingerprint.",
    promptSnippet: "Run project verification commands and certify the current workspace before declaring completion",
    promptGuidelines: [
      "Use quality_gate with action run after the final code change when project quality gates are configured.",
      "Do not claim that work is complete when Hobot Code reports the quality gate as missing, stale, or failed.",
    ],
    parameters: qualityGateSchema,
    async execute(_toolCallId, params, signal, _onUpdate, ctx) {
      if (params.action === "run") {
        const result = await runQualityGates(ctx.cwd, signal, ctx);
        return {
          content: [{ type: "text", text: result.text }],
          details: result.details,
        };
      }
      const status = await evaluateQualityStatus(ctx.cwd);
      setQualityStatus(ctx, status);
      return {
        content: [{ type: "text", text: gateReport(qualityGateState, status) }],
        details: { status, state: qualityGateState },
      };
    },
  });

  pi.on("session_start", async (_event, ctx) => {
    const loadedPolicy = await loadPolicy(permissionPolicyPath());
    permissionPolicy = loadedPolicy.policy as PermissionPolicy;
    permissionPolicyError = loadedPolicy.error;
    applyDeniedTools();
    await restoreQualityState(ctx);
    setQualityStatus(ctx, await evaluateQualityStatus(ctx.cwd));

    try {
      const snapshot = await getBoardSnapshot(false);
      currentSnapshot = snapshot;
      ctx.ui.setStatus("hobot-rdk", compactBoardSummary(snapshot));
    } catch {
      ctx.ui.setStatus("hobot-rdk", "RDK status unavailable");
    }

    if (permissionPolicyError) {
      ctx.ui.notify(`Permission policy fallback is active: ${permissionPolicyError}`, "warning");
    }
    if (qualityConfigError) {
      ctx.ui.notify(`Quality gate config was ignored: ${qualityConfigError}`, "warning");
    }
  });

  pi.on("session_tree", async (_event, ctx) => {
    await restoreQualityState(ctx);
    setQualityStatus(ctx, await evaluateQualityStatus(ctx.cwd));
  });

  pi.on("before_agent_start", async (event, ctx) => {
    applyDeniedTools();
    const snapshot = currentSnapshot ?? await getBoardSnapshot(false);
    currentSnapshot = snapshot;
    const expertPrompt = await renderExpertPrompt(snapshot);
    const status = await evaluateQualityStatus(ctx.cwd);
    setQualityStatus(ctx, status);
    const qualityInstructions = qualityGateState.commands.length > 0
      ? [
          "## Hobot Code quality gate",
          `Current status: ${qualityStatusText(status)}.`,
          `Configured commands: ${qualityGateState.commands.join(" ; ")}.`,
          "After the final change, call quality_gate with action run. Do not state that the task is complete unless its current status is passed.",
        ].join("\n")
      : "## Hobot Code quality gate\nNo commands are configured for this session. Use /init or /gate set to enable completion verification for this project.";
    return {
      systemPrompt: `${event.systemPrompt}\n\n${expertPrompt}\n\n${qualityInstructions}`,
    };
  });

  pi.on("message_end", async (event, ctx) => {
    if (event.message.role !== "assistant") return undefined;
    const toolCalls = event.message.content.filter((block) => block.type === "toolCall");
    const hasMutation = toolCalls.some((block) => mutatingToolNames.has(block.name) || toolIsMcp(block.name));
    if (hasMutation) {
      for (const block of toolCalls) {
        if (block.name === "quality_gate" && block.arguments?.action === "run") {
          qualityGateBlockedCalls.add(block.id);
        }
      }
    }

    if (qualityGateState.commands.length === 0) return undefined;
    const responseText = event.message.content
      .filter((block) => block.type === "text")
      .map((block) => block.text)
      .join("\n");
    if (!completionAssertionPattern.test(responseText)) return undefined;
    const status = await evaluateQualityStatus(ctx.cwd);
    setQualityStatus(ctx, status);
    if (status === "passed") return undefined;
    return {
      message: {
        ...event.message,
        content: [
          ...event.message.content,
          {
            type: "text",
            text: `\n\n[Hobot Code quality gate: completion is not accepted because the gate is ${qualityStatusText(status)}. Run /gate run or call quality_gate after the final change.]`,
          },
        ],
      },
    };
  });

  pi.on("tool_call", async (event, ctx) => {
    const action = toolAction(event.toolName);
    if (action === "deny") {
      return { block: true, reason: `${event.toolName} is denied by ${permissionPolicyPath()}` };
    }
    if (event.toolName === "quality_gate" && qualityGateBlockedCalls.delete(event.toolCallId)) {
      return {
        block: true,
        reason: "Run quality_gate in a separate tool batch after all mutating tools have finished",
      };
    }

    const approvalReasons: string[] = [];
    if (action === "ask") approvalReasons.push("the permission policy requires confirmation");

    if (event.toolName === "write" || event.toolName === "edit") {
      const path = resolve(ctx.cwd, String(event.input.path ?? ""));
      if (criticalVirtualRoots.some((root) => path === root || path.startsWith(`${root}/`))) {
        return { block: true, reason: `Direct writes under ${path} are blocked by the RDK safety policy` };
      }
      if (!path.startsWith(`${resolve(ctx.cwd)}/`) && path !== resolve(ctx.cwd)) {
        approvalReasons.push("the target is outside the current workspace");
      }
    }

    if (event.toolName === "bash") {
      const command = String(event.input.command ?? "");
      if (destructiveCommandPatterns.some((pattern) => pattern.test(command))) {
        approvalReasons.push("the command matches an RDK destructive-operation rule");
      }
    }

    if (approvalReasons.length > 0) {
      if (!ctx.hasUI) {
        return {
          block: true,
          reason: `${event.toolName} requires interactive approval: ${approvalReasons.join("; ")}`,
        };
      }
      const detail = [
        describeToolCall(event.toolName, event.input, qualityGateState.commands),
        `Reason: ${approvalReasons.join("; ")}`,
      ].join("\n");
      const approved = await ctx.ui.confirm(`Allow ${event.toolName}?`, detail);
      if (!approved) return { block: true, reason: `${event.toolName} was cancelled by the user` };
    }

    if (mutatingToolNames.has(event.toolName) || toolIsMcp(event.toolName)) {
      if (qualityGateState.lastRun) {
        qualityGateState = { ...qualityGateState, invalidated: true };
        setQualityStatus(ctx, "stale");
      }
    }
    return undefined;
  });

  pi.registerCommand("init", {
    description: "Initialize AGENTS.md and Hobot Code quality gates for this project",
    handler: async (_args, ctx) => {
      const snapshot = currentSnapshot ?? await getBoardSnapshot(false);
      currentSnapshot = snapshot;
      try {
        const result = await initializeProject(ctx.cwd, snapshot);
        const loaded = await loadQualityConfig(ctx.cwd);
        qualityGateState = { ...loaded.config, source: "project" };
        qualityConfigError = loaded.error;
        persistQualityState();
        setQualityStatus(ctx, await evaluateQualityStatus(ctx.cwd));
        const created = result.created.length > 0 ? result.created.join("\n") : "(none)";
        const preserved = result.preserved.length > 0 ? result.preserved.join("\n") : "(none)";
        ctx.ui.notify(
          `Project initialized.\nCreated:\n${created}\nPreserved unchanged:\n${preserved}\nReloading project context...`,
          "info",
        );
        await ctx.reload();
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      }
    },
  });

  pi.registerCommand("permissions", {
    description: "Inspect or change allow/ask/deny tool permissions",
    handler: async (args, ctx) => {
      const input = String(args ?? "").trim();
      const [operation = "status", first, second] = input.split(/\s+/);
      const previouslyDenied = new Set(
        pi.getAllTools()
          .map((tool) => tool.name)
          .filter((name) => toolAction(name) === "deny"),
      );

      try {
        if (operation === "reload") {
          const loaded = await loadPolicy(permissionPolicyPath());
          permissionPolicy = loaded.policy as PermissionPolicy;
          permissionPolicyError = loaded.error;
        } else if (operation === "set") {
          if (!first || !second) throw new Error("Usage: /permissions set <tool-pattern|mcp:*> <allow|ask|deny>");
          permissionPolicy = await writePolicy(
            permissionPolicyPath(),
            setPolicyRule(permissionPolicy, first, second),
          ) as PermissionPolicy;
          permissionPolicyError = undefined;
        } else if (operation === "default") {
          if (!first) throw new Error("Usage: /permissions default <allow|ask|deny>");
          permissionPolicy = await writePolicy(
            permissionPolicyPath(),
            parsePolicy({ ...permissionPolicy, default: first }),
          ) as PermissionPolicy;
          permissionPolicyError = undefined;
        } else if (operation !== "status") {
          throw new Error("Usage: /permissions [status|reload|set <pattern> <action>|default <action>]");
        }

        if (operation !== "status") {
          const active = new Set(pi.getActiveTools());
          for (const name of previouslyDenied) {
            if (toolAction(name) !== "deny") active.add(name);
          }
          pi.setActiveTools([...active]);
        }
        const hidden = applyDeniedTools();
        const rules = permissionPolicy.rules
          .map((rule) => `${rule.tool}: ${rule.action}`)
          .join("\n");
        ctx.ui.notify([
          `Policy: ${permissionPolicyPath()}`,
          `Default: ${permissionPolicy.default}`,
          permissionPolicyError ? `Fallback: ${permissionPolicyError}` : undefined,
          `Hidden tools: ${hidden.length > 0 ? hidden.join(", ") : "none"}`,
          "Rules:",
          rules || "(none)",
        ].filter(Boolean).join("\n"), permissionPolicyError ? "warning" : "info");
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      }
    },
  });

  pi.registerCommand("gate", {
    description: "Configure, inspect, or run session quality gates",
    handler: async (args, ctx) => {
      const input = String(args ?? "").trim();
      const space = input.indexOf(" ");
      const operation = (space < 0 ? input : input.slice(0, space)) || "status";
      const remainder = space < 0 ? "" : input.slice(space + 1).trim();

      try {
        if (operation === "run") {
          const result = await runQualityGates(ctx.cwd, undefined, ctx);
          ctx.ui.notify(result.text, result.details.passed ? "info" : "warning");
          return;
        }
        if (operation === "reload") {
          const loaded = await loadQualityConfig(ctx.cwd);
          qualityGateState = { ...loaded.config, source: "project" };
          qualityConfigError = loaded.error;
        } else if (operation === "set") {
          if (!remainder) throw new Error("Usage: /gate set <command> or /gate set [\"command 1\",\"command 2\"]");
          const commands = remainder.startsWith("[") ? JSON.parse(remainder) : [remainder];
          const config = parseQualityConfig({ ...qualityGateState, commands });
          qualityGateState = { ...config, source: "session" };
          qualityConfigError = undefined;
        } else if (operation === "add") {
          if (!remainder) throw new Error("Usage: /gate add <command>");
          const config = parseQualityConfig({
            ...qualityGateState,
            commands: [...qualityGateState.commands, remainder],
          });
          qualityGateState = { ...config, source: "session" };
        } else if (operation === "remove") {
          const index = Number(remainder) - 1;
          if (!Number.isInteger(index) || index < 0 || index >= qualityGateState.commands.length) {
            throw new Error("Usage: /gate remove <1-based-command-index>");
          }
          const commands = qualityGateState.commands.filter((_command, commandIndex) => commandIndex !== index);
          qualityGateState = {
            ...parseQualityConfig({ ...qualityGateState, commands }),
            source: "session",
          };
        } else if (operation === "timeout") {
          const seconds = Number(remainder);
          const config = parseQualityConfig({
            ...qualityGateState,
            timeoutMs: Math.round(seconds * 1000),
          });
          qualityGateState = { ...config, source: "session" };
        } else if (operation === "clear") {
          qualityGateState = {
            schemaVersion: 1,
            timeoutMs: qualityGateState.timeoutMs,
            commands: [],
            source: "session",
          };
        } else if (operation !== "status") {
          throw new Error("Usage: /gate [status|run|reload|set|add|remove|timeout|clear]");
        }

        if (!["status", "run"].includes(operation)) {
          qualityGateState = { ...qualityGateState, lastRun: undefined, invalidated: false };
          persistQualityState();
        }
        const status = await evaluateQualityStatus(ctx.cwd);
        setQualityStatus(ctx, status);
        ctx.ui.notify([
          qualityConfigError ? `Config warning: ${qualityConfigError}` : undefined,
          gateReport(qualityGateState, status),
        ].filter(Boolean).join("\n"), qualityConfigError ? "warning" : "info");
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      }
    },
  });

  pi.registerCommand("doctor", {
    description: "Show live D-Robotics board and Hobot Code runtime status",
    handler: async (_args, ctx) => {
      const snapshot = await getBoardSnapshot(true);
      ctx.ui.notify(JSON.stringify(snapshot, null, 2), "info");
    },
  });

  pi.registerCommand("rdk", {
    description: "Show a concise live RDK board summary",
    handler: async (_args, ctx) => {
      ctx.ui.notify(compactBoardSummary(await getBoardSnapshot(false)), "info");
    },
  });

  pi.registerCommand("knowledge", {
    description: "Search the local RDK knowledge pack",
    handler: async (args, ctx) => {
      const query = String(args ?? "").trim();
      if (!query) {
        ctx.ui.notify("Usage: /knowledge <question or keywords>", "warning");
        return;
      }
      const snapshot = currentSnapshot ?? await getBoardSnapshot(false);
      currentSnapshot = snapshot;
      const result = await searchKnowledge({
        query,
        boardId: snapshot.boardId,
        rdkOsVersion: snapshot.rdkOsVersion,
      });
      ctx.ui.notify(JSON.stringify(result, null, 2), "info");
    },
  });

  pi.registerCommand("system-prompt", {
    description: "Show the effective Pi and D-Robotics expert system prompt",
    handler: async (_args, ctx) => {
      const snapshot = currentSnapshot ?? await getBoardSnapshot(false);
      currentSnapshot = snapshot;
      const currentPrompt = ctx.getSystemPrompt();
      const prompt = currentPrompt.includes(EXPERT_PROMPT_MARKER)
        ? currentPrompt
        : `${currentPrompt}\n\n${await renderExpertPrompt(snapshot)}`;
      ctx.ui.notify(prompt, "info");
    },
  });

  for (const alias of ["exit", "q"]) {
    pi.registerCommand(alias, {
      description: "Quit Hobot Code",
      handler: async (_args, ctx) => ctx.shutdown(),
    });
  }
}
