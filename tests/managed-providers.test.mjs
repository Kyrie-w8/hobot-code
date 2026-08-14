import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { loadManagedProviders, registerManagedProviders } from "../extensions/rdk/managed-providers.mjs";
import { parseStrictJSON } from "../extensions/rdk/strict-json.mjs";

async function fixture(content, mode = 0o600) {
  const root = await mkdtemp(join(tmpdir(), "hobot-managed-providers-"));
  const agentDir = join(root, "agent");
  await mkdir(agentDir, { recursive: true, mode: 0o700 });
  const path = join(agentDir, "providers.json");
  await writeFile(path, content, { mode });
  return {env: {HOME: root, HOBOT_CODING_AGENT_DIR: agentDir}, path};
}

const configured = JSON.stringify({
  schemaVersion: 1,
  providers: [
    {id: "acme", name: "Acme Gateway", baseUrl: "https://models.example.com/v1", api: "openai-completions", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME", models: [{id: "coder-v2", reasoning: true, input: ["text", "image"], contextWindow: 65536, maxTokens: 4096, compat: {supportsDeveloperRole: false}}]},
    {id: "local", baseUrl: "http://127.0.0.1:8080/v1", api: "anthropic-messages", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_LOCAL", models: [{id: "local-model"}]},
  ],
});

test("managed providers register only with isolated credentials", async () => {
  const {env} = await fixture(configured);
  const catalog = loadManagedProviders(env, {HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret"});
  assert.equal(catalog.providers.length, 1);
  assert.equal(catalog.providers[0].id, "acme");
  assert.equal(catalog.providers[0].apiKey, "acme-secret");
	assert.equal(Object.hasOwn(catalog.providers[0], "authHeader"), false);
  assert.deepEqual(catalog.providers[0].models[0].input, ["text", "image"]);
  assert.deepEqual(catalog.diagnostics, [
    {id: "acme", status: "configured", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME"},
    {id: "local", status: "missing-credential", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_LOCAL"},
  ]);
  const registrations = [];
  registerManagedProviders({registerProvider: (...args) => registrations.push(args)}, {HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret"}, env);
  assert.equal(registrations.length, 1);
  assert.equal(registrations[0][0], "acme");
  assert.equal(registrations[0][1].apiKey, "acme-secret");
});

test("managed providers preserve an explicit auth header choice without inventing one", async () => {
	const {env} = await fixture(JSON.stringify({schemaVersion: 1, providers: [{
		id: "google-proxy", baseUrl: "https://models.example.com/v1", api: "google-generative-ai",
		credentialEnv: "HOBOT_CODE_PROVIDER_KEY_GOOGLE", authHeader: false, models: [{id: "gemini"}],
	}]}));
	const catalog = loadManagedProviders(env, {HOBOT_CODE_PROVIDER_KEY_GOOGLE: "secret"});
	assert.equal(catalog.providers[0].authHeader, false);
});

test("model-only registers only broker-authorized managed providers", async () => {
	const {env} = await fixture(configured);
	const catalog = loadManagedProviders(env, {}, {
		modelEgressSocket: "/private/model.sock",
		modelEgressProviders: new Set(["acme"]),
	});
	assert.equal(catalog.providers.length, 1);
	assert.equal(catalog.providers[0].id, "acme");
	assert.equal(catalog.providers[0].apiKey, "hobot-model-egress");
	assert.equal(catalog.providers[0].modelEgress, true);
	assert.deepEqual(catalog.diagnostics, [
		{id: "acme", status: "configured", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME"},
		{id: "local", status: "missing-credential", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_LOCAL"},
	]);

	const registrations = [];
	const stream = () => "stream";
	registerManagedProviders({registerProvider: (...args) => registrations.push(args)}, {}, env, {
		modelEgressSocket: "/private/model.sock",
		modelEgressProviders: new Set(["acme"]),
		createModelEgressStream: (id, api, socket) => {
			assert.deepEqual([id, api, socket], ["acme", "openai-completions", "/private/model.sock"]);
			return stream;
		},
	});
	assert.equal(registrations.length, 1);
	assert.equal(registrations[0][1].streamSimple, stream);
	assert.throws(
		() => registerManagedProviders({registerProvider() {}}, {}, env, {
			modelEgressSocket: "/private/model.sock",
			modelEgressProviders: new Set(["acme"]),
		}),
		/cannot use the configured model egress broker/,
	);
});

test("model-only keeps Google fail-closed until Pi supports custom fetch", async () => {
	const {env} = await fixture(JSON.stringify({schemaVersion: 1, providers: [{
		id: "google-proxy", baseUrl: "https://models.example.com/v1", api: "google-generative-ai",
		credentialEnv: "HOBOT_CODE_PROVIDER_KEY_GOOGLE", models: [{id: "gemini"}],
	}]}));
	const catalog = loadManagedProviders(env, {}, {
		modelEgressSocket: "/private/model.sock",
		modelEgressProviders: new Set(["google-proxy"]),
	});
	assert.equal(catalog.providers.length, 0);
	assert.equal(catalog.diagnostics[0].status, "missing-credential");
});

test("managed provider config rejects secrets, unsafe URLs, duplicates, and unknown fields", async () => {
  const invalid = [
    {schemaVersion: 1, providers: [{id: "acme", baseUrl: "https://example.com", api: "openai-completions", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME", apiKey: "secret", models: [{id: "one"}]}]},
    {schemaVersion: 1, providers: [{id: "acme", baseUrl: "http://remote.example.com", api: "openai-completions", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME", models: [{id: "one"}]}]},
    {schemaVersion: 1, providers: [{id: "drobotics", baseUrl: "https://example.com", api: "openai-completions", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME", models: [{id: "one"}]}]},
    {schemaVersion: 1, providers: [{id: "acme", baseUrl: "https://example.com", api: "openai-completions", credentialEnv: "OPENAI_API_KEY", models: [{id: "one"}]}]},
    {schemaVersion: 1, providers: [{id: "acme", baseUrl: "https://example.com", api: "openai-completions", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME", models: [{id: "one"}, {id: "one"}]}]},
	{schemaVersion: 1, providers: [{id: "acme", name: {coerced: true}, baseUrl: "https://example.com", api: "openai-completions", credentialEnv: "HOBOT_CODE_PROVIDER_KEY_ACME", models: [{id: "one"}]}]},
  ];
  for (const document of invalid) {
    const {env} = await fixture(JSON.stringify(document));
    assert.throws(() => loadManagedProviders(env, {HOBOT_CODE_PROVIDER_KEY_ACME: "secret"}));
  }
  const duplicate = await fixture('{"schemaVersion":1,"providers":[],"providers":[]}');
  assert.throws(() => loadManagedProviders(duplicate.env, {}), /duplicate key/);
});

test("managed provider config requires private file permissions", async () => {
  const {env, path} = await fixture(configured);
  await chmod(path, 0o644);
  assert.throws(() => loadManagedProviders(env, {HOBOT_CODE_PROVIDER_KEY_ACME: "secret"}), /private regular file/);
});

test("strict JSON rejects duplicate keys at every depth", () => {
  assert.deepEqual(parseStrictJSON('{"outer":{"value":1},"items":[true,null]}'), {outer: {value: 1}, items: [true, null]});
  assert.throws(() => parseStrictJSON('{"outer":{"value":1,"value":2}}'), /duplicate key/);
  assert.throws(() => parseStrictJSON('{"a":1} trailing'), /strict JSON/);
});
