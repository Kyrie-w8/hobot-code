import assert from "node:assert/strict";
import test from "node:test";

import { iterateAnthropicSse, readBoundedBody } from "../extensions/rdk/anthropic-sse.mjs";
import {
  DEFAULT_GATEWAY_TIMEOUT_MS,
  normalizeGatewayTimeout,
  resolveGatewayTimeout,
} from "../extensions/rdk/drobotics-config.mjs";
import { convertMessages } from "../extensions/rdk/drobotics-payload.mjs";
import {
  validateBufferedGatewayResponse,
  validateGatewayContentBlock,
  validateGatewayUsage,
  mapGatewayStopReason,
} from "../extensions/rdk/drobotics-response.mjs";
import { toWellFormedText } from "../extensions/rdk/text-safety.mjs";

function chunkedBody(chunks) {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
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
