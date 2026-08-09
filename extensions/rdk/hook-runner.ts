import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { appendFileSync, chmodSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";

import { redactSensitiveText } from "./control-plane.mjs";

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
}

function sha256(value: string): string {
  return createHash("sha256").update(value).digest("hex");
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
  return await new Promise((resolve, reject) => {
    const child = spawn(command[0]!, command.slice(1), {
      cwd,
      env: { ...process.env, HOBOT_CODE_HOOK: "1" },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    let killed = false;
    let settled = false;
    const append = (current: string, chunk: Buffer): string => {
      const next = current + chunk.toString("utf8");
      return next.length > maxOutputChars ? next.slice(0, maxOutputChars) : next;
    };
    child.stdout.on("data", (chunk: Buffer) => { stdout = append(stdout, chunk); });
    child.stderr.on("data", (chunk: Buffer) => { stderr = append(stderr, chunk); });
    const stop = () => {
      killed = true;
      child.kill("SIGKILL");
    };
    const timer = setTimeout(stop, timeoutMs);
    const abort = () => stop();
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
      resolve({ code, killed, stdout, stderr, durationMs: Date.now() - started });
    });
    child.stdin.end(input);
  });
}

function audit(path: string, record: Record<string, unknown>): void {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
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
  const payload: HookInput = {
    schemaVersion: 1,
    event: options.event,
    toolName: options.toolName,
    toolCallId: options.toolCallId,
    cwd: options.cwd,
    input: options.input,
    ...(options.result ? { result: options.result } : {}),
  };
  const serialized = JSON.stringify(payload);

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
      result = {
        code: null,
        killed: false,
        stdout: "",
        stderr: error instanceof Error ? error.message : String(error),
        durationMs: 0,
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
    const failed = result.code !== 0 || result.killed;
    const reason = redactSensitiveText(
      response.reason || result.stderr || `${hook.name} exited with ${result.code ?? "no status"}`,
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
      stdout: redactSensitiveText(result.stdout, options.config.maxOutputChars),
      stderr: redactSensitiveText(result.stderr, options.config.maxOutputChars),
    });

    if (response.appendText) {
      outcome.appendText = [outcome.appendText, redactSensitiveText(response.appendText, options.config.maxOutputChars)]
        .filter(Boolean).join("\n");
    }
    if (response.isError) outcome.forceError = true;
    if (response.block || (failed && policy === "block")) {
      outcome.blocked = true;
      outcome.reason = reason;
      break;
    }
    if (failed) outcome.warnings.push(`${hook.name}: ${reason}`);
  }
  return outcome;
}
