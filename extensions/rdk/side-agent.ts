import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";

import {
  getMarkdownTheme,
  type ExtensionAPI,
  type ExtensionCommandContext,
  type Theme,
} from "@earendil-works/pi-coding-agent";
import {
  Input,
  Markdown,
  matchesKey,
  truncateToWidth,
  visibleWidth,
  type Focusable,
  type TUI,
} from "@earendil-works/pi-tui";

import { redactSensitiveText } from "./control-plane.mjs";
import { acquireSideAgentLease } from "./side-agent-lease.mjs";
import {
  applySideAgentEvent,
  buildSideAgentArgs,
  buildSideSessionSnapshot,
  createSideAgentEventState,
  parseSideAgentEvent,
  sideAgentPanelLayout,
} from "./side-agent-session.mjs";

const MAX_QUESTION_CHARS = 32_000;
const MAX_STDERR_CHARS = 16_000;
const MAX_JSON_LINE_CHARS = 2_000_000;
const MAX_TRANSCRIPT_CHARS = 240_000;
const MAX_TRANSCRIPT_MESSAGES = 120;
const SIDE_AGENT_SYSTEM_NOTE = `

## Ephemeral side-agent mode

You are an independent temporary agent created from an exact snapshot of the parent conversation.
Complete side tasks with the same engineering standards, tools, workspace, and board safety rules.
This is a private multi-turn conversation and will not be merged into the parent session. Files, processes,
services, and devices are shared with the parent and changes persist, so inspect live state before mutating it
and avoid conflicting writes. Do not update persistent memory or the parent's persistent goal.
`;

type SidePhase = "starting" | "idle" | "running" | "aborting" | "failed" | "closed";
type TranscriptRole = "user" | "assistant" | "notice";

interface SideEventState {
  streamingText: string;
  finalText: string;
  thinkingText: string;
  finalThinking: string;
  thinkingChars: number;
  tools: Array<{ id: string; name: string; target: string; status: "running" | "done" | "failed" }>;
  turns: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  stopReason?: string;
  errorMessage?: string;
}

interface TranscriptMessage {
  role: TranscriptRole;
  text: string;
  thinking?: string;
}

interface SideUiRequest {
  id: string;
  method: "confirm" | "select" | "input" | "editor";
  title: string;
  message?: string;
  options?: string[];
  placeholder?: string;
  timeout?: number;
}

interface SideAgentLease {
  activeCount: number;
  limit: number;
  release: () => Promise<void>;
}

function terminateSideAgent(child: ChildProcessWithoutNullStreams, signal: NodeJS.Signals): void {
  if (process.platform !== "win32" && child.pid) {
    try {
      process.kill(-child.pid, signal);
      return;
    } catch {
      // Fall back to the direct child if it is no longer a process-group leader.
    }
  }
  child.kill(signal);
}

function getHobotInvocation(args: string[]): { command: string; args: string[] } {
  const currentScript = process.argv[1];
  const isBunVirtualScript = currentScript?.startsWith("/$bunfs/root/");
  if (currentScript && !isBunVirtualScript && existsSync(currentScript)) {
    return { command: process.execPath, args: [currentScript, ...args] };
  }
  const execName = basename(process.execPath).toLowerCase();
  if (!/^(node|bun)(\.exe)?$/.test(execName)) return { command: process.execPath, args };
  return { command: "hobot", args };
}

