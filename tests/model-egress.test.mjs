import assert from "node:assert/strict";
import { chmod, mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { modelEgressFetch, resolveModelEgressProviders, resolveModelEgressSocket } from "../extensions/rdk/model-egress.mjs";

async function listenUnix(server, path, context) {
	try {
		await new Promise((resolve, reject) => {
			server.once("error", reject);
			server.listen(path, () => {
				server.off("error", reject);
				resolve();
			});
		});
		return true;
	} catch (error) {
		if (error?.code === "EPERM") {
			context.skip("the test sandbox does not allow local Unix socket listeners");
			return false;
		}
		throw error;
	}
}

async function closeServer(server) {
	await new Promise((resolve) => server.close(() => resolve()));
}

test("model egress client uses a private fixed Unix route and streams the response", async (t) => {
	const root = await mkdtemp(join(tmpdir(), "hobot-egress-"));
	const socket = join(root, "s");
	let requestBody = "";
	const server = createServer((request, response) => {
		assert.equal(request.method, "POST");
		assert.equal(request.url, "/v1/providers/drobotics");
		assert.equal(request.headers.authorization, undefined);
		assert.equal(request.headers["anthropic-beta"], "interleaved-thinking-2025-05-14");
		request.setEncoding("utf8");
		request.on("data", (chunk) => { requestBody += chunk; });
		request.on("end", () => {
			response.writeHead(200, { "Content-Type": "text/event-stream", "Request-Id": "safe-id" });
			response.write("event: message_start\ndata: {}\n\n");
			response.end("event: message_stop\ndata: {}\n\n");
		});
	});
	if (!await listenUnix(server, socket, t)) {
		await rm(root, { recursive: true, force: true });
		return;
	}
	await chmod(socket, 0o600);
	t.after(async () => {
		await closeServer(server);
		await rm(root, { recursive: true, force: true });
	});

	assert.equal(resolveModelEgressSocket({ HOBOT_CODE_MODEL_EGRESS_SOCKET: socket }), socket);
	const body = JSON.stringify({ model: "kimi-k3", stream: true, messages: [] });
	const response = await modelEgressFetch(socket, "drobotics", "https://attacker.invalid/ignored", {
		method: "DELETE",
		headers: { Authorization: "Bearer worker-secret", Accept: "text/event-stream, application/json", "Anthropic-Beta": "interleaved-thinking-2025-05-14" },
		body: new TextEncoder().encode(body),
	});
	assert.equal(response.status, 200);
	assert.equal(response.headers.get("request-id"), "safe-id");
	assert.match(await response.text(), /message_start[\s\S]*message_stop/);
	assert.equal(requestBody, body);
});

test("model egress client rejects a public or spoofed socket", async (t) => {
	const root = await mkdtemp(join(tmpdir(), "hobot-egress-public-"));
	const socket = join(root, "s");
	const server = createServer((_request, response) => response.end());
	if (!await listenUnix(server, socket, t)) {
		await rm(root, { recursive: true, force: true });
		return;
	}
	await chmod(socket, 0o666);
	t.after(async () => {
		await closeServer(server);
		await rm(root, { recursive: true, force: true });
	});
	assert.throws(
		() => resolveModelEgressSocket({ HOBOT_CODE_MODEL_EGRESS_SOCKET: socket }),
		/private and owned/,
	);
	assert.throws(
		() => resolveModelEgressSocket({ HOBOT_CODE_MODEL_EGRESS_SOCKET: "relative.sock" }),
		/is invalid/,
	);
	assert.throws(
		() => modelEgressFetch(socket, "../attacker", "https://attacker.invalid", {body: "{}"}),
		/provider is invalid/,
	);
	assert.deepEqual([...resolveModelEgressProviders({HOBOT_CODE_MODEL_EGRESS_PROVIDERS: "acme,drobotics"})], ["acme", "drobotics"]);
	assert.throws(
		() => resolveModelEgressProviders({HOBOT_CODE_MODEL_EGRESS_PROVIDERS: "acme,../attacker"}),
		/is invalid/,
	);
});
