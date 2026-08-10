import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { appendFileSync, chmodSync, mkdirSync, renameSync, rmSync, statSync } from "node:fs";
import { dirname } from "node:path";

import { redactSensitiveText } from "./control-plane.mjs";
import { sanitizedChildEnv, terminateProcessGroup } from "./runtime-safety.mjs";

export type HookEvent = "PreToolUse" | "PostToolUse";
export type HookFailurePolicy = "block" | "warn";

export interface HookDefinition {
  name: string;
  event: HookEvent;
  tool: string;
  command: string[];
  timeoutMs?: number;
  failurePolicy?: HookFailurePolicy;
}

export interface HookConfig {
  schemaVersion: 1;
  enabled: boolean;
  failurePolicy: HookFailurePolicy;
  timeoutMs: number;
  maxOutputChars: number;
  allowProjectHooks: boolean;
  hooks: HookDefinition[];
}

export interface HookInput {
  schemaVersion: 1;
  event: HookEvent;
  toolName: string;
  toolCallId: string;
  cwd: string;
  input: unknown;
  result?: { content: unknown; details: unknown; isError: boolean };
}

export interface HookRunResult {
  blocked: boolean;
  reason?: string;
  warnings: string[];
  appendText?: string;
  forceError?: boolean;
}

interface ProcessResult {
  code: number | null;
  killed: boolean;
  stdout: string;
  stderr: string;
  durationMs: number;
  failureReason?: string;
}

export const HOOK_MAX_INPUT_BYTES = 1024 * 1024;

function hookChildEnv() {
  return sanitizedChildEnv(process.env, { HOBOT_CODE_HOOK: "1" });
}

function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function serializeHookPayload(value: unknown): string {
  const seen = new WeakSet<object>();
  const serialized = JSON.stringify(value, (_key, item: unknown) => {
    if (typeof item === "bigint") return item.toString();
    if (item && typeof item === "object") {
      if (seen.has(item)) return "[Circular]";
      seen.add(item);
    }
    return item;
  });
  if (serialized === undefined) throw new Error("hook payload is not JSON serializable");
  return serialized;
}

function wildcardMatches(value: string, pattern: string): boolean {
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*");
  return new RegExp(`^${escaped}$`, "i").test(value);
}

