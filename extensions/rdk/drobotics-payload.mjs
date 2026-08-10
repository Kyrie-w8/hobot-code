import { toWellFormedText } from "./text-safety.mjs";

export function convertContentBlocks(content) {
  const blocks = content.map((block) => {
    if (block.type === "text") return { type: "text", text: toWellFormedText(block.text) };
    return {
      type: "image",
      source: { type: "base64", media_type: block.mimeType, data: block.data },
    };
  });
  return blocks.length === 1 && blocks[0]?.type === "text" ? blocks[0].text : blocks;
}

export function convertMessages(messages, options = {}) {
  const converted = [];
  const allowEmptyThinkingSignature = options.allowEmptyThinkingSignature === true;
  let pendingToolCallIds = [];

  const flushInterruptedToolCalls = () => {
    if (pendingToolCallIds.length === 0) return;
    converted.push({
      role: "user",
      content: pendingToolCallIds.map((toolCallId) => ({
        type: "tool_result",
        tool_use_id: toolCallId,
        content: "Tool execution was interrupted before a result was recorded.",
        is_error: true,
      })),
    });
    pendingToolCallIds = [];
  };

  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (message.role === "user") {
      flushInterruptedToolCalls();
      const content = typeof message.content === "string"
        ? toWellFormedText(message.content)
        : convertContentBlocks(message.content);
      converted.push({ role: "user", content });
      continue;
    }

    if (message.role === "assistant") {
      flushInterruptedToolCalls();
      const content = [];
      const toolCallIds = [];
      for (const block of message.content) {
        if (block.type === "text" && block.text) {
          content.push({ type: "text", text: toWellFormedText(block.text) });
        } else if (block.type === "thinking" && block.thinking) {
          const thinking = toWellFormedText(block.thinking);
          if (block.thinkingSignature || allowEmptyThinkingSignature) {
            content.push({
              type: "thinking",
              thinking,
              signature: block.thinkingSignature ?? "",
            });
          } else {
            content.push({ type: "text", text: thinking });
          }
        } else if (block.type === "toolCall") {
          content.push({ type: "tool_use", id: block.id, name: block.name, input: block.arguments });
          toolCallIds.push(block.id);
        }
      }
      if (content.length > 0) converted.push({ role: "assistant", content });
      pendingToolCallIds = toolCallIds;
      continue;
    }

    if (message.role === "toolResult") {
      const results = [];
      const seenToolCallIds = new Set();
      let current = message;
      while (true) {
        if (pendingToolCallIds.includes(current.toolCallId) && !seenToolCallIds.has(current.toolCallId)) {
          results.push({
            type: "tool_result",
            tool_use_id: current.toolCallId,
            content: convertContentBlocks(current.content),
            is_error: current.isError,
          });
          seenToolCallIds.add(current.toolCallId);
        }
        const next = messages[index + 1];
        if (!next || next.role !== "toolResult") break;
        index += 1;
        current = next;
      }
      if (pendingToolCallIds.length === 0) continue;
      for (const toolCallId of pendingToolCallIds) {
        if (seenToolCallIds.has(toolCallId)) continue;
        results.push({
          type: "tool_result",
          tool_use_id: toolCallId,
          content: "Tool execution was interrupted before a result was recorded.",
          is_error: true,
        });
      }
      converted.push({ role: "user", content: results });
      pendingToolCallIds = [];
    }
  }

  flushInterruptedToolCalls();

  return converted;
}

export function convertTools(tools) {
  if (!tools?.length) return undefined;
  return tools.map((tool) => ({
    name: tool.name,
    description: tool.description,
    input_schema: tool.parameters,
  }));
}
