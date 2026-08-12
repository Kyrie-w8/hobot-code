import {
  type Api,
  type AssistantMessage,
  type AssistantMessageEventStream,
  type Context,
  type Model,
  type SimpleStreamOptions,
  type StopReason,
  type TextContent,
  type ThinkingContent,
  type ToolCall,
  calculateCost,
  createAssistantMessageEventStream,
} from "@earendil-works/pi-ai";

import { iterateAnthropicSse, readBoundedBody } from "./anthropic-sse.mjs";
import { recordCacheObservation } from "./cache-metrics.mjs";
import { resolveGatewayTimeout } from "./drobotics-config.mjs";
import { convertMessages, convertTools } from "./drobotics-payload.mjs";
import {
  GatewayStreamError,
  IncompleteGatewayStreamError,
  describeGatewayStreamError,
  requireGatewayObject,
  requireGatewayString,
  requireGatewayStringAlternative,
  mapGatewayStopReason,
  shouldRetryBufferedGatewayResponse,
  validateBufferedGatewayResponse,
  validateGatewayContentBlock,
  validateGatewayUsage,
} from "./drobotics-response.mjs";
import { toWellFormedText } from "./text-safety.mjs";

export const DEFAULT_DROBOTICS_BASE_URL = "https://ai-api.d-robotics.cc";

const MAX_BUFFERED_RESPONSE_BYTES = 8 * 1024 * 1024;
const MAX_REQUEST_BODY_BYTES = 32 * 1024 * 1024;
const MAX_STREAM_BLOCKS = 128;
const MAX_STREAM_CONTENT_CHARS = 16 * 1024 * 1024;
const MAX_STREAM_EVENT_CHARS = 32 * 1024 * 1024;
const MAX_TOOL_ARGUMENT_CHARS = 1024 * 1024;

type JsonRecord = Record<string, unknown>;
type DroboticsStreamOptions = SimpleStreamOptions & {
  fetch?: typeof fetch;
  timeoutMs?: number;
  temperature?: number;
};

interface GatewayUsage {
  input_tokens?: number;
  output_tokens?: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
}

interface GatewayResponse {
  id?: string;
  content: Array<{
    type: "thinking" | "reasoning" | "redacted_thinking" | "text" | "tool_use";
    thinking?: string;
    text?: string;
    signature?: string;
    data?: string;
    id?: string;
    name?: string;
    input?: JsonRecord;
  }>;
  stop_reason: string;
  usage?: GatewayUsage;
}

type StreamingGatewayBlock = (ThinkingContent | TextContent | ToolCall) & {
  providerIndex: number;
  partialJson?: string;
};

function mapStopReason(reason: string | undefined): StopReason {
  return mapGatewayStopReason(reason) as StopReason;
}

function thinkingBudget(
  level: SimpleStreamOptions["reasoning"],
  maxTokens: number,
  customBudgets?: SimpleStreamOptions["thinkingBudgets"],
): number | undefined {
  if (!level || level === "off" || maxTokens < 2048) return undefined;
  const requested: Record<string, number> = {
    minimal: 1024,
    low: 2048,
    medium: 4096,
    high: 6144,
    xhigh: 6144,
    max: 6144,
  };
  const custom = customBudgets?.[level as keyof typeof customBudgets];
  return Math.max(1024, Math.min(custom ?? requested[level] ?? 4096, maxTokens - 1024, Math.floor(maxTokens / 2)));
}

function updateGatewayUsage(output: AssistantMessage, value: unknown, context: string): void {
  const usage = validateGatewayUsage(value, context) as GatewayUsage | undefined;
  if (!usage) return;
  if (usage.input_tokens !== undefined) output.usage.input = usage.input_tokens;
  if (usage.output_tokens !== undefined) output.usage.output = usage.output_tokens;
  if (usage.cache_read_input_tokens !== undefined) output.usage.cacheRead = usage.cache_read_input_tokens;
  if (usage.cache_creation_input_tokens !== undefined) output.usage.cacheWrite = usage.cache_creation_input_tokens;
  const total = output.usage.input + output.usage.output + output.usage.cacheRead + output.usage.cacheWrite;
  if (!Number.isSafeInteger(total)) {
    throw new Error(`Model gateway returned invalid ${context}: token total exceeds the safe integer range`);
  }
  output.usage.totalTokens = total;
}