async function runProcess(
  command: string[],
  input: string,
  cwd: string,
  timeoutMs: number,
  maxOutputChars: number,
  signal?: AbortSignal,
): Promise<ProcessResult> {
  const started = Date.now();
  if (signal?.aborted) {
    return {
      code: null,
      killed: true,
      stdout: "",
      stderr: "",
      durationMs: 0,
      failureReason: "hook execution was aborted before start",
    };
  }
  const inputBytes = Buffer.byteLength(input);
  if (inputBytes > HOOK_MAX_INPUT_BYTES) {
    return {
      code: null,
      killed: false,
      stdout: "",
      stderr: "",
      durationMs: 0,
      failureReason: `hook input exceeds ${HOOK_MAX_INPUT_BYTES} bytes`,
    };
  }
  return await new Promise((resolve, reject) => {
    const child = spawn(command[0]!, command.slice(1), {
      cwd,
      env: hookChildEnv(),
      detached: process.platform !== "win32",
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    let killed = false;
    let settled = false;
    let capturedChars = 0;
    let outputTruncated = false;
    let failureReason: string | undefined;
    const append = (current: string, chunk: Buffer): string => {
      const text = chunk.toString("utf8");
      const available = Math.max(0, maxOutputChars - capturedChars);
      const captured = text.slice(0, available);
      capturedChars += captured.length;
      if (captured.length < text.length) outputTruncated = true;
      return current + captured;
    };
    child.stdout.on("data", (chunk: Buffer) => { stdout = append(stdout, chunk); });
    child.stderr.on("data", (chunk: Buffer) => { stderr = append(stderr, chunk); });
    child.stdin.on("error", (error) => {
      if (settled || failureReason) return;
      failureReason = `hook stdin failed: ${error.message}`;
      killed = true;
      terminateProcessGroup(child, "SIGKILL");
    });
    const stop = (reason: string) => {
      if (!failureReason) failureReason = reason;
      killed = true;
      terminateProcessGroup(child, "SIGKILL");
    };
    const timer = setTimeout(() => stop(`hook timed out after ${timeoutMs} ms`), timeoutMs);
    const abort = () => stop("hook execution was aborted");
    signal?.addEventListener("abort", abort, { once: true });
    child.on("error", (error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      reject(error);
    });
    child.on("close", (code) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal?.removeEventListener("abort", abort);
      resolve({
        code,
        killed,
        stdout,
        stderr,
        durationMs: Date.now() - started,
        ...(failureReason || outputTruncated
          ? { failureReason: failureReason || `hook output exceeds ${maxOutputChars} characters` }
          : {}),
      });
    });
    if (signal?.aborted) abort();
    try {
      child.stdin.end(input);
    } catch (error) {
      failureReason = `hook stdin failed: ${error instanceof Error ? error.message : String(error)}`;
      stop(failureReason);
    }
  });
}

function audit(path: string, record: Record<string, unknown>): void {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  try {
    if (statSync(path).size >= 5 * 1024 * 1024) {
      rmSync(`${path}.1`, { force: true });
      renameSync(path, `${path}.1`);
    }
  } catch {
    // A missing audit file is the normal first-run case.
  }
  appendFileSync(path, `${JSON.stringify(record)}\n`, { mode: 0o600 });
  chmodSync(path, 0o600);
}

export async function runHooks(options: {
  config: HookConfig;
  event: HookEvent;
  toolName: string;
  toolCallId: string;
  cwd: string;
  input: unknown;
  result?: HookInput["result"];
  auditPath: string;
  signal?: AbortSignal;
}): Promise<HookRunResult> {
  const outcome: HookRunResult = { blocked: false, warnings: [] };
  if (!options.config.enabled) return outcome;
  const hooks = options.config.hooks.filter((hook) =>
    hook.event === options.event && wildcardMatches(options.toolName, hook.tool));
  if (hooks.length === 0) return outcome;
  const payload: HookInput = {
    schemaVersion: 1,
    event: options.event,
    toolName: options.toolName,
    toolCallId: options.toolCallId,
    cwd: options.cwd,
    input: options.input,
    ...(options.result ? { result: options.result } : {}),
  };
  let serialized: string;
  try {
    serialized = serializeHookPayload(payload);
  } catch (error) {
    const reason = redactSensitiveText(
      `hook input serialization failed: ${error instanceof Error ? error.message : String(error)}`,
      800,
    );
    const blocked = hooks.some((hook) => (hook.failurePolicy ?? options.config.failurePolicy) === "block");
    return blocked
      ? { blocked: true, reason, warnings: [] }
      : { blocked: false, warnings: [reason] };
  }

  const appendOutcomeText = (value: string): void => {
    const combined = [outcome.appendText, value].filter(Boolean).join("\n");
    outcome.appendText = combined.slice(0, options.config.maxOutputChars);
  };
  let warningChars = 0;
  const appendWarning = (value: string): void => {
    const remaining = options.config.maxOutputChars - warningChars;
    if (remaining <= 0) return;
    const warning = value.slice(0, remaining);
    outcome.warnings.push(warning);
    warningChars += warning.length;
  };

  for (const hook of hooks) {
    const policy = hook.failurePolicy ?? options.config.failurePolicy;
    let result: ProcessResult;
    try {
      result = await runProcess(
        hook.command,
        serialized,
        options.cwd,
        hook.timeoutMs ?? options.config.timeoutMs,
        options.config.maxOutputChars,
        options.signal,
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      result = {
        code: null,
        killed: false,
        stdout: "",
        stderr: "",
        durationMs: 0,
        failureReason: `hook process could not be started: ${message}`,
      };
    }

    let response: { block?: boolean; reason?: string; appendText?: string; isError?: boolean } = {};
    if (result.stdout.trim().startsWith("{")) {
      try {
        response = JSON.parse(result.stdout) as typeof response;
      } catch {
        result = { ...result, code: result.code === 0 ? null : result.code, stderr: "hook returned invalid JSON" };
      }
    }
    const failed = result.code !== 0 || result.killed || Boolean(result.failureReason);
    const reason = redactSensitiveText(
      response.reason
        || result.failureReason
        || result.stderr
        || `${hook.name} exited with ${result.code ?? "no status"}`,
      800,
    );
    audit(options.auditPath, {
      timestamp: new Date().toISOString(),
      hook: hook.name,
      event: options.event,
      toolName: options.toolName,
      toolCallId: options.toolCallId,
      inputHash: sha256(serialized),
      code: result.code,
      killed: result.killed,
      durationMs: result.durationMs,
      policy,
      blocked: Boolean(response.block) || (failed && policy === "block"),
      outputChars: result.stdout.length + result.stderr.length,
      outputHash: sha256(`${result.stdout}\0${result.stderr}`),
      ...(failed || response.block ? { reason } : {}),
    });

    if (response.appendText) {
      appendOutcomeText(redactSensitiveText(response.appendText, options.config.maxOutputChars));
    }
    if (response.isError) outcome.forceError = true;
    if (response.block || (failed && policy === "block")) {
      outcome.blocked = true;
      outcome.reason = reason;
      break;
    }
    if (failed) appendWarning(`${hook.name}: ${reason}`);
  }
  return outcome;
}
