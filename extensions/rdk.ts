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

const execFileAsync = promisify(execFile);
const DEFAULT_BASE_URL = "https://ai-api.d-robotics.cc";
const DEFAULT_MODEL = "kimi-k3";

type JsonRecord = Record<string, unknown>;

interface BoardSnapshot {
  board: string;
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
  const [board, osRelease, devEntries, thermalZones, somStatus, modelExec] = await Promise.all([
    readText(["/sys/firmware/devicetree/base/model", "/proc/device-tree/model"]),
    readText(["/etc/os-release"]),
    listMatching("/dev", /^(bpu|hobot|ion|dnn)/i),
    readThermals(),
    commandExists(["/usr/bin/hrut_somstatus", "/usr/local/bin/hrut_somstatus"]),
    commandExists(["/usr/bin/hrt_model_exec", "/usr/local/bin/hrt_model_exec"]),
  ]);

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
    hostname: hostname(),
    platform: platform(),
    kernel: release(),
    architecture: process.arch,
    cpuCores: cpus().length,
    memoryTotalMiB: Math.round(totalmem() / 1024 / 1024),
    memoryFreeMiB: Math.round(freemem() / 1024 / 1024),
    loadAverage: loadavg().map((value) => Math.round(value * 100) / 100),
    uptimeSeconds: Math.round(uptime()),
    os: parseOsRelease(osRelease),
    bpuDevices: devEntries.map((name) => `/dev/${name}`),
    thermalZones,
    rdkUtilities: {
      hrut_somstatus: somStatus,
      hrt_model_exec: modelExec,
    },
    ...(processes ? { processes } : {}),
  };
}

function compactBoardSummary(snapshot: BoardSnapshot): string {
  const temperature = snapshot.thermalZones[0]?.celsius;
  return [
    snapshot.board,
    `${snapshot.cpuCores} CPU`,
    `${snapshot.memoryTotalMiB} MiB RAM`,
    temperature === undefined ? undefined : `${temperature} C`,
  ]
    .filter(Boolean)
    .join(" | ");
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
          "User-Agent": "aster-edge/0.5",
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

const criticalVirtualRoots = ["/proc", "/sys", "/dev"];
const destructiveCommandPatterns = [
  /(^|\s)rm\s+[^\n]*(?:-rf|-fr|--recursive)/i,
  /(^|\s)(?:mkfs|fdisk|parted)\b/i,
  /(^|\s)dd\s+[^\n]*\bof=\/dev\//i,
  /(^|\s)(?:reboot|poweroff|halt)\b/i,
  /systemctl\s+(?:reboot|poweroff|halt)/i,
  />\s*\/sys\//i,
];

export default function rdkExtension(pi: ExtensionAPI) {
  const baseUrl = process.env.ANTHROPIC_BASE_URL || DEFAULT_BASE_URL;
  const modelId = process.env.ANTHROPIC_MODEL || DEFAULT_MODEL;

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
      return {
        content: [{ type: "text", text: JSON.stringify(snapshot, null, 2) }],
        details: snapshot,
      };
    },
  });

  pi.on("session_start", async (_event, ctx) => {
    try {
      const snapshot = await getBoardSnapshot(false);
      ctx.ui.setStatus("aster-rdk", compactBoardSummary(snapshot));
    } catch {
      ctx.ui.setStatus("aster-rdk", "RDK status unavailable");
    }
  });

  pi.on("before_agent_start", async (event) => {
    return {
      systemPrompt: `${event.systemPrompt}\n\n## D-Robotics board runtime\nThis agent runs on a D-Robotics embedded board. Use system_snapshot only when live hardware facts are needed. Keep CPU, memory, disk, and thermal load bounded. Do not place an LLM agent in a motor, CAN, GPIO, or other hard real-time control loop.`,
    };
  });

  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName === "write" || event.toolName === "edit") {
      const path = resolve(ctx.cwd, String(event.input.path ?? ""));
      if (criticalVirtualRoots.some((root) => path === root || path.startsWith(`${root}/`))) {
        return { block: true, reason: `Direct writes under ${path} are blocked by the RDK safety policy` };
      }
      if (!path.startsWith(`${resolve(ctx.cwd)}/`) && path !== resolve(ctx.cwd)) {
        if (!ctx.hasUI) return { block: true, reason: "Write outside the working directory requires interactive approval" };
        const approved = await ctx.ui.confirm("Write outside workspace?", path);
        if (!approved) return { block: true, reason: "Write cancelled by user" };
      }
    }

    if (event.toolName === "bash") {
      const command = String(event.input.command ?? "");
      if (!destructiveCommandPatterns.some((pattern) => pattern.test(command))) return undefined;
      if (!ctx.hasUI) return { block: true, reason: "Destructive command requires interactive approval" };
      const approved = await ctx.ui.confirm("Run destructive command?", command);
      if (!approved) return { block: true, reason: "Command cancelled by user" };
    }
    return undefined;
  });

  pi.registerCommand("doctor", {
    description: "Show live D-Robotics board and Aster runtime status",
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

  for (const alias of ["exit", "q"]) {
    pi.registerCommand(alias, {
      description: "Quit Aster",
      handler: async (_args, ctx) => ctx.shutdown(),
    });
  }
}