function formatTokens(value: number): string {
  if (value < 1000) return String(value);
  if (value < 1_000_000) return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)}k`;
  return `${(value / 1_000_000).toFixed(1)}m`;
}

class SideAgentRun {
  readonly id: string;
  readonly initialQuestion: string;
  readonly tempDir: string;
  readonly sessionPath: string;
  phase: SidePhase = "starting";
  state: SideEventState = createSideAgentEventState() as SideEventState;
  transcript: TranscriptMessage[] = [];
  pendingUiRequest: SideUiRequest | undefined;
  stderr = "";
  startedAt = Date.now();
  turnStartedAt = Date.now();
  exitCode: number | null = null;
  readonly finished: Promise<void>;

  private process: ChildProcessWithoutNullStreams | undefined;
  private finish!: () => void;
  private finishedSettled = false;
  private listeners = new Set<() => void>();
  private stdoutBuffer = "";
  private oversizedLine = false;
  private terminating = false;
  private requestId = 0;
  private uiRequestTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly args: string[];
  private readonly cwd: string;
  private readonly parentSession: string | undefined;

  constructor(
    id: string,
    initialQuestion: string,
    tempDir: string,
    sessionPath: string,
    args: string[],
    cwd: string,
    parentSession: string | undefined,
  ) {
    this.id = id;
    this.initialQuestion = initialQuestion;
    this.tempDir = tempDir;
    this.sessionPath = sessionPath;
    this.args = args;
    this.cwd = cwd;
    this.parentSession = parentSession;
    this.finished = new Promise((resolve) => {
      this.finish = resolve;
    });
  }

  get isBusy(): boolean {
    return ["starting", "running", "aborting"].includes(this.phase);
  }

  get canPrompt(): boolean {
    return this.phase === "idle" && !this.pendingUiRequest;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private changed(): void {
    for (const listener of this.listeners) listener();
  }

  private settleFinished(): void {
    if (this.finishedSettled) return;
    this.finishedSettled = true;
    this.finish();
  }

  private appendTranscript(message: TranscriptMessage): void {
    if (!message.text && !message.thinking) return;
    this.transcript.push(message);
    let chars = this.transcript.reduce((total, item) => total + item.text.length + (item.thinking?.length ?? 0), 0);
    while (this.transcript.length > MAX_TRANSCRIPT_MESSAGES || chars > MAX_TRANSCRIPT_CHARS) {
      const removed = this.transcript.shift();
      if (!removed) break;
      chars -= removed.text.length + (removed.thinking?.length ?? 0);
    }
  }

  private addNotice(message: string): void {
    this.appendTranscript({ role: "notice", text: redactSensitiveText(message).slice(0, MAX_STDERR_CHARS) });
  }

  private resetTurnState(): void {
    this.state = {
      ...this.state,
      streamingText: "",
      finalText: "",
      thinkingText: "",
      finalThinking: "",
      tools: [],
      stopReason: undefined,
      errorMessage: undefined,
    };
  }

  private sendRaw(command: Record<string, unknown>): boolean {
    const stdin = this.process?.stdin;
    if (!stdin || stdin.destroyed || !stdin.writable) {
      this.addNotice("The side-agent process is not available.");
      this.phase = "failed";
      this.changed();
      return false;
    }
    try {
      stdin.write(`${JSON.stringify(command)}\n`, (error) => {
        if (!error) return;
        this.addNotice(error.message);
        this.phase = "failed";
        this.changed();
      });
      return true;
    } catch (error) {
      this.addNotice(error instanceof Error ? error.message : String(error));
      this.phase = "failed";
      this.changed();
      return false;
    }
  }

  private sendCommand(type: string, fields: Record<string, unknown> = {}): boolean {
    return this.sendRaw({ id: `btw_${++this.requestId}`, type, ...fields });
  }

  start(): void {
    const invocation = getHobotInvocation(this.args);
    try {
      const child = spawn(invocation.command, invocation.args, {
        cwd: this.cwd,
        shell: false,
        stdio: ["pipe", "pipe", "pipe"],
        detached: process.platform !== "win32",
        env: {
          ...process.env,
          HOBOT_CODE_SIDE_AGENT: "1",
          HOBOT_CODE_SIDE_PARENT_SESSION: this.parentSession ?? "",
        },
      });
      this.process = child;
      child.stdin.on("error", (error) => {
        if (this.terminating) return;
        this.addNotice(error.message);
        this.phase = "failed";
        this.changed();
      });
      child.stdout.on("data", (chunk: Buffer) => this.consumeStdout(chunk.toString("utf8")));
      child.stderr.on("data", (chunk: Buffer) => {
        this.stderr = `${this.stderr}${redactSensitiveText(chunk.toString("utf8"))}`.slice(-MAX_STDERR_CHARS);
        this.changed();
      });
      child.on("error", (error) => {
        if (this.terminating) return;
        this.addNotice(error.message);
        this.phase = "failed";
        this.changed();
      });
      child.on("close", (code) => {
        this.consumeLine(this.stdoutBuffer);
        this.stdoutBuffer = "";
        this.exitCode = code;
        this.clearUiRequest();
        if (this.terminating) this.phase = "closed";
        else {
          this.phase = "failed";
          this.addNotice(`Side-agent process exited unexpectedly with code ${code ?? "unknown"}.`);
        }
        this.changed();
        this.settleFinished();
      });
      this.phase = "idle";
      this.changed();
      this.sendPrompt(this.initialQuestion);
    } catch (error) {
      this.phase = "failed";
      this.addNotice(error instanceof Error ? error.message : String(error));
      this.changed();
      this.settleFinished();
    }
  }

  sendPrompt(question: string): boolean {
    const trimmed = question.trim();
    if (!trimmed || trimmed.length > MAX_QUESTION_CHARS || !this.canPrompt) return false;
    this.resetTurnState();
    this.appendTranscript({ role: "user", text: trimmed });
    this.turnStartedAt = Date.now();
    this.phase = "running";
    const sent = this.sendCommand("prompt", { message: trimmed });
    this.changed();
    return sent;
  }

  abortTurn(): void {
    if (!this.isBusy || this.phase === "starting") return;
    this.phase = "aborting";
    this.sendCommand("abort");
    this.changed();
  }

  respondToUi(response: { value?: string; confirmed?: boolean; cancelled?: true }): void {
    const request = this.pendingUiRequest;
    if (!request) return;
    this.sendRaw({ type: "extension_ui_response", id: request.id, ...response });
    this.clearUiRequest();
    this.changed();
  }

  private setUiRequest(request: SideUiRequest): void {
    this.clearUiRequest();
    this.pendingUiRequest = request;
    if (request.timeout && request.timeout > 0) {
      this.uiRequestTimer = setTimeout(() => this.respondToUi({ cancelled: true }), request.timeout);
      this.uiRequestTimer.unref?.();
    }
    this.changed();
  }

  private clearUiRequest(): void {
    if (this.uiRequestTimer) clearTimeout(this.uiRequestTimer);
    this.uiRequestTimer = undefined;
    this.pendingUiRequest = undefined;
  }

  private consumeStdout(chunk: string): void {
    this.stdoutBuffer += chunk;
    if (this.stdoutBuffer.length > MAX_JSON_LINE_CHARS && !this.stdoutBuffer.includes("\n")) {
      this.stdoutBuffer = "";
      this.oversizedLine = true;
      this.addNotice("A side-agent event exceeded the display limit and was omitted.");
      this.changed();
      return;
    }
    const lines = this.stdoutBuffer.split("\n");
    this.stdoutBuffer = lines.pop() ?? "";
    for (const line of lines) this.consumeLine(line);
  }

  private consumeLine(line: string): void {
    if (!line || this.oversizedLine) {
      this.oversizedLine = false;
      return;
    }
    const event = parseSideAgentEvent(line) as Record<string, any> | undefined;
    if (!event) return;

    if (event.type === "extension_ui_request") {
      if (event.method === "notify") {
        this.addNotice(String(event.message ?? ""));
      } else if (["confirm", "select", "input", "editor"].includes(String(event.method))) {
        this.setUiRequest({
          id: String(event.id),
          method: event.method,
          title: String(event.title ?? "Side agent request"),
          message: typeof event.message === "string" ? event.message : undefined,
          options: Array.isArray(event.options) ? event.options.map(String) : undefined,
          placeholder: typeof event.placeholder === "string" ? event.placeholder : event.prefill,
          timeout: Number.isFinite(event.timeout) ? Number(event.timeout) : undefined,
        });
      }
      this.changed();
      return;
    }

    if (event.type === "response" && event.success === false) {
      this.state = { ...this.state, errorMessage: String(event.error ?? "RPC command failed") };
      this.addNotice(String(event.error ?? "RPC command failed"));
      if (event.command === "prompt" || event.command === "abort") this.phase = "idle";
      this.changed();
      return;
    }

    this.state = applySideAgentEvent(this.state, event, redactSensitiveText) as SideEventState;
    if (event.type === "message_end" && event.message?.role === "assistant") {
      this.appendTranscript({
        role: "assistant",
        text: this.state.finalText,
        thinking: this.state.finalThinking,
      });
      this.state = {
        ...this.state,
        streamingText: "",
        finalText: "",
        thinkingText: "",
        finalThinking: "",
      };
    } else if (event.type === "agent_start") {
      this.phase = "running";
    } else if (event.type === "agent_end" || event.type === "agent_settled") {
      this.phase = "idle";
    }
    this.changed();
  }

  async cleanup(): Promise<void> {
    this.terminating = true;
    this.clearUiRequest();
    const child = this.process;
    if (child && child.exitCode === null) {
      this.sendCommand("abort");
      terminateSideAgent(child, "SIGTERM");
      const timer = setTimeout(() => {
        if (child.exitCode === null) terminateSideAgent(child, "SIGKILL");
      }, 5000);
      timer.unref?.();
    } else {
      this.settleFinished();
    }
    await this.finished;
    await rm(this.tempDir, { recursive: true, force: true });
  }
}

class SideAgentOverlay implements Focusable {
  private readonly input = new Input();
  private readonly tui: TUI;
  private readonly theme: Theme;
  private readonly run: SideAgentRun;
  private readonly done: (result: "close") => void;
  private _focused = false;
  private scrollOffset = 0;
  private disposed = false;
  private inputError = "";
  private unsubscribe: () => void;
  private ticker?: ReturnType<typeof setInterval>;

  get focused(): boolean {
    return this._focused;
  }

  set focused(value: boolean) {
    this._focused = value;
    this.input.focused = value;
  }

  constructor(
    tui: TUI,
    theme: Theme,
    run: SideAgentRun,
    done: (result: "close") => void,
  ) {
    this.tui = tui;
    this.theme = theme;
    this.run = run;
    this.done = done;
    this.input.onSubmit = (value) => this.submit(value);
    this.unsubscribe = run.subscribe(() => {
      this.scrollOffset = Number.MAX_SAFE_INTEGER;
      this.inputError = "";
      this.syncTicker();
      this.tui.requestRender();
    });
    this.syncTicker();
  }

  private syncTicker(): void {
    if (this.run.isBusy && !this.ticker) {
      this.ticker = setInterval(() => this.tui.requestRender(), 1000);
      this.ticker.unref?.();
    } else if (!this.run.isBusy && this.ticker) {
      clearInterval(this.ticker);
      this.ticker = undefined;
    }
  }

  private submit(value: string): void {
    const trimmed = value.trim();
    const request = this.run.pendingUiRequest;
    if (request) {
      if (request.method === "confirm") {
        const normalized = trimmed.toLowerCase();
        if (!["y", "yes", "n", "no"].includes(normalized)) {
          this.inputError = "Enter y or n.";
          this.tui.requestRender();
          return;
        }
        this.run.respondToUi({ confirmed: normalized === "y" || normalized === "yes" });
      } else if (request.method === "select") {
        const numeric = Number.parseInt(trimmed, 10);
        const valueFromIndex = Number.isInteger(numeric) && numeric > 0 ? request.options?.[numeric - 1] : undefined;
        const selected = valueFromIndex ?? request.options?.find((option) => option === trimmed);
        if (!selected) {
          this.inputError = "Enter an option number or exact value.";
          this.tui.requestRender();
          return;
        }
        this.run.respondToUi({ value: selected });
      } else {
        this.run.respondToUi({ value: trimmed });
      }
      this.input.setValue("");
      return;
    }

    if (!trimmed) return;
    if (!this.run.canPrompt) {
      this.inputError = "The current turn is still running. Press Esc to abort it first.";
      this.tui.requestRender();
      return;
    }
    if (trimmed.length > MAX_QUESTION_CHARS) {
      this.inputError = `Message exceeds ${MAX_QUESTION_CHARS} characters.`;
      this.tui.requestRender();
      return;
    }
    if (this.run.sendPrompt(trimmed)) this.input.setValue("");
  }

  handleInput(data: string): void {
    if (matchesKey(data, "ctrl+d")) {
      this.done("close");
      return;
    }

    const request = this.run.pendingUiRequest;
    if (request?.method === "confirm") {
      if (data.toLowerCase() === "y") {
        this.run.respondToUi({ confirmed: true });
        this.input.setValue("");
        return;
      }
      if (data.toLowerCase() === "n") {
        this.run.respondToUi({ confirmed: false });
        this.input.setValue("");
        return;
      }
    }

    if (matchesKey(data, "escape")) {
      if (request) this.run.respondToUi({ cancelled: true });
      else if (this.run.isBusy) this.run.abortTurn();
      else this.done("close");
      return;
    }
    if (matchesKey(data, "ctrl+c")) {
      this.input.setValue("");
      this.inputError = "";
      this.tui.requestRender();
      return;
    }
    if (matchesKey(data, "pageup")) {
      this.scrollOffset = Math.max(0, this.scrollOffset - 8);
      this.tui.requestRender();
      return;
    }
    if (matchesKey(data, "pagedown")) {
      this.scrollOffset += 8;
      this.tui.requestRender();
      return;
    }
    if (!this.input.getValue() && matchesKey(data, "up")) {
      this.scrollOffset = Math.max(0, this.scrollOffset - 1);
      this.tui.requestRender();
      return;
    }
    if (!this.input.getValue() && matchesKey(data, "down")) {
      this.scrollOffset += 1;
      this.tui.requestRender();
      return;
    }
    this.input.handleInput(data);
    this.tui.requestRender();
  }

  private transcriptLines(width: number): string[] {
    const lines: string[] = [];
    const markdownTheme = getMarkdownTheme();
    for (const message of this.run.transcript) {
      if (message.role === "user") {
        const parts = message.text.split("\n");
        lines.push(this.theme.fg("accent", ` You: ${parts[0] ?? ""}`));
        for (const part of parts.slice(1)) lines.push(this.theme.fg("accent", `      ${part}`));
      } else if (message.role === "notice") {
        lines.push(this.theme.fg("warning", ` ${message.text}`));
      } else {
        if (message.thinking) {
          lines.push(this.theme.fg("dim", " Thinking:"));
          for (const part of message.thinking.split("\n")) lines.push(this.theme.fg("dim", ` ${part}`));
        }
        if (message.text) {
          lines.push(this.theme.fg("success", " Agent:"));
          lines.push(...new Markdown(message.text, 1, 0, markdownTheme).render(width));
        }
      }
      lines.push("");
    }

    for (const tool of this.run.state.tools) {
      const mark = tool.status === "done" ? "+" : tool.status === "failed" ? "x" : ">";
      const color = tool.status === "done" ? "success" : tool.status === "failed" ? "error" : "accent";
      lines.push(this.theme.fg(color, ` ${mark} ${tool.name}${tool.target ? `: ${tool.target}` : ""}`));
    }
    if (this.run.state.thinkingText) {
      lines.push(this.theme.fg("dim", " Thinking:"));
      for (const part of this.run.state.thinkingText.split("\n")) lines.push(this.theme.fg("dim", ` ${part}`));
    }
    if (this.run.state.streamingText) {
      lines.push(this.theme.fg("success", " Agent:"));
      lines.push(...new Markdown(this.run.state.streamingText, 1, 0, markdownTheme).render(width));
    } else if (this.run.isBusy && !this.run.pendingUiRequest) {
      lines.push(this.theme.fg("dim", " Working..."));
    }

    const request = this.run.pendingUiRequest;
    if (request) {
      lines.push("");
      lines.push(this.theme.fg("warning", ` ${request.title}`));
      if (request.message) lines.push(this.theme.fg("warning", ` ${request.message}`));
      if (request.options) {
        request.options.forEach((option, index) => lines.push(` ${index + 1}. ${option}`));
      }
      if (request.method === "confirm") lines.push(this.theme.fg("dim", " Confirm with y or n."));
    }
    if (this.inputError) lines.push(this.theme.fg("error", ` ${this.inputError}`));
    return lines;
  }

  render(width: number): string[] {
    const th = this.theme;
    const layout = sideAgentPanelLayout(width, this.tui.terminal.rows);
    const { innerWidth, panelWidth } = layout;
    const border = (value: string) => th.fg("border", value);
    const pad = (value: string) => {
      const clipped = truncateToWidth(value, innerWidth, innerWidth >= 3 ? "..." : "", true);
      return clipped + " ".repeat(Math.max(0, innerWidth - visibleWidth(clipped)));
    };
    const phaseColor = this.run.phase === "idle"
      ? "success"
      : this.run.phase === "failed"
        ? "error"
        : this.run.phase === "aborting"
          ? "warning"
          : "accent";
    const elapsedStart = this.run.isBusy ? this.run.turnStartedAt : this.run.startedAt;
    const elapsed = Math.max(0, Math.round((Date.now() - elapsedStart) / 1000));
    if (layout.compact) {
      const status = th.fg(phaseColor as any, `BTW ${this.run.phase} ${elapsed}s`);
      const clipped = truncateToWidth(status, panelWidth, "", true);
      return [clipped + " ".repeat(Math.max(0, panelWidth - visibleWidth(clipped)))];
    }
    const title = ` BTW side agent | ${this.run.phase} | ${elapsed}s `;
    const titleText = truncateToWidth(title, innerWidth, innerWidth >= 3 ? "..." : "", true);
    const titleRule = "─".repeat(Math.max(0, innerWidth - visibleWidth(titleText)));

    const content = this.transcriptLines(innerWidth);
    const maxContentLines = layout.contentRows;
    const maxOffset = Math.max(0, content.length - maxContentLines);
    this.scrollOffset = Math.min(this.scrollOffset, maxOffset);
    const visible = content.slice(this.scrollOffset, this.scrollOffset + maxContentLines);
    const result = [border("╭") + th.fg(phaseColor as any, titleText) + border(`${titleRule}╮`)];
    for (const line of visible) result.push(border("│") + pad(line) + border("│"));
    while (result.length < maxContentLines + 1) result.push(border("│") + pad("") + border("│"));

    let inputLabel = this.run.pendingUiRequest ? " Reply: " : " You: ";
    if (innerWidth < 10) inputLabel = this.run.pendingUiRequest ? "?" : ">";
    const inputWidth = Math.max(1, innerWidth - visibleWidth(inputLabel));
    const inputLine = this.input.render(inputWidth)[0] ?? "";
    result.push(border("├") + border("─".repeat(innerWidth)) + border("┤"));
    result.push(border("│") + pad(`${th.fg("accent", inputLabel)}${inputLine}`) + border("│"));

    const usage = [
      `${this.run.state.turns} response${this.run.state.turns === 1 ? "" : "s"}`,
      `in ${formatTokens(this.run.state.inputTokens)}`,
      `out ${formatTokens(this.run.state.outputTokens)}`,
      this.run.state.cacheReadTokens ? `cache ${formatTokens(this.run.state.cacheReadTokens)}` : undefined,
    ].filter(Boolean).join(" | ");
    const help = this.run.isBusy
      ? "Enter after completion | Esc abort turn | Ctrl+D close and delete"
      : "Enter send | Esc/Ctrl+D close and delete | PgUp/PgDn scroll";
    result.push(border("│") + pad(th.fg("dim", ` ${usage} | ${help}`)) + border("│"));
    result.push(border(`╰${"─".repeat(innerWidth)}╯`));
    return result;
  }

  invalidate(): void {
    this.input.invalidate();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    if (this.ticker) clearInterval(this.ticker);
    this.ticker = undefined;
    this.unsubscribe();
  }
}

async function createSideAgentRun(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): Promise<SideAgentRun> {
  const question = args.trim();
  if (!question) throw new Error("Usage: /btw <task>");
  if (question.length > MAX_QUESTION_CHARS) throw new Error(`/btw task exceeds ${MAX_QUESTION_CHARS} characters`);
  if (!ctx.model) throw new Error("No model is selected");

  const tempDir = await mkdtemp(join(tmpdir(), "hobot-btw-"));
  await chmod(tempDir, 0o700);
  try {
    const id = randomUUID();
    const timestamp = new Date().toISOString();
    const sessionPath = join(tempDir, "session.jsonl");
    const systemPromptPath = join(tempDir, "system-prompt.md");
    const parentSession = ctx.sessionManager.getSessionFile();
    const snapshot = buildSideSessionSnapshot({
      header: ctx.sessionManager.getHeader(),
      entries: ctx.sessionManager.getBranch(),
      id,
      timestamp,
      cwd: ctx.cwd,
      parentSession,
    });
    await writeFile(sessionPath, snapshot, { encoding: "utf8", mode: 0o600 });
    await writeFile(systemPromptPath, `${ctx.getSystemPrompt()}${SIDE_AGENT_SYSTEM_NOTE}`, {
      encoding: "utf8",
      mode: 0o600,
    });
    const childArgs = buildSideAgentArgs({
      sessionPath,
      sessionDir: tempDir,
      systemPromptPath,
      model: ctx.model,
      thinkingLevel: ctx.thinkingLevel,
      tools: pi.getActiveTools(),
      projectTrusted: ctx.isProjectTrusted(),
    });
    return new SideAgentRun(id, question, tempDir, sessionPath, childArgs, ctx.cwd, parentSession);
  } catch (error) {
    await rm(tempDir, { recursive: true, force: true });
    throw error;
  }
}

export function registerSideAgent(pi: ExtensionAPI): () => Promise<void> {
  let active: SideAgentRun | undefined;
  let activeLease: SideAgentLease | undefined;

  pi.registerCommand("btw", {
    description: "Open a private multi-turn agent with a snapshot of the current context",
    handler: async (args, ctx) => {
      if (ctx.mode !== "tui") {
        ctx.ui.notify("/btw requires the interactive TUI", "error");
        return;
      }
      if (active) {
        ctx.ui.notify("A /btw side agent is already open. Close it before starting another.", "warning");
        return;
      }

      try {
        activeLease = await acquireSideAgentLease() as SideAgentLease;
        active = await createSideAgentRun(pi, String(args ?? ""), ctx);
        const run = active;
        ctx.ui.setStatus("hobot-btw", `btw: open ${activeLease.activeCount}/${activeLease.limit}`);
        run.start();
        await ctx.ui.custom<"close">(
          (tui, theme, _keybindings, done) => new SideAgentOverlay(tui, theme, run, done),
          {
            overlay: true,
            overlayOptions: { anchor: "right-center", width: "50%", maxHeight: "100%", margin: 0 },
          },
        );
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      } finally {
        const run = active;
        const lease = activeLease;
        active = undefined;
        activeLease = undefined;
        ctx.ui.setStatus("hobot-btw", undefined);
        if (run) {
          await run.cleanup();
          ctx.ui.notify("Side-agent conversation deleted. Workspace and device changes were retained.", "info");
        }
        await lease?.release();
      }
    },
  });

  return async () => {
    const run = active;
    const lease = activeLease;
    active = undefined;
    activeLease = undefined;
    if (run) await run.cleanup();
    await lease?.release();
  };
}
