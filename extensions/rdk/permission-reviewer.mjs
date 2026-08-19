import { createHash } from "node:crypto";
import { lstatSync } from "node:fs";
import { createConnection } from "node:net";
import { isAbsolute } from "node:path";

export const AUTO_REVIEW_MODE = "auto-review";
export const REVIEWER_VERSION = 2;
export const REVIEWER_DENIAL_LIMIT = 3;
export const REVIEWER_WINDOW_LIMIT = 10;
export const REVIEWER_WINDOW_MS = 10 * 60_000;

const CONTROL_SOCKET_ENV = "HOBOT_CODE_TASK_CONTROL_SOCKET";
const TASK_ID_ENV = "HOBOT_CODE_TASK_ID";
const TASK_ID = /^[0-9a-f]{24}$/u;
const TOOL_NAME = /^[A-Za-z0-9_.:-]{1,80}$/u;
const MAX_SOCKET_BYTES = 100;
const MAX_RESPONSE_BYTES = 64 * 1024;
const REVIEW_TIMEOUT_MS = 35_000;

// These are product invariants, not a growing command denylist. Everything
// else, including network, SSH, packages, services, processes and hardware,
// is judged by the independent approval model in agentd.
const CREDENTIAL_OR_ACCESS_PATH = /(?:^|[\s'"=:/])(?:\.ssh|\.gnupg|\.aws|\.kube|authorized_keys|shadow|sudoers)(?:[\s'"/:]|$)/iu;
const REVIEWER_CONTROL_PATH = /(?:hobot-code.*(?:permissions\.json|approval-review-audit|task-control|agentd\.sock|model-egress)|HOBOT_CODE_(?:TASK_CONTROL_SOCKET|PERMISSION_POLICY|MODEL_EGRESS_SOCKET))/iu;
const REVIEWER_CONTROL_MUTATION = /(?:^|[;&|]\s*)(?:sudo\s+)?(?:rm|mv|cp|ln|chmod|chown|chgrp|setfacl|tee|truncate|shred|sed\s+-i)\b[^\n;&|]*(?:hobot-code|HOBOT_CODE_(?:TASK_CONTROL_SOCKET|PERMISSION_POLICY|MODEL_EGRESS_SOCKET))/iu;
const BROAD_DESTRUCTION = /(?:^|[;&|]\s*)(?:sudo\s+)?(?:rm\s+(?:-[A-Za-z]*r[A-Za-z]*f[A-Za-z]*|-[A-Za-z]*f[A-Za-z]*r[A-Za-z]*)\s+(?:\/|~|\$HOME)(?:\s|$)|mkfs(?:\.[A-Za-z0-9]+)?\s+\/dev\/|wipefs\b|dd\b[^;&|\n]*\bof=\/dev\/)/iu;
const SECURITY_ACCESS_PATH = /(?:sshd_config|authorized_keys|\/etc\/(?:shadow|sudoers))/iu;
const SECURITY_WEAKENING_COMMAND = /(?:^|[;&|]\s*)(?:sudo\s+)?(?:(?:iptables|ip6tables)\s+-F\b|nft\s+flush\s+ruleset\b|(?:systemctl|service)\s+(?:disable|stop)\s+(?:auditd|firewalld|ufw|sshd?)\b)/iu;
const SECRET_EGRESS_CLIENT = /(?:^|[;&|]\s*)(?:sudo\s+)?(?:curl|wget|nc|ncat|ssh|scp|sftp)\b/iu;
const SECRET_REFERENCE = /(?:\$(?:\{)?[A-Za-z_][A-Za-z0-9_]*(?:API_KEY|AUTH_TOKEN|ACCESS_TOKEN|TOKEN|SECRET|PASSWORD)[A-Za-z0-9_]*(?:\})?|\bBearer\s+[A-Za-z0-9._~+/-]{12,}|sk-[A-Za-z0-9_-]{12,}|\.ssh\/|\.gnupg\/|authorized_keys)/iu;
const HUMAN_IMPACT_REASONS = new Set([
  "removes or destroys files",
  "deletes files through find",
  "terminates running processes",
  "changes or stops a system service",
  "changes system service configuration or process state",
  "stops or reboots the board",
  "performs a destructive or forceful Git operation",
  "performs a privileged or destructive container operation",
  "changes cluster state or executes inside a workload",
  "deletes Hobot Code task state",
  "deletes Hobot Code schedule state",
  "removes or replaces Hobot Code persistent task and conversation state",
  "changes a filesystem or partition table",
  "writes directly to a block or device node",
]);

function stableJson(value) {
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  if (value && typeof value === "object") return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableJson(value[key])}`).join(",")}}`;
  return JSON.stringify(value) ?? "null";
}

export function actionFingerprint(tool, input) {
  return createHash("sha256").update(`${String(tool).toLowerCase()}\0${stableJson(input ?? {})}`).digest("hex");
}

export function hardPermissionReviewBoundary(tool, input, facts = {}) {
  const target = String(facts.target ?? input?.path ?? "");
  const command = String(input?.command ?? "");
  const reasons = [];
  if (((tool === "write" || tool === "edit") && REVIEWER_CONTROL_PATH.test(target)) || (tool === "bash" && REVIEWER_CONTROL_MUTATION.test(command))) reasons.push("the action tampers with Hobot Code approval or control infrastructure");
  if (BROAD_DESTRUCTION.test(command)) reasons.push("the action performs broad irreversible destruction");
  if (((tool === "write" || tool === "edit") && SECURITY_ACCESS_PATH.test(target)) || (tool === "bash" && SECURITY_WEAKENING_COMMAND.test(command))) reasons.push("the action changes persistent access or disables a security control");
  if (SECRET_EGRESS_CLIENT.test(command) && SECRET_REFERENCE.test(command)) reasons.push("the action may transmit credentials or secrets");
  if ((tool === "write" || tool === "edit") && CREDENTIAL_OR_ACCESS_PATH.test(target)) reasons.push("the action writes authentication, credential, or privileged access configuration");
  const destructiveReasons = Array.isArray(facts.destructiveReasons) ? facts.destructiveReasons : [];
  for (const reason of destructiveReasons) {
    if (HUMAN_IMPACT_REASONS.has(String(reason))) reasons.push(`the action ${reason}`);
  }
  return reasons;
}

function taskControlEnvironment(env = process.env) {
  const path = String(env[CONTROL_SOCKET_ENV] ?? "").trim();
  const taskId = String(env[TASK_ID_ENV] ?? "").trim();
  if (!path || !taskId) throw new Error("task approval control is unavailable");
  if (!isAbsolute(path) || Buffer.byteLength(path) > MAX_SOCKET_BYTES || path.includes("\0") || !TASK_ID.test(taskId)) {
    throw new Error("task approval control environment is invalid");
  }
  const info = lstatSync(path);
  const uid = process.getuid?.();
  if (!info.isSocket() || info.isSymbolicLink() || (info.mode & 0o077) !== 0 || (uid !== undefined && info.uid !== uid)) {
    throw new Error("task approval control socket must be private and owned by the current user");
  }
  return { path, taskId };
}

export function requestPermissionModelReview(request, { env = process.env, timeoutMs = REVIEW_TIMEOUT_MS } = {}) {
  const { path } = taskControlEnvironment(env);
  const tool = String(request?.tool ?? "");
  const input = request?.input && typeof request.input === "object" ? request.input : {};
  if (!TOOL_NAME.test(tool)) return Promise.reject(new Error("permission review tool is invalid"));
  const fingerprint = actionFingerprint(tool, input);
  const id = `permission-review-${Date.now()}-${process.pid}`;
  const envelope = `${JSON.stringify({
    protocol: 1, id, method: "permission.review",
    params: { tool, input, facts: request?.facts ?? {}, reasons: request?.reasons ?? [], fingerprint },
  })}\n`;
  return new Promise((resolve, reject) => {
    let settled = false;
    let received = Buffer.alloc(0);
    const socket = createConnection({ path });
    const timer = setTimeout(() => finish(new Error("approval model timed out")), timeoutMs);
    const finish = (error, value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      socket.destroy();
      if (error) reject(error); else resolve(value);
    };
    // Pi's Bun runtime may treat end(payload) as a full close for Unix
    // sockets. A newline terminates the request, so keep the read side open
    // until agentd returns the decision and finish() destroys the socket.
    socket.on("connect", () => socket.write(envelope));
    socket.on("data", (chunk) => {
      received = Buffer.concat([received, chunk]);
      if (received.length > MAX_RESPONSE_BYTES) return finish(new Error("approval model response is too large"));
      const newline = received.indexOf(10);
      if (newline < 0) return;
      try {
        const response = JSON.parse(received.subarray(0, newline).toString("utf8"));
        if (response.protocol !== 1 || response.id !== id) throw new Error("approval model returned an invalid response envelope");
        if (!response.ok) throw new Error(`${response.error?.code ?? "reviewer_failed"}: ${response.error?.message ?? "approval model failed"}`);
        const result = response.result;
        if (!result || !["approved", "manual-required", "denied"].includes(result.status) || result.fingerprint !== fingerprint
          || !Array.isArray(result.reasons) || result.reasons.some((reason) => typeof reason !== "string")) {
          throw new Error("approval model returned an invalid decision");
        }
        finish(undefined, result);
      } catch (error) { finish(error); }
    });
    socket.on("error", (error) => finish(error));
    socket.on("end", () => { if (!settled) finish(new Error("approval model closed without a decision")); });
  });
}

export function createPermissionReviewer({ now = () => Date.now(), review = requestPermissionModelReview } = {}) {
  const denials = [];
  const exactRetries = new Set();
  let consecutiveDenials = 0;
  const trim = () => {
    const cutoff = now() - REVIEWER_WINDOW_MS;
    while (denials.length && denials[0].at < cutoff) denials.shift();
    if (denials.length === 0) consecutiveDenials = 0;
  };
  return {
    requestExactRetry(fingerprint) {
      if (!fingerprint || !denials.some((entry) => entry.fingerprint === fingerprint) || exactRetries.has(fingerprint)) return false;
      exactRetries.add(fingerprint);
      return true;
    },
    recordDenial(fingerprint) { trim(); denials.push({ fingerprint, at: now() }); consecutiveDenials += 1; },
    recordNonDenial() { consecutiveDenials = 0; },
    async review(request) {
      const tool = String(request?.tool ?? "");
      const input = request?.input && typeof request.input === "object" ? request.input : {};
      const fingerprint = actionFingerprint(tool, input);
      const retry = exactRetries.delete(fingerprint);
      const hardReasons = hardPermissionReviewBoundary(tool, input, request?.facts ?? {});
      if (hardReasons.length > 0) {
        return { status: "manual-required", source: "hard-safety-boundary", fingerprint, retry, reasons: hardReasons, hard: true };
      }
      trim();
      if ((consecutiveDenials >= REVIEWER_DENIAL_LIMIT || denials.length >= REVIEWER_WINDOW_LIMIT) && !retry) {
        return { status: "denied", source: "approval-model-circuit", fingerprint, reasons: ["approval review paused after repeated denials; choose a materially safer action or approve manually"], hard: false };
      }
      return review({ ...request, tool, input, fingerprint });
    },
  };
}
