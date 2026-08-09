import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync } from "node:fs";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { randomUUID } from "node:crypto";

import {
  getMarkdownTheme,
  type ExtensionAPI,
  type ExtensionCommandContext,
  type Theme,
} from "@earendil-works/pi-coding-agent";
import {
  Markdown,
  matchesKey,
  truncateToWidth,
  visibleWidth,
  type Focusable,
  type TUI,
} from "@earendil-works/pi-tui";

import { redactSensitiveText } from "./control-plane.mjs";
import {
  applySideAgentEvent,
  buildSideAgentArgs,
  buildSideSessionSnapshot,
  createSideAgentEventState,
  parseSideAgentEvent,
} from "./side-agent-session.mjs";

const MAX_QUESTION_CHARS = 32_000;
const MAX_STDERR_CHARS = 16_000;
const MAX_JSON_LINE_CHARS = 2_000_000;
const SIDE_AGENT_SYSTEM_NOTE = `

## Ephemeral side-agent mode

You are an independent temporary agent created from an exact snapshot of the parent conversation.
Complete the side task with the same engineering standards, tools, workspace, and board safety rules.
Your conversation will not be merged into the parent session. Files, processes, services, and devices are
shared with the parent and any changes you make persist, so inspect live state before mutating it and avoid
conflicting writes. Do not update persistent memory or the parent's persistent goal from this side session.
`;

type SidePhase = "starting" | "running" | "completed" | "failed" | "cancelled";

interface SideEventState {
  streamingText: string;
  finalText: string;
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
  phase: SidePhase = "starting";
  state: SideEventState = createSideAgentEventState() as SideEventState;
  stderr = "";
  startedAt = Date.now();
  exitCode: number | null = null;
  readonly finished: Promise<void>;

  private process: ChildProcessWithoutNullStreams | undefined;
  private finish!: () => void;
  private listeners = new Set<() => void>();
  private stdoutBuffer = "";
  private oversizedLine = false;
  private terminating = false;