function resetGatewayOutputForRetry(output: AssistantMessage): void {
  output.content.length = 0;
  delete output.responseId;
  delete output.rawStopReason;
  output.stopReason = "pending";
  output.usage.input = 0;
  output.usage.output = 0;
  output.usage.cacheRead = 0;
  output.usage.cacheWrite = 0;
  output.usage.totalTokens = 0;
  output.usage.cost = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 };
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function consumeBufferedGatewayResponse(
  value: unknown,
  output: AssistantMessage,
  stream: AssistantMessageEventStream,
): void {
  const result = validateBufferedGatewayResponse(value) as GatewayResponse;
  if (result.id) output.responseId = result.id;
  if (result.content.length > MAX_STREAM_BLOCKS) {
    throw new Error(`Model gateway response exceeds ${MAX_STREAM_BLOCKS} content blocks`);
  }
  let contentChars = 0;
  for (const block of result.content) {
    contentChars += (block.thinking ?? block.text ?? block.signature ?? block.data ?? "").length;
    if (contentChars > MAX_STREAM_CONTENT_CHARS) {
      throw new Error(`Model gateway response exceeds ${MAX_STREAM_CONTENT_CHARS} content characters`);
    }
    const contentIndex = output.content.length;
    if (block.type === "thinking" || block.type === "reasoning" || block.type === "redacted_thinking") {
      const thinking = block.type === "redacted_thinking"
        ? "[Reasoning redacted]"
        : block.thinking ?? block.text ?? "";
      output.content.push({
        type: "thinking",
        thinking,
        thinkingSignature: block.signature ?? block.data ?? "",
        ...(block.type === "redacted_thinking" ? { redacted: true } : {}),
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
      const argumentsText = JSON.stringify(block.input ?? {});
      if (argumentsText.length > MAX_TOOL_ARGUMENT_CHARS) {
        throw new Error(`Model gateway tool arguments exceed ${MAX_TOOL_ARGUMENT_CHARS} characters`);
      }
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
  updateGatewayUsage(output, result.usage, "buffered response.usage");
  output.stopReason = mapStopReason(result.stop_reason);
  if (output.stopReason === "error") {
    throw new Error(`Model gateway stopped with unsupported or unsuccessful reason: ${result.stop_reason}`);
  }
  if (output.content.length === 0) {
    throw new Error("Model gateway returned an empty successful response");
  }
}

async function consumeStreamingGatewayResponse(
  response: Response,
  output: AssistantMessage,
  stream: AssistantMessageEventStream,
  signal: AbortSignal | undefined,
): Promise<void> {
  const blocks = output.content as StreamingGatewayBlock[];
  const activeProviderIndexes = new Set<number>();
  const seenProviderIndexes = new Set<number>();
  let contentChars = 0;
  let eventChars = 0;
  let sawMessageStop = false;

  const addContent = (value: string): void => {
    contentChars += value.length;
    if (contentChars > MAX_STREAM_CONTENT_CHARS) {
      throw new Error(`Model gateway stream exceeds ${MAX_STREAM_CONTENT_CHARS} content characters`);
    }
  };

  for await (const rawEvent of iterateAnthropicSse(response.body, { signal })) {
    const event = requireGatewayObject(rawEvent, "stream event") as JsonRecord;
    eventChars += JSON.stringify(event).length;
    if (eventChars > MAX_STREAM_EVENT_CHARS) {
      throw new Error(`Model gateway stream exceeds ${MAX_STREAM_EVENT_CHARS} event characters`);
    }
    const eventType = requireGatewayString(event, "type", "stream event", { required: true, nonEmpty: true });
    if (eventType === "error") {
      throw new GatewayStreamError(`Model gateway stream error: ${describeGatewayStreamError(event)}`);
    }
    if (eventType === "message_start") {
      const message = requireGatewayObject(event.message, "message_start.message") as JsonRecord;
      const responseId = requireGatewayString(message, "id", "message_start.message", { required: true, nonEmpty: true });
      output.responseId = responseId;
      updateGatewayUsage(output, message.usage, "message_start.message.usage");
      continue;
    }
    if (eventType === "content_block_start") {
      if (blocks.length >= MAX_STREAM_BLOCKS) {
        throw new Error(`Model gateway stream exceeds ${MAX_STREAM_BLOCKS} content blocks`);
      }
      const providerIndex = event.index;
      if (typeof providerIndex !== "number" || !Number.isInteger(providerIndex) || providerIndex < 0) {
        throw new Error("Model gateway returned an invalid content block index");
      }
      if (seenProviderIndexes.has(providerIndex)) {
        throw new Error(`Model gateway reused content block index ${providerIndex}`);
      }
      const contentBlock = validateGatewayContentBlock(
        event.content_block,
        `stream content block ${providerIndex}`,
      ) as JsonRecord;
      const blockType = contentBlock.type as string;
      let block: StreamingGatewayBlock | undefined;
      if (blockType === "text") {
        const text = contentBlock.text as string;
        addContent(text);
        block = { type: "text", text, providerIndex };
      } else if (blockType === "thinking" || blockType === "reasoning") {
        const thinking = (contentBlock.thinking ?? contentBlock.text) as string;
        addContent(thinking);
        block = {
          type: "thinking",
          thinking,
          thinkingSignature: (contentBlock.signature ?? "") as string,
          providerIndex,
        };
      } else if (blockType === "redacted_thinking") {
        block = {
          type: "thinking",
          thinking: "[Reasoning redacted]",
          thinkingSignature: (contentBlock.data ?? contentBlock.signature) as string,
          redacted: true,
          providerIndex,
        };
      } else if (blockType === "tool_use") {
        const initialArguments = contentBlock.input as JsonRecord;
        if (JSON.stringify(initialArguments).length > MAX_TOOL_ARGUMENT_CHARS) {
          throw new Error(`Model gateway tool arguments exceed ${MAX_TOOL_ARGUMENT_CHARS} characters`);
        }
        block = {
          type: "toolCall",
          id: contentBlock.id as string,
          name: contentBlock.name as string,
          arguments: initialArguments,
          partialJson: "",
          providerIndex,
        };
      }
      if (!block) throw new Error(`Model gateway returned unsupported stream content block type: ${blockType}`);
      seenProviderIndexes.add(providerIndex);
      activeProviderIndexes.add(providerIndex);
      blocks.push(block);
      const contentIndex = blocks.length - 1;
      if (block.type === "text") stream.push({ type: "text_start", contentIndex, partial: output });
      else if (block.type === "thinking") stream.push({ type: "thinking_start", contentIndex, partial: output });
      else stream.push({ type: "toolcall_start", contentIndex, partial: output });
      continue;
    }
    if (eventType === "content_block_delta") {
      const providerIndex = event.index;
      if (typeof providerIndex !== "number" || !Number.isInteger(providerIndex) || providerIndex < 0) {
        throw new Error("Model gateway returned an invalid content block index");
      }
      const contentIndex = blocks.findIndex((block) => block.providerIndex === providerIndex);
      const block = blocks[contentIndex];
      if (!block) throw new Error(`Model gateway returned a delta for unknown content block ${providerIndex}`);
      const delta = requireGatewayObject(event.delta, `stream content block ${providerIndex} delta`) as JsonRecord;
      const deltaType = requireGatewayString(delta, "type", `stream content block ${providerIndex} delta`, {
        required: true,
        nonEmpty: true,
      });
      if (deltaType === "text_delta") {
        if (block.type !== "text") throw new Error(`Model gateway returned text delta for non-text block ${providerIndex}`);
        const text = requireGatewayString(delta, "text", `stream content block ${providerIndex} delta`, { required: true });
        addContent(text);
        block.text += text;
        if (text) stream.push({ type: "text_delta", contentIndex, delta: text, partial: output });
      } else if (deltaType === "thinking_delta" || deltaType === "reasoning_delta") {
        if (block.type !== "thinking") throw new Error(`Model gateway returned thinking delta for non-thinking block ${providerIndex}`);
        const thinking = requireGatewayStringAlternative(
          delta,
          ["thinking", "text"],
          `stream content block ${providerIndex} delta`,
        );
        addContent(thinking);
        block.thinking += thinking;
        if (thinking) stream.push({ type: "thinking_delta", contentIndex, delta: thinking, partial: output });
      } else if (deltaType === "signature_delta") {
        if (block.type !== "thinking") throw new Error(`Model gateway returned signature delta for non-thinking block ${providerIndex}`);
        const signature = requireGatewayString(delta, "signature", `stream content block ${providerIndex} delta`, { required: true });
        block.thinkingSignature = `${block.thinkingSignature ?? ""}${signature}`;
      } else if (deltaType === "input_json_delta") {
        if (block.type !== "toolCall") throw new Error(`Model gateway returned tool input delta for non-tool block ${providerIndex}`);
        const json = requireGatewayString(delta, "partial_json", `stream content block ${providerIndex} delta`, { required: true });
        block.partialJson = `${block.partialJson ?? ""}${json}`;
        if (block.partialJson.length > MAX_TOOL_ARGUMENT_CHARS) {
          throw new Error(`Model gateway tool arguments exceed ${MAX_TOOL_ARGUMENT_CHARS} characters`);
        }
        if (json) stream.push({ type: "toolcall_delta", contentIndex, delta: json, partial: output });
      } else {
        // Treat future delta variants as metadata; unknown content blocks are still rejected at block start.
      }
      continue;
    }
    if (eventType === "content_block_stop") {
      const providerIndex = event.index;
      if (typeof providerIndex !== "number" || !Number.isInteger(providerIndex) || providerIndex < 0) {
        throw new Error("Model gateway returned an invalid content block index");
      }
      const contentIndex = blocks.findIndex((block) => block.providerIndex === providerIndex);
      const block = blocks[contentIndex];
      if (!block) throw new Error(`Model gateway stopped unknown content block ${providerIndex}`);
      activeProviderIndexes.delete(providerIndex);
      delete block.providerIndex;
      if (block.type === "text") {
        stream.push({ type: "text_end", contentIndex, content: block.text, partial: output });
      } else if (block.type === "thinking") {
        stream.push({ type: "thinking_end", contentIndex, content: block.thinking, partial: output });
      } else {
        if (block.partialJson) {
          try {
            const parsed = JSON.parse(block.partialJson) as unknown;
            if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
              throw new Error("tool arguments must be a JSON object");
            }
            block.arguments = parsed as JsonRecord;
          } catch {
            throw new Error(`Model gateway returned invalid tool arguments for ${block.name}`);
          }
        }
        delete block.partialJson;
        stream.push({ type: "toolcall_end", contentIndex, toolCall: block, partial: output });
      }
      continue;
    }
    if (eventType === "message_delta") {
      const delta = requireGatewayObject(event.delta, "message_delta.delta") as JsonRecord;
      if (delta.stop_reason !== undefined && delta.stop_reason !== null) {
        const rawReason = requireGatewayString(delta, "stop_reason", "message_delta.delta", { required: true });
        output.rawStopReason = rawReason;
        output.stopReason = mapStopReason(rawReason);
      }
      updateGatewayUsage(output, event.usage, "message_delta.usage");
      continue;
    }
    if (eventType === "message_stop") sawMessageStop = true;
  }

  for (const block of blocks) {
    delete block.providerIndex;
    delete block.partialJson;
  }
  if (signal?.aborted) throw new Error("Request was aborted");
  if (!sawMessageStop) throw new IncompleteGatewayStreamError("Model gateway stream ended before message_stop");
  if (activeProviderIndexes.size > 0) {
    throw new IncompleteGatewayStreamError("Model gateway stream ended with incomplete content blocks");
  }
  if (output.stopReason === "pending") {
    throw new IncompleteGatewayStreamError("Model gateway stream ended without a stop reason");
  }
  if (output.stopReason === "error") {
    throw new Error(`Model gateway stopped with unsupported or unsuccessful reason: ${output.rawStopReason ?? "unknown"}`);
  }
  if (output.content.length === 0) {
    throw new IncompleteGatewayStreamError("Model gateway returned an empty successful response");
  }
}

export function streamDrobotics(
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
    const extendedOptions = (options ?? {}) as DroboticsStreamOptions;
    const controller = new AbortController();
    const abortFromParent = () => controller.abort();
    let timedOut = false;
    if (options?.signal?.aborted) controller.abort();
    else options?.signal?.addEventListener("abort", abortFromParent, { once: true });
    const timeoutMs = resolveGatewayTimeout(process.env.API_TIMEOUT_MS, extendedOptions.timeoutMs);
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);

    try {
      if (!extendedOptions.apiKey) throw new Error("ANTHROPIC_AUTH_TOKEN is not configured");
      stream.push({ type: "start", partial: output });

      const requestedMaxTokens = extendedOptions.maxTokens ?? model.maxTokens;
      if (!Number.isInteger(requestedMaxTokens) || requestedMaxTokens < 1) {
        throw new Error("Model gateway maxTokens must be a positive integer");
      }
      const maxTokens = Math.min(requestedMaxTokens, model.maxTokens);
      const budget = thinkingBudget(extendedOptions.reasoning, maxTokens, extendedOptions.thinkingBudgets);
      const deepSeekV4 = /^deepseek-v4-(flash|pro)$/.test(model.id);
      const temperature = extendedOptions.temperature;
      if (temperature !== undefined && (!Number.isFinite(temperature) || temperature < 0 || temperature > 1)) {
        throw new Error("Model gateway temperature must be between 0 and 1");
      }
      const systemPrompt = context.systemPrompt ? toWellFormedText(context.systemPrompt) : undefined;
      const convertedTools = convertTools(context.tools);
      const body: JsonRecord = {
        model: model.id,
        max_tokens: maxTokens,
        stream: true,
        system: systemPrompt,
        messages: convertMessages(context.messages, {
          allowEmptyThinkingSignature: model.compat?.allowEmptySignature === true,
        }),
        tools: convertedTools,
        ...(budget
          ? { thinking: { type: "enabled", budget_tokens: budget } }
          : deepSeekV4 ? { thinking: { type: "disabled" } } : {}),
        ...(temperature === undefined ? {} : { temperature }),
      };

      const endpoint = `${(model.baseUrl || DEFAULT_DROBOTICS_BASE_URL).replace(/\/$/, "")}/v1/messages`;
      const transport = extendedOptions.fetch ?? fetch;
      const request = (payload: JsonRecord, accept: string) => {
        const headers = new Headers({
          Accept: accept,
          Authorization: `Bearer ${extendedOptions.apiKey}`,
          "Content-Type": "application/json",
          "anthropic-version": "2023-06-01",
          "User-Agent": `hobot-code/${process.env.HOBOT_CODE_VERSION || "development"}`,
        });
        for (const [name, value] of Object.entries(extendedOptions.headers ?? {})) {
          if (value === null) headers.delete(name);
          else if (typeof value === "string") headers.set(name, value);
        }
        const serialized = JSON.stringify(payload);
        if (Buffer.byteLength(serialized) > MAX_REQUEST_BODY_BYTES) {
          throw new Error(`Model gateway request exceeds ${MAX_REQUEST_BODY_BYTES} bytes`);
        }
        return transport(endpoint, {
          method: "POST",
          headers,
          body: serialized,
          signal: controller.signal,
        });
      };

      let response = await request(body, "text/event-stream, application/json");
      let streamingFailure: { status: number; detail: string } | undefined;
      let attemptedBuffered = false;
      if (!response.ok) {
        const firstStatus = response.status;
        const firstDetail = (await readBoundedBody(response, 64 * 1024)).slice(0, 4096);
        if ([400, 415, 422].includes(firstStatus)) {
          streamingFailure = { status: firstStatus, detail: firstDetail };
          attemptedBuffered = true;
          response = await request({ ...body, stream: false }, "application/json");
        } else {
          throw new Error(`D-Robotics model gateway HTTP ${firstStatus}: ${firstDetail}`);
        }
      }
      if (!response.ok) {
        const detail = (await readBoundedBody(response, 64 * 1024)).slice(0, 4096);
        throw new Error(`D-Robotics model gateway rejected streaming (${streamingFailure?.status}: ${streamingFailure?.detail}) and buffered fallback (${response.status}: ${detail})`);
      }
      const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
      if (contentType.includes("text/event-stream")) {
        try {
          await consumeStreamingGatewayResponse(response, output, stream, controller.signal);
        } catch (error) {
          if (attemptedBuffered
            || !shouldRetryBufferedGatewayResponse(error, output.content.length, controller.signal.aborted)) throw error;
          attemptedBuffered = true;
          resetGatewayOutputForRetry(output);
          try {
            const fallback = await request({ ...body, stream: false }, "application/json");
            if (!fallback.ok) {
              const detail = (await readBoundedBody(fallback, 64 * 1024)).slice(0, 4096);
              throw new Error(`HTTP ${fallback.status}: ${detail}`);
            }
            const fallbackContentType = fallback.headers.get("content-type")?.toLowerCase() ?? "";
            if (fallbackContentType.includes("text/event-stream")) {
              throw new Error("returned another event stream");
            }
            const text = await readBoundedBody(fallback, MAX_BUFFERED_RESPONSE_BYTES);
            const value = JSON.parse(text) as unknown;
            consumeBufferedGatewayResponse(value, output, stream);
          } catch (fallbackError) {
            throw new Error(`${describeError(error)}; buffered fallback failed: ${describeError(fallbackError)}`);
          }
        }
      } else {
        const text = await readBoundedBody(response, MAX_BUFFERED_RESPONSE_BYTES);
        consumeBufferedGatewayResponse(JSON.parse(text) as unknown, output, stream);
      }
      calculateCost(model, output.usage);
      if (!["stop", "length", "toolUse", "deferred"].includes(output.stopReason)) {
        throw new Error(`Model gateway ended in an invalid state: ${output.stopReason}`);
      }
      recordCacheObservation({
        model: model.id,
        usage: output.usage,
        systemPrompt,
        tools: convertedTools,
      });
      stream.push({ type: "done", reason: output.stopReason as "stop" | "length" | "toolUse" | "deferred", message: output });
      stream.end();
    } catch (error) {
      for (const block of output.content as StreamingGatewayBlock[]) {
        delete block.providerIndex;
        delete block.partialJson;
      }
      output.stopReason = options?.signal?.aborted ? "aborted" : "error";
      output.errorMessage = timedOut
        ? `Model gateway request timed out after ${timeoutMs} ms`
        : error instanceof Error ? error.message : String(error);
      stream.push({ type: "error", reason: output.stopReason, error: output });
      stream.end();
    } finally {
      clearTimeout(timeout);
      options?.signal?.removeEventListener("abort", abortFromParent);
    }
  })();

  return stream;
}
