import assert from "node:assert/strict";
import test from "node:test";

import { iterateAnthropicSse, readBoundedBody } from "../extensions/rdk/anthropic-sse.mjs";

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
