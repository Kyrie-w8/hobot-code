import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";

import { iterateAnthropicSse, readBoundedBody } from "../extensions/rdk/anthropic-sse.mjs";
import {
  DEFAULT_GATEWAY_TIMEOUT_MS,
  normalizeGatewayTimeout,
  resolveGatewayTimeout,
} from "../extensions/rdk/drobotics-config.mjs";
import { convertMessages } from "../extensions/rdk/drobotics-payload.mjs";
import {
  GatewayStreamError,
  IncompleteGatewayStreamError,
  describeGatewayStreamError,
  validateBufferedGatewayResponse,
  validateGatewayContentBlock,
  validateGatewayUsage,
  mapGatewayStopReason,
  shouldRetryBufferedGatewayResponse,
} from "../extensions/rdk/drobotics-response.mjs";
import { toWellFormedText } from "../extensions/rdk/text-safety.mjs";

const STRIP_TYPES_PROGRAM = [
  'import { readFileSync } from "node:fs";',
  'import { stripTypeScriptTypes } from "node:module";',
  'process.stdout.write(stripTypeScriptTypes(readFileSync(0, "utf8"), { mode: "strip" }));',
].join("\n");

function chunkedBody(chunks) {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
}

function sseEvent(event) {
  return `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`;
}

function sseResponse(events, status = 200) {
  return new Response(chunkedBody(events.map(sseEvent)), {
    status,
    headers: { "content-type": "text/event-stream; charset=utf-8" },
  });
}

function jsonResponse(value, status = 200) {
  return new Response(typeof value === "string" ? value : JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function emptyMessageStart(id = "stream-empty", inputTokens = 101) {
  return {
    type: "message_start",
    message: {
      id,
      type: "message",
      role: "assistant",
      content: [],
      stop_reason: null,
      stop_sequence: null,
      usage: {
        input_tokens: inputTokens,
        output_tokens: 0,
        cache_read_input_tokens: 0,
        cache_creation_input_tokens: 0,
      },
    },
  };
}

function bufferedMessage(id = "buffered-complete") {
  return {
    id,
    type: "message",
    role: "assistant",
    content: [
      { type: "thinking", thinking: "check the result", signature: "signed" },
      { type: "text", text: "OK" },
    ],
    stop_reason: "end_turn",
    usage: {
      input_tokens: 11,
      output_tokens: 3,
      cache_read_input_tokens: 2,
      cache_creation_input_tokens: 1,
    },
  };
}

async function createProviderHarness(t) {
  const root = await mkdtemp(join(tmpdir(), "hobot-provider-state-"));
  t.after(() => rm(root, { recursive: true, force: true }));

  const extensionRoot = new URL("../extensions/rdk/", import.meta.url);
  for (const name of [
    "anthropic-sse.mjs",
    "cache-metrics.mjs",
    "drobotics-config.mjs",
    "drobotics-payload.mjs",
    "drobotics-response.mjs",
    "text-safety.mjs",
  ]) {
    await copyFile(new URL(name, extensionRoot), join(root, name));
  }

  const providerSource = await readFile(new URL("drobotics-provider.ts", extensionRoot), "utf8");
  const strippedProvider = execFileSync(
    process.execPath,
    ["--no-warnings", "--input-type=module", "-e", STRIP_TYPES_PROGRAM],
    { input: providerSource, encoding: "utf8", maxBuffer: 16 * 1024 * 1024 },
  );
  await writeFile(
    join(root, "drobotics-provider.mjs"),
    strippedProvider,
  );

  const stubRoot = join(root, "node_modules", "@earendil-works", "pi-ai");
  await mkdir(stubRoot, { recursive: true });
  await writeFile(join(stubRoot, "package.json"), JSON.stringify({
    name: "@earendil-works/pi-ai",
    type: "module",
    exports: "./index.mjs",
  }));
  await writeFile(join(stubRoot, "index.mjs"), `
export function calculateCost() {}

export function createAssistantMessageEventStream() {
  const events = [];
  let ended = false;
  let finish;
  const completed = new Promise((resolve) => { finish = resolve; });
  return {
    events,
    completed,
    push(event) {
      if (ended) throw new Error("event pushed after stream end");
      events.push(event);
    },
    end() {
      if (ended) return;
      ended = true;
      finish();
    },
  };
}
`);

  const provider = await import(`${pathToFileURL(join(root, "drobotics-provider.mjs")).href}?fixture=${Date.now()}`);
  const cacheMetrics = await import(pathToFileURL(join(root, "cache-metrics.mjs")).href);
  return { ...provider, cacheMetrics };
}

function providerModel(id = "kimi-k3") {
  return {
    id,
    name: id,
    api: "drobotics-anthropic",
    provider: "drobotics",
    baseUrl: "https://gateway.invalid",
    reasoning: true,
    input: ["text"],
    contextWindow: 1_000_000,
    maxTokens: 8192,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
  };
}

function providerContext() {
  return {
    systemPrompt: "",
    messages: [{ role: "user", content: "Reply with OK only." }],
    tools: [],
  };
}

function fakeFetchSequence(responses, requests) {
  return async (_input, init) => {
    requests.push({
      accept: new Headers(init.headers).get("accept"),
      body: JSON.parse(init.body),
      aborted: init.signal.aborted,
    });
    const response = responses.shift();
    if (response instanceof Error) throw response;
    if (!response) throw new Error("unexpected extra gateway request");
    return response;
  };
}

async function collectProviderEvents(stream) {
  let timer;
  try {
    await Promise.race([
      stream.completed,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error("provider stream did not end")), 2000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
  return stream.events;
}

function providerOptions(fetch, signal) {
  return {
    apiKey: "test-token",
    fetch,
    maxTokens: 8192,
    reasoning: "off",
    timeoutMs: 1000,
    ...(signal ? { signal } : {}),
  };
}

test("Anthropic SSE parser preserves events across arbitrary chunk boundaries", async () => {
  const body = chunkedBody([
    "event: message_start\r\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\r",
    "\n\r\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,",
    "\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
    ": keepalive\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
  ]);
  const events = [];
  for await (const event of iterateAnthropicSse(body)) events.push(event);
  assert.deepEqual(events.map((event) => event.type), ["message_start", "content_block_delta", "message_stop"]);
  assert.equal(events[1].delta.text, "hello");
});

test("SSE and buffered response limits reject oversized gateway payloads", async () => {
  const oversizedEvent = chunkedBody([`data: ${"x".repeat(64)}`]);
  await assert.rejects(async () => {
    for await (const _event of iterateAnthropicSse(oversizedEvent, { maxEventChars: 32 })) {
      // The generator must reject before yielding.
    }
  }, /exceeds/);

  const response = new Response(chunkedBody(["12345", "67890"]));
  await assert.rejects(() => readBoundedBody(response, 8), /exceeds/);
});

test("gateway readers cancel oversized response bodies", async () => {
  const encoder = new TextEncoder();
  let cancelled = false;
  const body = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode("oversized"));
    },
    cancel() {
      cancelled = true;
    },
  });
  await assert.rejects(() => readBoundedBody(new Response(body), 4), /exceeds/);
  assert.equal(cancelled, true);
});

