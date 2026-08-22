import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  BUILTIN_DROBOTICS_MODELS,
  createDroboticsModelConfig,
  isDeepSeekV4Model,
  supportsDroboticsImageInput,
} from "../extensions/rdk/drobotics-models.mjs";

const options = {
  baseUrl: "https://ai-api.d-robotics.cc/",
  contextWindow: 1_000_000,
  maxTokens: 8192,
};

const expectedModels = [
  "kimi-k3",
  "kimi-k2.6",
  "kimi@latest",
  "qwen3.8-max",
  "qwen3.7-max",
  "qwen-max@latest",
  "glm-5.2",
  "glm-5.3",
  "glm@latest",
  "deepseek/deepseek-v4-flash",
  "deepseek-v4-flash",
  "deepseek-v4-pro",
  "deepseek-flash@latest",
  "deepseek-pro@latest",
];

test("exports the complete ordered D-Robotics gateway catalog", () => {
  assert.deepEqual(BUILTIN_DROBOTICS_MODELS, expectedModels);
  assert.equal(new Set(BUILTIN_DROBOTICS_MODELS).size, BUILTIN_DROBOTICS_MODELS.length);
  assert.equal(Object.isFrozen(BUILTIN_DROBOTICS_MODELS), true);
});

test("packaged Pi settings enable the complete built-in catalog", async () => {
  const settings = JSON.parse(await readFile(
    new URL("../packaging/pi/settings.json", import.meta.url),
    "utf8",
  ));
  assert.deepEqual(
    settings.enabledModels,
    expectedModels.map((id) => `drobotics/${id}`),
  );
});

test("routes DeepSeek gateway models through the cache-capable OpenAI endpoint", () => {
  const kimi = createDroboticsModelConfig("kimi-k3", options);
  const flash = createDroboticsModelConfig("deepseek/deepseek-v4-flash", options);
  const models = [flash, ...[
    "deepseek-v4-flash",
    "deepseek-v4-pro",
    "deepseek-flash@latest",
    "deepseek-pro@latest",
  ].map((id) => createDroboticsModelConfig(id, {
    ...options,
    baseUrl: "https://ai-api.d-robotics.cc/v1",
  }))];

  assert.equal(kimi.api, undefined);
  assert.equal(kimi.baseUrl, undefined);
  assert.deepEqual(kimi.input, ["text", "image"]);

  for (const model of models) {
    assert.equal(model.api, "openai-completions");
    assert.equal(model.baseUrl, "https://ai-api.d-robotics.cc/v1");
    assert.deepEqual(model.input, ["text"]);
    assert.equal(model.compat.supportsUsageInStreaming, true);
    assert.equal(model.compat.maxTokensField, "max_tokens");
    assert.equal(model.compat.thinkingFormat, "chat-template");
    assert.deepEqual(model.compat.chatTemplateKwargs, {
      enable_thinking: { $var: "thinking.enabled" },
    });
  }
});

test("classifies OpenAI-compatible gateway models without name guessing", () => {
  assert.equal(isDeepSeekV4Model("deepseek-v4-flash"), true);
  assert.equal(isDeepSeekV4Model("deepseek/deepseek-v4-flash"), true);
  assert.equal(isDeepSeekV4Model("deepseek-v4-pro"), true);
  assert.equal(isDeepSeekV4Model("deepseek-flash@latest"), true);
  assert.equal(isDeepSeekV4Model("deepseek-pro@latest"), true);
  assert.equal(isDeepSeekV4Model("deepseek/deepseek-v4-pro"), false);
  assert.equal(isDeepSeekV4Model("deepseek-v4"), false);
  assert.equal(isDeepSeekV4Model("deepseek-v4-flash-preview"), false);
  assert.equal(isDeepSeekV4Model("kimi-k3"), false);
});

test("advertises image input only for models verified with the gateway", () => {
  const imageModels = new Set([
    "kimi-k3",
    "kimi-k2.6",
    "kimi@latest",
    "qwen3.8-max",
    "qwen-max@latest",
    "glm-5.2",
  ]);
  for (const id of expectedModels) {
    const expected = imageModels.has(id);
    assert.equal(supportsDroboticsImageInput(id), expected, id);
    assert.deepEqual(
      createDroboticsModelConfig(id, options).input,
      expected ? ["text", "image"] : ["text"],
      id,
    );
  }
  assert.equal(supportsDroboticsImageInput("unknown-model"), false);
});