  constructor(
    readonly id: string,
    readonly question: string,
    readonly tempDir: string,
    readonly sessionPath: string,
    private readonly args: string[],
    private readonly cwd: string,
    private readonly parentSession: string | undefined,
  ) {
    this.finished = new Promise((resolve) => {
      this.finish = resolve;
    });
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private changed(): void {
    for (const listener of this.listeners) listener();
  }

  start(): void {
    const invocation = getHobotInvocation(this.args);
    this.phase = "running";
    try {
      const child = spawn(invocation.command, invocation.args, {
        cwd: this.cwd,
        shell: false,
        stdio: ["pipe", "pipe", "pipe"],
        env: {
          ...process.env,
          HOBOT_CODE_SIDE_AGENT: "1",
          HOBOT_CODE_SIDE_PARENT_SESSION: this.parentSession ?? "",
        },
      });
      this.process = child;
      child.stdin.on("error", () => undefined);
      child.stdin.end(this.question);
      child.stdout.on("data", (chunk: Buffer) => this.consumeStdout(chunk.toString("utf8")));
      child.stderr.on("data", (chunk: Buffer) => {
        this.stderr = `${this.stderr}${redactSensitiveText(chunk.toString("utf8"))}`.slice(-MAX_STDERR_CHARS);
        this.changed();
      });
      child.on("error", (error) => {
        this.stderr = redactSensitiveText(error.message).slice(-MAX_STDERR_CHARS);
      });
      child.on("close", (code) => {
        this.consumeLine(this.stdoutBuffer);
        this.stdoutBuffer = "";
        this.exitCode = code;
        if (this.terminating) this.phase = "cancelled";
        else if (code === 0 && this.state.stopReason !== "error" && this.state.stopReason !== "aborted") {
          this.phase = "completed";
        } else {
          this.phase = "failed";
        }
        this.changed();
        this.finish();
      });
    } catch (error) {
      this.phase = "failed";
      this.stderr = redactSensitiveText(error instanceof Error ? error.message : String(error));
      this.changed();
      this.finish();
    }
    this.changed();
  }

  private consumeStdout(chunk: string): void {
    this.stdoutBuffer += chunk;
    if (this.stdoutBuffer.length > MAX_JSON_LINE_CHARS && !this.stdoutBuffer.includes("\n")) {
      this.stdoutBuffer = "";
      this.oversizedLine = true;
      this.stderr = "A side-agent event exceeded the display limit and was omitted.";
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
    const event = parseSideAgentEvent(line);
    if (!event) return;
    this.state = applySideAgentEvent(this.state, event, redactSensitiveText) as SideEventState;
    this.changed();
  }

  cancel(): void {
    if (!this.process || ["completed", "failed", "cancelled"].includes(this.phase)) return;
    this.terminating = true;
    this.phase = "cancelled";
    this.process.kill("SIGTERM");
    const processToKill = this.process;
    const timer = setTimeout(() => {
      if (processToKill.exitCode === null) processToKill.kill("SIGKILL");
    }, 5000);
    timer.unref?.();
    this.changed();
  }

  async cleanup(): Promise<void> {
    this.cancel();
    await this.finished;
    await rm(this.tempDir, { recursive: true, force: true });
  }
}

class SideAgentOverlay implements Focusable {
  focused = false;
  private scrollOffset = 0;
  private disposed = false;
  private unsubscribe: () => void;
  private ticker?: ReturnType<typeof setInterval>;

  constructor(
    private readonly tui: TUI,
    private readonly theme: Theme,
    private readonly run: SideAgentRun,
    private readonly done: (result: "close" | "cancel") => void,
  ) {
    this.unsubscribe = run.subscribe(() => {
      this.scrollOffset = Number.MAX_SAFE_INTEGER;
      if (["completed", "failed", "cancelled"].includes(this.run.phase)) this.stopTicker();
      this.tui.requestRender();
    });
    this.ticker = setInterval(() => this.tui.requestRender(), 1000);
    this.ticker.unref?.();
    if (["completed", "failed", "cancelled"].includes(this.run.phase)) this.stopTicker();
  }

  private stopTicker(): void {
    if (!this.ticker) return;
    clearInterval(this.ticker);
    this.ticker = undefined;
  }

  handleInput(data: string): void {
    const terminal = ["completed", "failed", "cancelled"].includes(this.run.phase);
    if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) {
      if (!terminal) this.run.cancel();
      this.done(terminal ? "close" : "cancel");
      return;
    }
    if (terminal && (matchesKey(data, "return") || matchesKey(data, "space"))) {
      this.done("close");
      return;
    }
    if (matchesKey(data, "up")) this.scrollOffset = Math.max(0, this.scrollOffset - 1);
    else if (matchesKey(data, "down")) this.scrollOffset += 1;
    else if (matchesKey(data, "pageup")) this.scrollOffset = Math.max(0, this.scrollOffset - 8);
    else if (matchesKey(data, "pagedown")) this.scrollOffset += 8;
    this.tui.requestRender();
  }

  render(width: number): string[] {
    const th = this.theme;
    const innerWidth = Math.max(20, width - 2);
    const border = (value: string) => th.fg("border", value);
    const pad = (value: string) => {
      const clipped = truncateToWidth(value, innerWidth, "...", true);
      return clipped + " ".repeat(Math.max(0, innerWidth - visibleWidth(clipped)));
    };
    const phaseColor = this.run.phase === "completed"
      ? "success"
      : this.run.phase === "failed"
        ? "error"
        : this.run.phase === "cancelled"
          ? "warning"
          : "accent";
    const elapsed = Math.max(0, Math.round((Date.now() - this.run.startedAt) / 1000));
    const title = ` BTW side agent | ${this.run.phase} | ${elapsed}s `;
    const titleText = truncateToWidth(title, innerWidth);
    const titleRule = "─".repeat(Math.max(0, innerWidth - visibleWidth(titleText)));

    const content: string[] = [];
    content.push(th.fg("dim", ` Task: ${this.run.question.replace(/\s+/g, " ")}`));
    content.push(th.fg("warning", " Ephemeral chat only; workspace and device changes persist."));
    content.push("");
    for (const tool of this.run.state.tools) {
      const mark = tool.status === "done" ? "+" : tool.status === "failed" ? "x" : ">";
      const color = tool.status === "done" ? "success" : tool.status === "failed" ? "error" : "accent";
      content.push(th.fg(color, ` ${mark} ${tool.name}${tool.target ? `: ${tool.target}` : ""}`));
    }

    const output = this.run.state.streamingText || this.run.state.finalText;
    if (output) {
      if (content.length > 3) content.push("");
      const markdown = new Markdown(output, 1, 0, getMarkdownTheme());
      content.push(...markdown.render(innerWidth));
    } else if (this.run.phase === "running" || this.run.phase === "starting") {
      content.push(th.fg("dim", " Waiting for the first model response..."));
    }
    if (this.run.phase === "failed" && (this.run.state.errorMessage || this.run.stderr)) {
      content.push("");
      content.push(th.fg("error", ` ${this.run.state.errorMessage || this.run.stderr}`));
    }

    const usage = [
      `${this.run.state.turns} turn${this.run.state.turns === 1 ? "" : "s"}`,
      `in ${formatTokens(this.run.state.inputTokens)}`,
      `out ${formatTokens(this.run.state.outputTokens)}`,
      this.run.state.cacheReadTokens ? `cache ${formatTokens(this.run.state.cacheReadTokens)}` : undefined,
    ].filter(Boolean).join(" | ");
    const help = ["Up/Down scroll", ["completed", "failed", "cancelled"].includes(this.run.phase)
      ? "Enter/Esc close and delete"
      : "Esc cancel and delete"].join(" | ");
    const footer = `${usage} | ${help}`;

    const maxContentLines = 18;
    const maxOffset = Math.max(0, content.length - maxContentLines);
    this.scrollOffset = Math.min(this.scrollOffset, maxOffset);
    const visible = content.slice(this.scrollOffset, this.scrollOffset + maxContentLines);
    const result = [border("╭") + th.fg(phaseColor as any, titleText) + border(`${titleRule}╮`)];
    for (const line of visible) result.push(border("│") + pad(line) + border("│"));
    while (result.length < maxContentLines + 1) result.push(border("│") + pad("") + border("│"));
    result.push(border("├") + border("─".repeat(innerWidth)) + border("┤"));
    result.push(border("│") + pad(th.fg("dim", ` ${footer}`)) + border("│"));
    result.push(border(`╰${"─".repeat(innerWidth)}╯`));
    return result;
  }

  invalidate(): void {}

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.stopTicker();
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

  pi.registerCommand("btw", {
    description: "Open an ephemeral full-capability agent with a snapshot of the current context",
    handler: async (args, ctx) => {
      if (ctx.mode !== "tui") {
        ctx.ui.notify("/btw requires the interactive TUI", "error");
        return;
      }
      if (active) {
        ctx.ui.notify("A /btw side agent is already open. Close or cancel it before starting another.", "warning");
        return;
      }

      try {
        active = await createSideAgentRun(pi, String(args ?? ""), ctx);
        const run = active;
        ctx.ui.setStatus("hobot-btw", "btw: running");
        run.start();
        await ctx.ui.custom<"close" | "cancel">(
          (tui, theme, _keybindings, done) => new SideAgentOverlay(tui, theme, run, done),
          {
            overlay: true,
            overlayOptions: { anchor: "center", width: "86%", maxHeight: "88%", margin: 1 },
          },
        );
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      } finally {
        const run = active;
        active = undefined;
        ctx.ui.setStatus("hobot-btw", undefined);
        if (run) {
          await run.cleanup();
          ctx.ui.notify("Side-agent context deleted. Workspace and device changes were retained.", "info");
        }
      }
    },
  });

  return async () => {
    const run = active;
    active = undefined;
    if (run) await run.cleanup();
  };
}
