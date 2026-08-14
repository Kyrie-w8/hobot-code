import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { chmod, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  captureGatewayCredential,
  captureGatewayCredentials,
  gatewayCredentialFdEnvironment,
  serializeGatewayCredentials,
  sideAgentCredentialStdio,
  writeSideAgentCredential,
} from "../extensions/rdk/gateway-credential.mjs";

test("ambient D-Robotics credentials are captured and removed from the process environment", () => {
  const env = { ANTHROPIC_AUTH_TOKEN: "ambient-secret" };
  assert.equal(captureGatewayCredential(env), "ambient-secret");
  assert.equal(env.ANTHROPIC_AUTH_TOKEN, undefined);
});

test("managed provider credentials are captured, removed, and serialized without unrelated environment", () => {
  const env = {ANTHROPIC_AUTH_TOKEN: "drobotics-secret", HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret", UNRELATED: "visible"};
  const bundle = captureGatewayCredentials(env);
  assert.deepEqual(bundle, {drobotics: "drobotics-secret", providerKeys: {HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret"}});
  assert.equal(env.ANTHROPIC_AUTH_TOKEN, undefined);
  assert.equal(env.HOBOT_CODE_PROVIDER_KEY_ACME, undefined);
  assert.equal(env.UNRELATED, "visible");
  const serialized = serializeGatewayCredentials(bundle);
  assert.doesNotMatch(serialized, /UNRELATED|visible/);
  assert.deepEqual(JSON.parse(serialized), {schemaVersion: 1, drobotics: "drobotics-secret", providerKeys: {HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret"}});
});

test("extension reloads retain the captured credential without restoring the environment", () => {
  process.env.ANTHROPIC_AUTH_TOKEN = "reload-secret";
  assert.equal(captureGatewayCredential(), "reload-secret");
  assert.equal(process.env.ANTHROPIC_AUTH_TOKEN, undefined);
  assert.equal(captureGatewayCredential(), "reload-secret");
});

test("Side Agent receives the D-Robotics credential through an anonymous descriptor", async () => {
  const modulePath = fileURLToPath(new URL("../extensions/rdk/gateway-credential.mjs", import.meta.url));
  const script = `
    import { captureGatewayCredential } from ${JSON.stringify(modulePath)};
    const token = captureGatewayCredential();
    process.stdout.write(JSON.stringify({ token, ambient: process.env.ANTHROPIC_AUTH_TOKEN, fd: process.env.HOBOT_CODE_GATEWAY_TOKEN_FD }));
  `;
  const token = "pipe-only-secret";
  const child = spawn(process.execPath, ["--input-type=module", "--eval", script], {
    stdio: sideAgentCredentialStdio(token),
    env: { PATH: process.env.PATH, [gatewayCredentialFdEnvironment]: "3" },
  });
  writeSideAgentCredential(child, token);
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => { stdout += chunk; });
  child.stderr.on("data", (chunk) => { stderr += chunk; });
  const code = await new Promise((resolve) => child.on("close", resolve));
  assert.equal(code, 0, stderr);
  assert.deepEqual(JSON.parse(stdout), { token });
});

test("Side Agent receives the complete managed credential bundle through one anonymous descriptor", async () => {
  const modulePath = fileURLToPath(new URL("../extensions/rdk/gateway-credential.mjs", import.meta.url));
  const script = `
    import { captureGatewayCredentials } from ${JSON.stringify(modulePath)};
    process.stdout.write(JSON.stringify(captureGatewayCredentials()));
  `;
  const payload = serializeGatewayCredentials({drobotics: "drobotics-secret", providerKeys: {HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret"}});
  const child = spawn(process.execPath, ["--input-type=module", "--eval", script], {
    stdio: sideAgentCredentialStdio(payload),
    env: {PATH: process.env.PATH, [gatewayCredentialFdEnvironment]: "3"},
  });
  writeSideAgentCredential(child, payload);
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => { stdout += chunk; });
  child.stderr.on("data", (chunk) => { stderr += chunk; });
  const code = await new Promise((resolve) => child.on("close", resolve));
  assert.equal(code, 0, stderr);
  assert.deepEqual(JSON.parse(stdout), {drobotics: "drobotics-secret", providerKeys: {HOBOT_CODE_PROVIDER_KEY_ACME: "acme-secret"}});
});

test("sandbox credential files use the fixed private path and are consumed once", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-credential-file-"));
  const stateRoot = join(root, "state");
  const tokenPath = join(stateRoot, "agentd", "credential", "token");
  await mkdir(join(stateRoot, "agentd", "credential"), { recursive: true, mode: 0o700 });
  await writeFile(tokenPath, "sandbox-secret", { mode: 0o600 });
  const env = {
    HOME: root,
    HOBOT_CODE_STATE_DIR: stateRoot,
    HOBOT_CODE_GATEWAY_TOKEN_FILE: tokenPath,
  };
  assert.equal(captureGatewayCredential(env), "sandbox-secret");
  assert.equal(env.HOBOT_CODE_GATEWAY_TOKEN_FILE, undefined);
  await assert.rejects(() => readFile(tokenPath), /ENOENT/);
});

test("sandbox credential files fail closed for spoofed paths and public permissions", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-credential-invalid-"));
  const stateRoot = join(root, "state");
  const tokenPath = join(stateRoot, "agentd", "credential", "token");
  await mkdir(join(stateRoot, "agentd", "credential"), { recursive: true, mode: 0o700 });
  await writeFile(tokenPath, "sandbox-secret", { mode: 0o600 });
  assert.throws(() => captureGatewayCredential({
    HOME: root,
    HOBOT_CODE_STATE_DIR: stateRoot,
    HOBOT_CODE_GATEWAY_TOKEN_FILE: join(root, "other-token"),
  }), /managed sandbox credential path/);
  await chmod(tokenPath, 0o644);
  assert.throws(() => captureGatewayCredential({
    HOME: root,
    HOBOT_CODE_STATE_DIR: stateRoot,
    HOBOT_CODE_GATEWAY_TOKEN_FILE: tokenPath,
  }), /private regular file/);
});

test("credential helpers keep the normal three-stream layout without a token", () => {
  assert.deepEqual(sideAgentCredentialStdio(""), ["pipe", "pipe", "pipe"]);
});

test("credential bundles reject non-string secrets and malformed maps", () => {
	assert.throws(() => serializeGatewayCredentials({drobotics: {secret: true}, providerKeys: {}}), /invalid/);
	assert.throws(() => serializeGatewayCredentials({drobotics: "", providerKeys: []}), /invalid/);
	assert.throws(() => serializeGatewayCredentials({drobotics: "", providerKeys: {HOBOT_CODE_PROVIDER_KEY_ACME: {secret: true}}}), /invalid/);
});