test("gateway text preserves valid Unicode and replaces only unpaired surrogates", () => {
  assert.equal(toWellFormedText("RDK 😀 𠀀"), "RDK 😀 𠀀");
  assert.equal(toWellFormedText("left\ud800right"), "left\uFFFDright");
  assert.equal(toWellFormedText("left\udc00right"), "left\uFFFDright");
});

test("gateway history replays unsigned thinking as text", () => {
  const history = [{
    role: "assistant",
    content: [
      { type: "thinking", thinking: "unsigned reasoning" },
      { type: "thinking", thinking: "signed reasoning", thinkingSignature: "signature" },
    ],
  }];
  assert.deepEqual(convertMessages(history), [{
    role: "assistant",
    content: [
      { type: "text", text: "unsigned reasoning" },
      { type: "thinking", thinking: "signed reasoning", signature: "signature" },
    ],
  }]);
  assert.deepEqual(convertMessages(history, { allowEmptyThinkingSignature: true })[0].content[0], {
    type: "thinking",
    thinking: "unsigned reasoning",
    signature: "",
  });
});

test("gateway history preserves complete tool-call sequences", () => {
  const history = [
    { role: "assistant", content: [
      { type: "toolCall", id: "tool-1", name: "read", arguments: { path: "README.md" } },
      { type: "toolCall", id: "tool-2", name: "bash", arguments: { command: "pwd" } },
    ] },
    { role: "toolResult", toolCallId: "tool-1", content: [{ type: "text", text: "readme" }], isError: false },
    { role: "toolResult", toolCallId: "tool-2", content: [{ type: "text", text: "/work" }], isError: false },
  ];
  assert.deepEqual(convertMessages(history), [
    { role: "assistant", content: [
      { type: "tool_use", id: "tool-1", name: "read", input: { path: "README.md" } },
      { type: "tool_use", id: "tool-2", name: "bash", input: { command: "pwd" } },
    ] },
    { role: "user", content: [
      { type: "tool_result", tool_use_id: "tool-1", content: "readme", is_error: false },
      { type: "tool_result", tool_use_id: "tool-2", content: "/work", is_error: false },
    ] },
  ]);
});

