const DEEPSEEK_V4_PATTERN = /^deepseek-v4-(flash|pro)$/u;

function openAIBaseUrl(baseUrl) {
  const normalized = String(baseUrl).replace(/\/+$/u, "");
  return normalized.endsWith("/v1") ? normalized : `${normalized}/v1`;
}

export function isDeepSeekV4Model(id) {
  return DEEPSEEK_V4_PATTERN.test(id);
}

export function createDroboticsModelConfig(id, { baseUrl, contextWindow, maxTokens }) {
  const deepSeekV4 = isDeepSeekV4Model(id);
  return {
    id,
    name: `${id} (D-Robotics)`,
    ...(deepSeekV4 ? {
      api: "openai-completions",
      baseUrl: openAIBaseUrl(baseUrl),
    } : {}),
    reasoning: true,
    thinkingLevelMap: {
      xhigh: "xhigh",
      max: "max",
    },
    input: deepSeekV4 ? ["text"] : ["text", "image"],
    contextWindow,
    maxTokens,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    ...(deepSeekV4 ? {
      compat: {
        supportsDeveloperRole: false,
        supportsStore: false,
        supportsUsageInStreaming: true,
        maxTokensField: "max_tokens",
        thinkingFormat: "chat-template",
        chatTemplateKwargs: {
          enable_thinking: { $var: "thinking.enabled" },
        },
      },
    } : {}),
  };
}
