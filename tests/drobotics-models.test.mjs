import assert from "node:assert/strict";
import test from "node:test";

import {
  createDroboticsModelConfig,
  isDeepSeekV4Model,
} from "../extensions/rdk/drobotics-models.mjs";

const options = {
  baseUrl: "https://ai-api.d-robotics.cc/",
  contextWindow: 1_000_000,
  maxTokens: 8192,
};

test("routes only DeepSeek V4 through the cache-capable OpenAI endpoint", () => {
  const kimi = createDroboticsModelConfig("kimi-k3", options);
  const flash = createDroboticsModelConfig("deepseek/deepseek-v4-flash", options);
  const pro = createDroboticsModelConfig("deepseek-v4-pro", {
    ...options,
    baseUrl: "https://ai-api.d-robotics.cc/v1",
  });

  assert.equal(kimi.api, undefined);
  assert.equal(kimi.baseUrl, undefined);
  assert.deepEqual(kimi.input, ["text", "image"]);

  for (const model of [flash, pro]) {
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

test("does not route similarly named models as DeepSeek V4", () => {
  assert.equal(isDeepSeekV4Model("deepseek-v4-flash"), true);
  assert.equal(isDeepSeekV4Model("deepseek/deepseek-v4-flash"), true);
  assert.equal(isDeepSeekV4Model("deepseek-v4-pro"), true);
  assert.equal(isDeepSeekV4Model("deepseek/deepseek-v4-pro"), false);
  assert.equal(isDeepSeekV4Model("deepseek-v4"), false);
  assert.equal(isDeepSeekV4Model("deepseek-v4-flash-preview"), false);
  assert.equal(isDeepSeekV4Model("kimi-k3"), false);
});