test("gateway history repairs interrupted and partial tool-call sequences", () => {
  const interrupted = convertMessages([
    { role: "assistant", content: [{ type: "toolCall", id: "tool-1", name: "bash", arguments: { command: "sleep 1" } }] },
    { role: "user", content: "continue" },
  ]);
  assert.deepEqual(interrupted[1], {
    role: "user",
    content: [{
      type: "tool_result",
      tool_use_id: "tool-1",
      content: "Tool execution was interrupted before a result was recorded.",
      is_error: true,
    }],
  });
  assert.deepEqual(interrupted[2], { role: "user", content: "continue" });

  const partial = convertMessages([
    { role: "assistant", content: [
      { type: "toolCall", id: "tool-1", name: "read", arguments: {} },
      { type: "toolCall", id: "tool-2", name: "bash", arguments: {} },
    ] },
    { role: "toolResult", toolCallId: "tool-1", content: [{ type: "text", text: "ok" }], isError: false },
  ]);
  assert.deepEqual(partial[1].content, [
    { type: "tool_result", tool_use_id: "tool-1", content: "ok", is_error: false },
    {
      type: "tool_result",
      tool_use_id: "tool-2",
      content: "Tool execution was interrupted before a result was recorded.",
      is_error: true,
    },
  ]);
});

test("gateway history drops orphan tool results and closes dangling calls", () => {
  assert.deepEqual(convertMessages([
    { role: "toolResult", toolCallId: "orphan", content: [{ type: "text", text: "stale" }], isError: false },
    { role: "user", content: "hello" },
  ]), [{ role: "user", content: "hello" }]);

  const dangling = convertMessages([
    { role: "assistant", content: [{ type: "toolCall", id: "tool-1", name: "read", arguments: {} }] },
  ]);
  assert.equal(dangling.length, 2);
  assert.equal(dangling[1].content[0].tool_use_id, "tool-1");
  assert.equal(dangling[1].content[0].is_error, true);
});

test("buffered gateway responses reject malformed runtime types", () => {
  const valid = {
    id: "msg_1",
    content: [{ type: "text", text: "hello" }],
    stop_reason: "end_turn",
    usage: { input_tokens: 1, output_tokens: 2 },
  };
  assert.equal(validateBufferedGatewayResponse(valid), valid);

  const invalidResponses = [
    { ...valid, content: {} },
    { ...valid, id: 1 },
    { ...valid, content: [{ type: "text", text: 1 }] },
    { ...valid, content: [{ type: "thinking", thinking: 1 }] },
    { ...valid, content: [{ type: "thinking", thinking: "ok", signature: 1 }] },
    { ...valid, content: [{ type: "tool_use", id: 1, name: "read", input: {} }] },
    { ...valid, content: [{ type: "tool_use", id: "tool_1", name: 1, input: {} }] },
    { ...valid, content: [{ type: "tool_use", id: "tool_1", name: "read", input: [] }] },
    { ...valid, usage: { input_tokens: -1 } },
    { ...valid, usage: { input_tokens: 1.5 } },
    { ...valid, usage: { input_tokens: Number.MAX_SAFE_INTEGER + 1 } },
    { ...valid, usage: { input_tokens: Number.MAX_SAFE_INTEGER, output_tokens: 1 } },
    { ...valid, usage: { output_tokens: "2" } },
  ];
  for (const response of invalidResponses) {
    assert.throws(() => validateBufferedGatewayResponse(response), /Model gateway returned invalid/);
  }
});

test("gateway validators reject unknown stream blocks and unsafe usage", () => {
  assert.throws(
    () => validateGatewayContentBlock({ type: "future_block", text: "ignored before" }, "stream content block 0"),
    /unsupported stream content block 0 type/,
  );
  assert.throws(() => validateGatewayUsage(null), /expected a JSON object/);
  assert.throws(() => validateGatewayUsage({ cache_read_input_tokens: Number.POSITIVE_INFINITY }), /safe integer/);
  assert.throws(() => validateGatewayUsage({ cache_creation_input_tokens: Number.NaN }), /safe integer/);
});

