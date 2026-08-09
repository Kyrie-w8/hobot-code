const DEFAULT_MAX_EVENT_CHARS = 2_000_000;

function decodeEvent(raw) {
  let eventName = "message";
  const data = [];
  for (const line of raw.split("\n")) {
    if (!line || line.startsWith(":")) continue;
    const separator = line.indexOf(":");
    const field = separator < 0 ? line : line.slice(0, separator);
    let value = separator < 0 ? "" : line.slice(separator + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "event") eventName = value;
    else if (field === "data") data.push(value);
  }
  const payload = data.join("\n");
  if (!payload || payload === "[DONE]") return undefined;
  const parsed = JSON.parse(payload);
  if (parsed && typeof parsed === "object" && !parsed.type && eventName !== "message") {
    parsed.type = eventName;
  }
  return parsed;
}

export async function* iterateAnthropicSse(body, options = {}) {
  if (!body) throw new Error("Model gateway returned an empty streaming body");
  const maxEventChars = options.maxEventChars ?? DEFAULT_MAX_EVENT_CHARS;
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    while (true) {
      if (options.signal?.aborted) throw new Error("Request was aborted");
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      buffer = buffer.replace(/\r\n/g, "\n");
      if (buffer.length > maxEventChars && !buffer.includes("\n\n")) {
        throw new Error(`Model gateway SSE event exceeds ${maxEventChars} characters`);
      }
      let boundary;
      while ((boundary = buffer.indexOf("\n\n")) >= 0) {
        const raw = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        if (raw.length > maxEventChars) throw new Error(`Model gateway SSE event exceeds ${maxEventChars} characters`);
        const event = decodeEvent(raw);
        if (event) yield event;
      }
      if (done) break;
    }
    if (buffer.trim()) {
      const event = decodeEvent(buffer);
      if (event) yield event;
    }
  } finally {
    reader.releaseLock();
  }
}

export async function readBoundedBody(response, maxBytes) {
  if (!response.body) return "";
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let total = 0;
  let text = "";
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > maxBytes) throw new Error(`Model gateway response exceeds ${maxBytes} bytes`);
      text += decoder.decode(value, { stream: true });
    }
    text += decoder.decode();
    return text;
  } finally {
    reader.releaseLock();
  }
}
