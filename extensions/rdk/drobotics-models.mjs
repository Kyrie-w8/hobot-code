export const DROBOTICS_MODEL_CATALOG = Object.freeze([
  { id: "kimi-k3", imageInput: true },
  { id: "kimi-k2.6", imageInput: true },
  { id: "kimi@latest", imageInput: true },
  { id: "qwen3.8-max", imageInput: true },
  { id: "qwen3.7-max", imageInput: false },
  { id: "qwen-max@latest", imageInput: true },
  { id: "glm-5.2", imageInput: true },
  { id: "glm-5.3", imageInput: false },
  { id: "glm@latest", imageInput: false },
  // Keep the historical model-group ID for existing sessions and configs.
  { id: "deepseek/deepseek-v4-flash", imageInput: false },
  { id: "deepseek-v4-flash", imageInput: false },
  { id: "deepseek-v4-pro", imageInput: false },
  { id: "deepseek-flash@latest", imageInput: false },
  { id: "deepseek-pro@latest", imageInput: false },
].map(Object.freeze));

export const BUILTIN_DROBOTICS_MODELS = Object.freeze(
  DROBOTICS_MODEL_CATALOG.map(({ id }) => id),
);

const MODEL_CAPABILITIES = new Map(
  DROBOTICS_MODEL_CATALOG.map(({ id, imageInput }) => [id, { imageInput }]),
);

const OPENAI_COMPATIBLE_MODELS = new Set([
  "deepseek/deepseek-v4-flash",
  "deepseek-v4-flash",
  "deepseek-v4-pro",
  "deepseek-flash@latest",
  "deepseek-pro@latest",
]);

function openAIBaseUrl(baseUrl) {
  const normalized = String(baseUrl).replace(/\/+$/u, "");
  return normalized.endsWith("/v1") ? normalized : `${normalized}/v1`;
}

export function isDeepSeekV4Model(id) {
  return OPENAI_COMPATIBLE_MODELS.has(id);
}

export function supportsDroboticsImageInput(id) {
  return MODEL_CAPABILITIES.get(id)?.imageInput === true;
}

export function createDroboticsModelConfig(id, { baseUrl, contextWindow, maxTokens }) {
  const openAICompatible = isDeepSeekV4Model(id);
  return {
    id,
    name: `${id} (D-Robotics)`,
    ...(openAICompatible ? {
      api: "openai-completions",
      baseUrl: openAIBaseUrl(baseUrl),
    } : {}),
    reasoning: true,
    thinkingLevelMap: {
      xhigh: "xhigh",
      max: "max",
    },
    input: supportsDroboticsImageInput(id) ? ["text", "image"] : ["text"],
    contextWindow,
    maxTokens,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    ...(openAICompatible ? {
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
