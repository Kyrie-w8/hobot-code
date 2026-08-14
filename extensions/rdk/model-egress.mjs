import { lstatSync } from "node:fs";
import { request as httpRequest } from "node:http";
import { isAbsolute } from "node:path";
import { Readable } from "node:stream";

const SOCKET_ENV = "HOBOT_CODE_MODEL_EGRESS_SOCKET";
const PROVIDERS_ENV = "HOBOT_CODE_MODEL_EGRESS_PROVIDERS";
const PROVIDER_ID = /^[a-z0-9][a-z0-9._-]{0,63}$/u;
const MAX_SOCKET_PATH_BYTES = 100;

export function resolveModelEgressSocket(env = process.env) {
	const path = String(env[SOCKET_ENV] ?? "").trim();
	if (!path) return "";
	if (!isAbsolute(path) || Buffer.byteLength(path) > MAX_SOCKET_PATH_BYTES || path.includes("\0")) {
		throw new Error(`${SOCKET_ENV} is invalid`);
	}
	const info = lstatSync(path);
	const uid = process.getuid?.();
	if (!info.isSocket() || info.isSymbolicLink() || (info.mode & 0o077) !== 0 || (uid !== undefined && info.uid !== uid)) {
		throw new Error("Model egress socket must be private and owned by the current user");
	}
	return path;
}

export function resolveModelEgressProviders(env = process.env) {
	const raw = String(env[PROVIDERS_ENV] ?? "").trim();
	if (!raw) return new Set();
	const providers = raw.split(",");
	if (providers.length > 64 || providers.some((provider) => !PROVIDER_ID.test(provider)) || new Set(providers).size !== providers.length) {
		throw new Error(`${PROVIDERS_ENV} is invalid`);
	}
	return new Set(providers);
}

export function modelEgressProviderEnabled(providerId, env = process.env) {
	return resolveModelEgressProviders(env).has(providerId);
}

export function modelEgressFetch(socketPath, providerId, _input, init = {}) {
	if (!socketPath) throw new Error("Model egress socket is unavailable");
	if (!PROVIDER_ID.test(providerId)) throw new Error("Model egress provider is invalid");
	const body = typeof init.body === "string" || Buffer.isBuffer(init.body)
		? init.body
		: ArrayBuffer.isView(init.body)
			? Buffer.from(init.body.buffer, init.body.byteOffset, init.body.byteLength)
			: init.body instanceof ArrayBuffer
				? Buffer.from(init.body)
				: "";
	if (!body) throw new Error("Model egress request body is required");
	const headers = new Headers(init.headers);
	const accept = headers.get("accept") === "application/json" ? "application/json" : "text/event-stream, application/json";
	const anthropicBeta = headers.get("anthropic-beta");
	return new Promise((resolve, reject) => {
		let settled = false;
		let request;
		const abort = () => request?.destroy(new DOMException("Request was aborted", "AbortError"));
		const cleanup = () => init.signal?.removeEventListener("abort", abort);
		const finishReject = (error) => {
			if (settled) return;
			settled = true;
			cleanup();
			reject(error);
		};
		request = httpRequest({
			socketPath,
			path: `/v1/providers/${providerId}`,
			method: "POST",
			headers: {
				Accept: accept,
				"Content-Type": "application/json",
				"Content-Length": Buffer.byteLength(body),
				...(anthropicBeta && anthropicBeta.length <= 4096 && !/[\r\n]/u.test(anthropicBeta)
					? {"Anthropic-Beta": anthropicBeta}
					: {}),
			},
		}, (response) => {
			if (settled) {
				response.destroy();
				return;
			}
			settled = true;
			cleanup();
			const responseHeaders = new Headers();
			for (const name of ["content-type", "request-id", "cache-control"]) {
				const value = response.headers[name];
				if (typeof value === "string") responseHeaders.set(name, value);
			}
			resolve(new Response(Readable.toWeb(response), {
				status: response.statusCode ?? 502,
				statusText: response.statusMessage,
				headers: responseHeaders,
			}));
		});
		request.on("error", finishReject);
		if (init.signal?.aborted) abort();
		else init.signal?.addEventListener("abort", abort, { once: true });
		request.end(body);
	});
}

export { PROVIDERS_ENV as modelEgressProvidersEnvironment, SOCKET_ENV as modelEgressSocketEnvironment };
