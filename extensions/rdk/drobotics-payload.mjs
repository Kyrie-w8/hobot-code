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

  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (message.role === "user") {
      const content = typeof message.content === "string"
        ? toWellFormedText(message.content)
        : convertContentBlocks(message.content);
      converted.push({ role: "user", content });
      continue;
    }

    if (message.role === "assistant") {
      const content = [];
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
        }
      }
      if (content.length > 0) converted.push({ role: "assistant", content });
      continue;
    }

    if (message.role === "toolResult") {
      const results = [];
      let current = message;
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
        current = next;
      }
      converted.push({ role: "user", content: results });
    }
  }

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