test("gateway timeout defaults to the documented 50 minutes and remains bounded", () => {
  assert.equal(DEFAULT_GATEWAY_TIMEOUT_MS, 3_000_000);
  assert.equal(normalizeGatewayTimeout(undefined), 3_000_000);
  assert.equal(normalizeGatewayTimeout("3000000"), 3_000_000);
  assert.equal(normalizeGatewayTimeout("invalid"), 3_000_000);
  assert.equal(normalizeGatewayTimeout(1), 1_000);
  assert.equal(normalizeGatewayTimeout(4_000_000), 3_600_000);
  assert.equal(resolveGatewayTimeout("2500000", 120_000), 2_500_000);
  assert.equal(resolveGatewayTimeout(undefined, 120_000), 120_000);
  assert.equal(resolveGatewayTimeout("invalid", 120_000), 3_000_000);
});

test("gateway stop reasons cover current Anthropic terminal states", () => {
  assert.equal(mapGatewayStopReason("refusal"), "stop");
  assert.equal(mapGatewayStopReason("model_context_window_exceeded"), "length");
  assert.equal(mapGatewayStopReason("future_reason"), "error");
});

test("empty incomplete streams alone may retry through the buffered gateway", () => {
  const incomplete = new IncompleteGatewayStreamError("stream ended early");
  const streamError = new GatewayStreamError("upstream disconnected");
  assert.equal(shouldRetryBufferedGatewayResponse(incomplete, 0), true);
  assert.equal(shouldRetryBufferedGatewayResponse(streamError, 0), true);
  assert.equal(shouldRetryBufferedGatewayResponse(streamError, 1), false);
  assert.equal(shouldRetryBufferedGatewayResponse(incomplete, 1), false);
  assert.equal(shouldRetryBufferedGatewayResponse(incomplete, 0, true), false);
  assert.equal(shouldRetryBufferedGatewayResponse(new Error("malformed event"), 0), false);
});

test("gateway stream errors preserve standard and compatibility messages", () => {
  assert.equal(describeGatewayStreamError({ error: { message: "standard failure" } }), "standard failure");
  assert.equal(describeGatewayStreamError({ error: "string failure" }), "string failure");
  assert.equal(describeGatewayStreamError({ error: { error: { detail: "nested failure" } } }), "nested failure");
  assert.equal(describeGatewayStreamError({ error: null, message: "top-level failure" }), "top-level failure");
  assert.equal(describeGatewayStreamError({ error: null }), "unknown gateway error");
  assert.equal(describeGatewayStreamError({ error: "x".repeat(5000) }).length, 4096);
});

