const TOKEN_FIELDS = [
  "input_tokens",
  "output_tokens",
  "cache_read_input_tokens",
  "cache_creation_input_tokens",
];

export function mapGatewayStopReason(reason) {
  switch (reason) {
    case "end_turn":
    case "stop_sequence":
    case "refusal":
      return "stop";
    case "max_tokens":
    case "model_context_window_exceeded":
      return "length";
    case "tool_use":
      return "toolUse";
    case "pause_turn":
      return "deferred";
    default:
      return "error";
  }
}

export function requireGatewayObject(value, context) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`Model gateway returned invalid ${context}: expected a JSON object`);
  }
  return value;
}

export function requireGatewayString(record, field, context, options = {}) {
  const value = record[field];
  if (value === undefined) {
    if (options.required) {
      throw new Error(`Model gateway returned invalid ${context}.${field}: expected a string`);
    }
    return undefined;
  }
  if (typeof value !== "string" || (options.nonEmpty && value.length === 0)) {
    throw new Error(`Model gateway returned invalid ${context}.${field}: expected ${options.nonEmpty ? "a non-empty string" : "a string"}`);
  }
  return value;
}

export function requireGatewayStringAlternative(record, fields, context) {
  let selected;
  for (const field of fields) {
    const value = requireGatewayString(record, field, context);
    if (selected === undefined && value !== undefined) selected = value;
  }
  if (selected === undefined) {
    throw new Error(`Model gateway returned invalid ${context}: expected string field ${fields.join(" or ")}`);
  }
  return selected;
}

export function validateGatewayUsage(value, context = "usage") {
  if (value === undefined) return undefined;
  const usage = requireGatewayObject(value, context);
  let total = 0;
  for (const field of TOKEN_FIELDS) {
    if (!(field in usage)) continue;
    const count = usage[field];
    if (typeof count !== "number" || !Number.isSafeInteger(count) || count < 0) {
      throw new Error(`Model gateway returned invalid ${context}.${field}: expected a non-negative safe integer`);
    }
    total += count;
  }
  if (!Number.isSafeInteger(total)) {
    throw new Error(`Model gateway returned invalid ${context}: token total exceeds the safe integer range`);
  }
  return usage;
}

export function validateGatewayContentBlock(value, context = "content block") {
  const block = requireGatewayObject(value, context);
  const type = requireGatewayString(block, "type", context, { required: true, nonEmpty: true });

  for (const field of ["text", "thinking", "signature", "data", "id", "name"]) {
    requireGatewayString(block, field, context);
  }

  if (type === "text") {
    requireGatewayString(block, "text", context, { required: true });
  } else if (type === "thinking" || type === "reasoning") {
    requireGatewayStringAlternative(block, ["thinking", "text"], context);
  } else if (type === "redacted_thinking") {
    requireGatewayStringAlternative(block, ["data", "signature"], context);
  } else if (type === "tool_use") {
    requireGatewayString(block, "id", context, { required: true, nonEmpty: true });
    requireGatewayString(block, "name", context, { required: true, nonEmpty: true });
    requireGatewayObject(block.input, `${context}.input`);
  } else {
    throw new Error(`Model gateway returned unsupported ${context} type: ${type}`);
  }
  return block;
}

export function validateBufferedGatewayResponse(value) {
  const response = requireGatewayObject(value, "buffered response");
  requireGatewayString(response, "id", "buffered response");
  requireGatewayString(response, "stop_reason", "buffered response", { required: true, nonEmpty: true });
  if (!Array.isArray(response.content)) {
    throw new Error("Model gateway returned invalid buffered response.content: expected an array");
  }
  response.content.forEach((block, index) => {
    validateGatewayContentBlock(block, `buffered response.content[${index}]`);
  });
  validateGatewayUsage(response.usage, "buffered response.usage");
  return response;
}