test("D-Robotics provider fallback state machine", async (t) => {
  const { streamDrobotics, cacheMetrics } = await createProviderHarness(t);

  await t.test("empty SSE retries once and emits only the buffered response", async () => {
    cacheMetrics.resetCacheMetrics();
    const requests = [];
    const fetch = fakeFetchSequence([
      sseResponse([emptyMessageStart()]),
      jsonResponse(bufferedMessage()),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch),
    ));

    assert.deepEqual(requests.map((request) => request.body.stream), [true, false]);
    assert.deepEqual(events.map((event) => event.type), [
      "start",
      "thinking_start",
      "thinking_delta",
      "thinking_end",
      "text_start",
      "text_delta",
      "text_end",
      "done",
    ]);
    assert.equal(events.filter((event) => event.type === "start").length, 1);
    assert.equal(events.filter((event) => event.type === "done").length, 1);

    const message = events.at(-1).message;
    assert.equal(message.responseId, "buffered-complete");
    assert.deepEqual(message.content, [
      {
        type: "thinking",
        thinking: "check the result",
        thinkingSignature: "signed",
      },
      { type: "text", text: "OK" },
    ]);
    assert.deepEqual(message.usage, {
      input: 11,
      output: 3,
      cacheRead: 2,
      cacheWrite: 1,
      totalTokens: 17,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
    });
    const metrics = cacheMetrics.getCacheMetrics();
    assert.equal(metrics.requests, 1);
    assert.equal(metrics.cacheRead, 2);
    assert.equal(metrics.cacheWrite, 1);
    assert.equal(metrics.latest.model, "kimi-k3");
  });

  await t.test("a non-standard empty stream error retries once through the buffered gateway", async () => {
    const requests = [];
    const fetch = fakeFetchSequence([
      sseResponse([{ type: "error", error: "transient upstream disconnect" }]),
      jsonResponse(bufferedMessage("buffered-after-stream-error")),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch),
    ));

    assert.deepEqual(requests.map((request) => request.body.stream), [true, false]);
    assert.equal(events.at(-1).type, "done");
    assert.equal(events.at(-1).message.responseId, "buffered-after-stream-error");
  });

  await t.test("empty streaming and buffered responses fail instead of completing silently", async () => {
    const empty = {
      id: "empty-buffered-response",
      type: "message",
      role: "assistant",
      content: [],
      stop_reason: "end_turn",
      usage: { input_tokens: 42, output_tokens: 0, cache_read_input_tokens: 0 },
    };
    const requests = [];
    const fetch = fakeFetchSequence([
      sseResponse([
        emptyMessageStart("empty-streaming-response", 42),
        { type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 0 } },
        { type: "message_stop" },
      ]),
      jsonResponse(empty),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch),
    ));

    assert.deepEqual(requests.map((request) => request.body.stream), [true, false]);
    assert.equal(events.at(-1).type, "error");
    assert.match(events.at(-1).error.errorMessage, /empty successful response/);
  });

  await t.test("partial SSE fails without a buffered retry", async () => {
    const requests = [];
    const fetch = fakeFetchSequence([
      sseResponse([
        emptyMessageStart("stream-partial", 23),
        {
          type: "content_block_start",
          index: 0,
          content_block: { type: "text", text: "" },
        },
        {
          type: "content_block_delta",
          index: 0,
          delta: { type: "text_delta", text: "partial" },
        },
      ]),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch),
    ));

    assert.equal(requests.length, 1);
    assert.deepEqual(events.map((event) => event.type), ["start", "text_start", "text_delta", "error"]);
    const error = events.at(-1).error;
    assert.match(error.errorMessage, /stream ended before message_stop/);
    assert.equal(error.responseId, "stream-partial");
    assert.equal(error.content[0].text, "partial");
  });

  await t.test("a stream error after partial output keeps its message and never retries", async () => {
    const requests = [];
    const fetch = fakeFetchSequence([
      sseResponse([
        emptyMessageStart("stream-error-after-output", 23),
        {
          type: "content_block_start",
          index: 0,
          content_block: { type: "text", text: "partial" },
        },
        { type: "error", error: "upstream failed after output" },
      ]),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch),
    ));

    assert.equal(requests.length, 1);
    assert.equal(events.at(-1).type, "error");
    assert.match(events.at(-1).error.errorMessage, /upstream failed after output/);
    assert.doesNotMatch(events.at(-1).error.errorMessage, /expected a JSON object/);
  });

  await t.test("an aborted request never retries through the buffered gateway", async () => {
    const requests = [];
    const controller = new AbortController();
    controller.abort();
    const fetch = fakeFetchSequence([
      sseResponse([emptyMessageStart("stream-aborted")]),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch, controller.signal),
    ));

    assert.equal(requests.length, 1);
    assert.deepEqual(events.map((event) => event.type), ["start", "error"]);
    assert.equal(events.at(-1).reason, "aborted");
    assert.match(events.at(-1).error.errorMessage, /Request was aborted/);
  });

  await t.test("fallback network and JSON errors preserve both causes and reset stream metadata", async (t) => {
    for (const scenario of [
      {
        name: "network",
        fallback: new Error("fallback offline"),
        secondary: /fallback offline/,
      },
      {
        name: "JSON",
        fallback: jsonResponse("{not-json"),
        secondary: /JSON|Unexpected token|Expected property name/,
      },
    ]) {
      await t.test(scenario.name, async () => {
        const requests = [];
        const fetch = fakeFetchSequence([
          sseResponse([emptyMessageStart(`stream-${scenario.name}`, 71)]),
          scenario.fallback,
        ], requests);

        const events = await collectProviderEvents(streamDrobotics(
          providerModel(),
          providerContext(),
          providerOptions(fetch),
        ));

        assert.equal(requests.length, 2);
        assert.deepEqual(events.map((event) => event.type), ["start", "error"]);
        const error = events.at(-1).error;
        assert.match(error.errorMessage, /stream ended before message_stop/);
        assert.match(error.errorMessage, /buffered fallback/i);
        assert.match(error.errorMessage, scenario.secondary);
        assert.equal(error.responseId, undefined);
        assert.equal(error.rawStopReason, undefined);
        assert.deepEqual(error.usage, {
          input: 0,
          output: 0,
          cacheRead: 0,
          cacheWrite: 0,
          totalTokens: 0,
          cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
        });
      });
    }
  });

  await t.test("an HTTP streaming fallback cannot trigger a third request", async () => {
    const requests = [];
    const fetch = fakeFetchSequence([
      jsonResponse({ error: { type: "unsupported_stream", message: "streaming disabled" } }, 400),
      sseResponse([emptyMessageStart("mislabeled-buffered")]),
    ], requests);

    const events = await collectProviderEvents(streamDrobotics(
      providerModel(),
      providerContext(),
      providerOptions(fetch),
    ));

    assert.equal(requests.length, 2);
    assert.deepEqual(requests.map((request) => request.body.stream), [true, false]);
    assert.deepEqual(events.map((event) => event.type), ["start", "error"]);
  });
});
