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
  HStack,
  Input,
  Markdown,
  ScrollView,
  VStack,
  isViewportTUI,
  matchesKey,
  truncateToWidth,
  visibleWidth,
  type Component,
  type Focusable,
  type TUI,
  type TuiInputListener,
  type ViewportTUI,
} from "@earendil-works/pi-tui";

import { redactSensitiveText } from "./control-plane.mjs";
import { acquireSideAgentLease } from "./side-agent-lease.mjs";
import { resolveUserPaths } from "./user-paths.mjs";
import {
  applySideAgentEvent,
  buildSideAgentArgs,
  buildSideAgentParentContext,
  buildSideSessionSnapshot,
  createSideAgentEventState,
  enqueueSideAgentUiRequest,
  notifySideAgentListeners,
  parseSideAgentEvent,
  removeSideAgentUiRequest,
  resolveSideAgentUiTimeout,
  selectSideAgentParentEntries,
  sideAgentCommandResponseMatches,
  sideAgentFocusSwitchAllowed,
  sideAgentLeafBeforeRun,
  sideAgentPanelLayout,
  sideAgentPhaseAfterEvent,
  sideAgentPointerFocusTarget,
} from "./side-agent-session.mjs";

const MAX_QUESTION_CHARS = 32_000;
const MAX_STDERR_CHARS = 16_000;
const MAX_JSON_LINE_CHARS = 2_000_000;
const MAX_TRANSCRIPT_CHARS = 240_000;
const MAX_TRANSCRIPT_MESSAGES = 120;
const MAX_PARENT_CONTEXT_CHARS = 24_000;
const MAX_SIDE_UI_WAIT_MS = 120_000;
const SIDE_AGENT_TERM_GRACE_MS = 5_000;
const SIDE_AGENT_EXIT_TIMEOUT_MS = 8_000;
const SIDE_AGENT_SYSTEM_NOTE = `

## Ephemeral side-agent mode

You are an independent temporary agent created from the parent's latest fully settled conversation snapshot.
The parent's in-flight turn is intentionally excluded. Treat inherited messages as read-only background and work
only on prompts entered in this side conversation; never continue an unfinished parent task on your own.
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
  rpcOwnsTimeout?: boolean;
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
  private pendingPromptId: string | undefined;
  private pendingAbortId: string | undefined;
  private uiRequests: SideUiRequest[] = [];
  private uiRequestTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private cleanupPromise: Promise<void> | undefined;
  private readonly args: string[];
  private readonly cwd: string;
  private readonly parentSession: string | undefined;
  private readonly parentContext: string;

  constructor(
    id: string,
    initialQuestion: string,
    tempDir: string,
    sessionPath: string,
    args: string[],
    cwd: string,
    parentSession: string | undefined,
    parentContext: string,
  ) {
    this.id = id;
    this.initialQuestion = initialQuestion;
    this.tempDir = tempDir;
    this.sessionPath = sessionPath;
    this.args = args;
    this.cwd = cwd;
    this.parentSession = parentSession;
    this.parentContext = parentContext;
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

  get pendingUiRequest(): SideUiRequest | undefined {
    return this.uiRequests[0];
  }

  get pendingUiRequestCount(): number {
    return this.uiRequests.length;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private changed(): void {
    notifySideAgentListeners(this.listeners);
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

  private sendCommand(type: string, fields: Record<string, unknown> = {}): string | undefined {
    const id = `btw_${++this.requestId}`;
    return this.sendRaw({ id, type, ...fields }) ? id : undefined;
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
        this.clearUiRequests();
        if (this.terminating) this.phase = "closed";
        else {
          this.phase = "failed";
          this.addNotice(`Side-agent process exited unexpectedly with code ${code ?? "unknown"}.`);
        }
        this.settleFinished();
        this.changed();
      });
      this.phase = "idle";
      this.changed();
      this.sendPrompt(this.initialQuestion, this.parentContext);
    } catch (error) {
      this.phase = "failed";
      this.addNotice(error instanceof Error ? error.message : String(error));
      this.changed();
      this.settleFinished();
    }
  }

  sendPrompt(question: string, parentContext = ""): boolean {
    const trimmed = question.trim();
    if (!trimmed || trimmed.length > MAX_QUESTION_CHARS || !this.canPrompt) return false;
    this.resetTurnState();
    this.appendTranscript({ role: "user", text: trimmed });
    this.turnStartedAt = Date.now();
    this.phase = "running";
    const message = parentContext
      ? `Parent task snapshot (${parentContext.length} characters; read-only):\n${parentContext}\n\nSide task request (${trimmed.length} characters):\n${trimmed}`
      : trimmed;
    this.pendingPromptId = this.sendCommand("prompt", { message });
    this.changed();
    return Boolean(this.pendingPromptId);
  }

  abortTurn(): void {
    if (!this.isBusy || this.phase === "starting") return;
    this.phase = "aborting";
    this.pendingAbortId = this.sendCommand("abort");
    this.changed();
  }

  respondToUi(response: { value?: string; confirmed?: boolean; cancelled?: true }): void {
    const request = this.pendingUiRequest;
    if (!request) return;
    this.sendRaw({ type: "extension_ui_response", id: request.id, ...response });
    this.removeUiRequest(request.id);
    this.changed();
  }

  private setUiRequest(request: SideUiRequest): void {
    const next = enqueueSideAgentUiRequest(this.uiRequests, request) as SideUiRequest[];
    if (next === this.uiRequests) return;
    this.uiRequests = next;
    if (request.timeout && request.timeout > 0) {
      // Avoid racing RPC-owned timeouts; only timeout-less dialogs need a cancellation response.
      const timer = setTimeout(() => {
        if (!this.uiRequests.some((candidate) => candidate.id === request.id)) return;
        if (!request.rpcOwnsTimeout) {
          this.sendRaw({ type: "extension_ui_response", id: request.id, cancelled: true });
        }
        this.removeUiRequest(request.id);
        this.changed();
      }, request.timeout + (request.rpcOwnsTimeout ? 50 : 0));
      timer.unref?.();
      this.uiRequestTimers.set(request.id, timer);
    }
    this.changed();
  }

  private removeUiRequest(id: string): void {
    const timer = this.uiRequestTimers.get(id);
    if (timer) clearTimeout(timer);
    this.uiRequestTimers.delete(id);
    this.uiRequests = removeSideAgentUiRequest(this.uiRequests, id) as SideUiRequest[];
  }

  private clearUiRequests(): void {
    for (const timer of this.uiRequestTimers.values()) clearTimeout(timer);
    this.uiRequestTimers.clear();
    this.uiRequests = [];
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
        const timeoutPolicy = resolveSideAgentUiTimeout(event.timeout, MAX_SIDE_UI_WAIT_MS);
        this.setUiRequest({
          id: String(event.id),
          method: event.method,
          title: String(event.title ?? "Side agent request"),
          message: typeof event.message === "string" ? event.message : undefined,
          options: Array.isArray(event.options) ? event.options.map(String) : undefined,
          placeholder: typeof event.placeholder === "string" ? event.placeholder : event.prefill,
          ...timeoutPolicy,
        });
      }
      this.changed();
      return;
    }

    if (event.type === "response") {
      const promptResponse = sideAgentCommandResponseMatches(event, this.pendingPromptId, "prompt");
      const abortResponse = sideAgentCommandResponseMatches(event, this.pendingAbortId, "abort");
      if (promptResponse && event.success === false) {
        this.pendingPromptId = undefined;
        this.phase = "idle";
      }
      if (abortResponse) {
        this.pendingAbortId = undefined;
        if (event.success === false) this.phase = "running";
      }
      if (event.success === false) {
        const message = String(event.error ?? "RPC command failed");
        this.state = { ...this.state, errorMessage: message };
        this.addNotice(message);
      }
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
    } else {
      this.phase = sideAgentPhaseAfterEvent(this.phase, event) as SidePhase;
      if (event.type === "agent_settled") {
        this.pendingPromptId = undefined;
        this.pendingAbortId = undefined;
        this.clearUiRequests();
      }
    }
    this.changed();
  }

  cleanup(): Promise<void> {
    this.cleanupPromise ??= this.cleanupOnce();
    return this.cleanupPromise;
  }

  private async cleanupOnce(): Promise<void> {
    this.terminating = true;
    this.clearUiRequests();
    const child = this.process;
    let forceKillTimer: ReturnType<typeof setTimeout> | undefined;
    if (child && child.exitCode === null && child.signalCode === null) {
      this.sendCommand("abort");
      terminateSideAgent(child, "SIGTERM");
      forceKillTimer = setTimeout(() => {
        if (!this.finishedSettled && child.exitCode === null && child.signalCode === null) {
          try {
            terminateSideAgent(child, "SIGKILL");
          } catch {
            // The bounded wait below reports a process that cannot be terminated.
          }
        }
      }, SIDE_AGENT_TERM_GRACE_MS);
      forceKillTimer.unref?.();
    }
    if (!child) this.settleFinished();

    let exitTimeout: ReturnType<typeof setTimeout> | undefined;
    const exited = await Promise.race([
      this.finished.then(() => true),
      new Promise<false>((resolve) => {
        exitTimeout = setTimeout(() => resolve(false), SIDE_AGENT_EXIT_TIMEOUT_MS);
        exitTimeout.unref?.();
      }),
    ]);
    if (forceKillTimer) clearTimeout(forceKillTimer);
    if (exitTimeout) clearTimeout(exitTimeout);
    if (!exited) {
      void this.finished.then(() => rm(this.tempDir, { recursive: true, force: true })).catch(() => undefined);
      throw new Error(`Side-agent process did not exit within ${SIDE_AGENT_EXIT_TIMEOUT_MS / 1000} seconds`);
    }
    await rm(this.tempDir, { recursive: true, force: true });
  }
}

interface SplitViewportTui extends ViewportTUI {
  layoutRoot?: Component;
  getFocusedComponent?: () => Component | null;
  inputListeners?: Set<TuiInputListener>;
}

function prependTuiInputListener(tui: SplitViewportTui, listener: TuiInputListener): () => void {
  const unsubscribe = tui.addInputListener(listener);
  const listeners = tui.inputListeners ?? Reflect.ownKeys(tui)
    .map((key) => (tui as unknown as Record<PropertyKey, unknown>)[key])
    .find((value): value is Set<TuiInputListener> => value instanceof Set && value.has(listener));
  if (!(listeners instanceof Set)) return unsubscribe;

  // Pi consumes fullscreen SGR mouse events, so the non-consuming focus
  // observer must run first while preserving every existing listener's order.
  listeners.delete(listener);
  const existing = [...listeners];
  listeners.clear();
  listeners.add(listener);
  for (const existingListener of existing) listeners.add(existingListener);
  return unsubscribe;
}

class DynamicComponent implements Component {
  private readonly renderer: (width: number) => string[];

  constructor(renderer: (width: number) => string[]) {
    this.renderer = renderer;
  }

  render(width: number): string[] {
    return this.renderer(width);
  }

  invalidate(): void {}
}

class SideAgentCustomHost implements Component {
  private readonly sidePane: SideAgentPane;

  constructor(sidePane: SideAgentPane) {
    this.sidePane = sidePane;
  }

  render(_width: number): string[] {
    return [];
  }

  invalidate(): void {
    this.sidePane.invalidate();
  }

  dispose(): void {
    this.sidePane.dispose();
  }
}

class SideAgentSplitWorkspace {
  private readonly tui: SplitViewportTui;
  private readonly originalRoot: Component;
  private readonly splitRoot: HStack;
  private readonly mainFocus: Component | null;
  private readonly sidePane: SideAgentPane;
  private unsubscribePointerFocus?: () => void;
  private restored = false;

  private constructor(
    tui: SplitViewportTui,
    originalRoot: Component,
    splitRoot: HStack,
    mainFocus: Component | null,
    sidePane: SideAgentPane,
  ) {
    this.tui = tui;
    this.originalRoot = originalRoot;
    this.splitRoot = splitRoot;
    this.mainFocus = mainFocus;
    this.sidePane = sidePane;
  }

  static mount(tui: TUI, sidePane: SideAgentPane): SideAgentSplitWorkspace | undefined {
    if (
      !isViewportTUI(tui)
      || tui.mode !== "fullscreen"
      || typeof (tui as SplitViewportTui).getFocusedComponent !== "function"
      || tui.terminal.columns < 80
      || tui.terminal.rows < 7
    ) return undefined;
    const viewport = tui as SplitViewportTui;
    const originalRoot = viewport.layoutRoot;
    if (!originalRoot) return undefined;

    const mainFocus = viewport.getFocusedComponent?.() ?? null;
    const sideWidth = Math.floor(tui.terminal.columns / 2);
    const mainWidth = tui.terminal.columns - sideWidth;
    const splitRoot = new HStack([
      { component: originalRoot, basis: mainWidth, grow: 1, shrink: 1, minSize: 1 },
      { component: sidePane, basis: sideWidth, grow: 1, shrink: 1, minSize: 1 },
    ]);
    const workspace = new SideAgentSplitWorkspace(viewport, originalRoot, splitRoot, mainFocus, sidePane);
    workspace.unsubscribePointerFocus = prependTuiInputListener(viewport, (data) => {
      if (!workspace.canSwitchFocus()) return undefined;
      const target = sideAgentPointerFocusTarget(data, viewport.terminal.columns);
      if (target === "side") workspace.focusSide();
      else if (target === "main") workspace.focusMain();
      return undefined;
    });
    sidePane.setSplitMounted(true);
    viewport.setLayoutRoot(splitRoot);
    viewport.requestRender(true);
    return workspace;
  }

  private canSwitchFocus(): boolean {
    const focused = this.tui.getFocusedComponent?.() ?? null;
    return sideAgentFocusSwitchAllowed(focused, this.mainFocus, this.sidePane);
  }

  focusSide(): void {
    if (this.restored || !this.canSwitchFocus()) return;
    this.tui.setFocus(this.sidePane);
    this.tui.requestRender();
  }

  focusMain(): void {
    if (this.restored || !this.canSwitchFocus()) return;
    this.tui.setFocus(this.mainFocus);
    this.tui.requestRender();
  }

  restore(): void {
    if (this.restored) return;
    this.restored = true;
    this.unsubscribePointerFocus?.();
    this.unsubscribePointerFocus = undefined;
    this.sidePane.setSplitMounted(false);
    if (this.tui.layoutRoot === this.splitRoot) this.tui.setLayoutRoot(this.originalRoot);
    if (this.canSwitchFocus()) this.tui.setFocus(this.mainFocus);
    this.tui.requestRender(true);
  }
}

class SideAgentPane extends VStack implements Focusable {
  private readonly input = new Input();
  private readonly tui: TUI;
  private readonly theme: Theme;
  private readonly run: SideAgentRun;
  private readonly done: (result: "close") => void;
  private readonly focusMain: () => void;
  private readonly transcriptView: ScrollView;
  private _focused = false;
  private scrollOffset = 0;
  private splitMounted = false;
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
    focusMain: () => void,
  ) {
    super();
    this.tui = tui;
    this.theme = theme;
    this.run = run;
    this.done = done;
    this.focusMain = focusMain;
    this.input.onSubmit = (value) => this.submit(value);
    this.transcriptView = new ScrollView(
      new DynamicComponent((width) => this.renderSplitTranscript(width)),
      {
        follow: "end",
        overscroll: "contain",
        scrollbar: "auto",
        scrollbarStyle: (text) => theme.fg("accent", text),
      },
    );
    this.addChild(new DynamicComponent((width) => this.renderHeader(width)), {
      basis: 1,
      grow: 0,
      shrink: 0,
      minSize: 1,
    });
    this.addChild(this.transcriptView, { basis: 0, grow: 1, shrink: 1, minSize: 1 });
    this.addChild(new DynamicComponent((width) => this.renderInputArea(width)), {
      basis: 4,
      grow: 0,
      shrink: 0,
      minSize: 4,
    });
    this.unsubscribe = run.subscribe(() => {
      if (!this.splitMounted) this.scrollOffset = Number.MAX_SAFE_INTEGER;
      this.inputError = "";
      this.syncTicker();
      this.tui.requestRender();
    });
    this.syncTicker();
  }

  setSplitMounted(value: boolean): void {
    this.splitMounted = value;
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
    if (this.run.sendPrompt(trimmed)) {
      this.input.setValue("");
      this.transcriptView.scrollToEnd();
    }
  }

  handleInput(data: string): void {
    if (matchesKey(data, "ctrl+shift+left")) {
      this.focusMain();
      return;
    }
    if (matchesKey(data, "ctrl+d")) {
      this.done("close");
      return;
    }

    const request = this.run.pendingUiRequest;
    if (request?.method === "confirm") {
      if (data.toLowerCase() === "y" || matchesKey(data, "y") || matchesKey(data, "shift+y")) {
        this.run.respondToUi({ confirmed: true });
        this.input.setValue("");
        return;
      }
      if (data.toLowerCase() === "n" || matchesKey(data, "n") || matchesKey(data, "shift+n")) {
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
    if (matchesKey(data, "pageup") || matchesKey(data, "ctrl+pageup")) {
      const amount = Math.max(1, this.transcriptView.viewportHeight - 2);
      if (this.splitMounted) this.transcriptView.scrollBy(-amount);
      else this.scrollOffset = Math.max(0, this.scrollOffset - 8);
      this.tui.requestRender();
      return;
    }
    if (matchesKey(data, "pagedown") || matchesKey(data, "ctrl+pagedown")) {
      const amount = Math.max(1, this.transcriptView.viewportHeight - 2);
      if (this.splitMounted) this.transcriptView.scrollBy(amount);
      else this.scrollOffset += 8;
      this.tui.requestRender();
      return;
    }
    if (!this.input.getValue() && matchesKey(data, "up")) {
      if (this.splitMounted) this.transcriptView.scrollBy(-1);
      else this.scrollOffset = Math.max(0, this.scrollOffset - 1);
      this.tui.requestRender();
      return;
    }
    if (!this.input.getValue() && matchesKey(data, "down")) {
      if (this.splitMounted) this.transcriptView.scrollBy(1);
      else this.scrollOffset += 1;
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

  private renderHeader(width: number): string[] {
    const th = this.theme;
    const layout = sideAgentPanelLayout(width, this.tui.terminal.rows);
    const request = this.run.pendingUiRequest;
    const displayPhase = request
      ? `${request.method === "confirm" ? "approval" : "input"}${this.run.pendingUiRequestCount > 1 ? ` 1/${this.run.pendingUiRequestCount}` : ""}`
      : this.run.phase;
    const phaseColor = request
      ? "warning"
      : this.run.phase === "idle"
        ? "success"
        : this.run.phase === "failed"
          ? "error"
          : this.run.phase === "aborting"
            ? "warning"
            : "accent";
    const elapsedStart = this.run.isBusy ? this.run.turnStartedAt : this.run.startedAt;
    const elapsed = Math.max(0, Math.round((Date.now() - elapsedStart) / 1000));
    if (layout.compact) {
      const status = th.fg(phaseColor as any, `BTW ${displayPhase} ${this.focused ? "side" : "main"} ${elapsed}s`);
      const clipped = truncateToWidth(status, layout.panelWidth, "", true);
      return [clipped + " ".repeat(Math.max(0, layout.panelWidth - visibleWidth(clipped)))];
    }
    const title = ` BTW side agent | ${displayPhase} | ${this.focused ? "side active" : "main active"} | ${elapsed}s `;
    const titleText = truncateToWidth(title, layout.innerWidth, layout.innerWidth >= 3 ? "..." : "", true);
    const titleRule = "─".repeat(Math.max(0, layout.innerWidth - visibleWidth(titleText)));
    return [
      th.fg("border", "╭")
        + th.fg(phaseColor as any, titleText)
        + th.fg("border", `${titleRule}╮`),
    ];
  }

  private renderInputArea(width: number): string[] {
    const th = this.theme;
    const layout = sideAgentPanelLayout(width, this.tui.terminal.rows);
    if (layout.compact) return Array.from({ length: 4 }, () => " ".repeat(layout.panelWidth));
    const border = (value: string) => th.fg("border", value);
    const pad = (value: string) => {
      const clipped = truncateToWidth(value, layout.innerWidth, layout.innerWidth >= 3 ? "..." : "", true);
      return clipped + " ".repeat(Math.max(0, layout.innerWidth - visibleWidth(clipped)));
    };
    const frame = (value: string) => `${border("│")}${pad(value)}${border("│")}`;
    const request = this.run.pendingUiRequest;
    let inputLabel = request ? " Reply: " : " You: ";
    if (layout.innerWidth < 10) inputLabel = request ? "?" : ">";
    const inputWidth = Math.max(1, layout.innerWidth - visibleWidth(inputLabel));
    const inputLine = this.input.render(inputWidth)[0] ?? "";
    const usage = [
      `${this.run.state.turns} response${this.run.state.turns === 1 ? "" : "s"}`,
      `in ${formatTokens(this.run.state.inputTokens)}`,
      `out ${formatTokens(this.run.state.outputTokens)}`,
      this.run.state.cacheReadTokens ? `cache ${formatTokens(this.run.state.cacheReadTokens)}` : undefined,
    ].filter(Boolean).join(" | ");
    const help = request
      ? request.method === "confirm"
        ? "Y allow | N deny | Ctrl+Shift+Left main | Ctrl+D close"
        : "Enter reply | Ctrl+Shift+Left main | Ctrl+D close"
      : this.run.isBusy
        ? "Ctrl+Shift+Left main | Esc abort | wheel scroll | Ctrl+D close"
        : "Enter send | Ctrl+Shift+Left main | wheel/Ctrl+PgUp/PgDn scroll | Ctrl+D close";
    return [
      border("├") + border("─".repeat(layout.innerWidth)) + border("┤"),
      frame(`${th.fg("accent", inputLabel)}${inputLine}`),
      frame(th.fg("dim", ` ${usage} | ${help}`)),
      border(`╰${"─".repeat(layout.innerWidth)}╯`),
    ];
  }

  private renderSplitTranscript(width: number): string[] {
    const th = this.theme;
    const layout = sideAgentPanelLayout(width, this.tui.terminal.rows);
    if (layout.compact) return [truncateToWidth("BTW", layout.panelWidth, "", true)];
    const border = (value: string) => th.fg("border", value);
    const pad = (value: string) => {
      const clipped = truncateToWidth(value, layout.innerWidth, layout.innerWidth >= 3 ? "..." : "", true);
      return clipped + " ".repeat(Math.max(0, layout.innerWidth - visibleWidth(clipped)));
    };
    const content = this.transcriptLines(layout.innerWidth);
    const minimumRows = Math.max(1, this.tui.terminal.rows - 5);
    while (content.length < minimumRows) content.push("");
    return content.map((line) => `${border("│")}${pad(line)}${border("│")}`);
  }

  render(width: number): string[] {
    const th = this.theme;
    const layout = sideAgentPanelLayout(width, this.tui.terminal.rows);
    const { innerWidth } = layout;
    if (layout.compact) return this.renderHeader(width);
    const border = (value: string) => th.fg("border", value);
    const pad = (value: string) => {
      const clipped = truncateToWidth(value, innerWidth, innerWidth >= 3 ? "..." : "", true);
      return clipped + " ".repeat(Math.max(0, innerWidth - visibleWidth(clipped)));
    };

    const content = this.transcriptLines(innerWidth);
    const maxContentLines = layout.contentRows;
    const maxOffset = Math.max(0, content.length - maxContentLines);
    this.scrollOffset = Math.min(this.scrollOffset, maxOffset);
    const visible = content.slice(this.scrollOffset, this.scrollOffset + maxContentLines);
    const result = this.renderHeader(width);
    for (const line of visible) result.push(border("│") + pad(line) + border("│"));
    while (result.length < maxContentLines + 1) result.push(border("│") + pad("") + border("│"));
    result.push(...this.renderInputArea(width));
    return result;
  }

  invalidate(): void {
    super.invalidate();
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

async function createSideAgentRun(
  pi: ExtensionAPI,
  args: string,
  ctx: ExtensionCommandContext,
  parentEntries: unknown[],
  parentContext: string,
): Promise<SideAgentRun> {
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
      entries: parentEntries,
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
    return new SideAgentRun(id, question, tempDir, sessionPath, childArgs, ctx.cwd, parentSession, parentContext);
  } catch (error) {
    await rm(tempDir, { recursive: true, force: true });
    throw error;
  }
}

export function registerSideAgent(pi: ExtensionAPI): () => Promise<void> {
  let active: SideAgentRun | undefined;
  let activeLease: SideAgentLease | undefined;
  let activeFocus: { focusSide: () => void; focusMain: () => void } | undefined;
  let activeWorkspace: SideAgentSplitWorkspace | undefined;
  let parentRunActive = false;
  let lastSettledLeafId: string | undefined;
  let transitioning = false;
  let disposed = false;

  pi.on("session_start", async (_event, ctx) => {
    parentRunActive = false;
    lastSettledLeafId = ctx.sessionManager.getLeafId() ?? undefined;
  });

  pi.on("before_agent_start", async (_event, ctx) => {
    if (!parentRunActive) {
      // Pi emits this before the submitted user message is persisted.
      lastSettledLeafId = ctx.sessionManager.getLeafId() ?? undefined;
    }
  });

  pi.on("agent_start", async (_event, ctx) => {
    if (!parentRunActive && !lastSettledLeafId) {
      lastSettledLeafId = sideAgentLeafBeforeRun(ctx.sessionManager.getBranch());
    }
    parentRunActive = true;
  });

  pi.on("agent_settled", async (_event, ctx) => {
    if (!ctx.isIdle()) {
      parentRunActive = true;
      return;
    }
    lastSettledLeafId = ctx.sessionManager.getLeafId() ?? undefined;
    parentRunActive = false;
  });

  pi.registerShortcut("ctrl+shift+right", {
    description: "Focus the open /btw side agent",
    handler: async (ctx) => {
      if (!activeFocus) {
        ctx.ui.notify("No /btw side agent is open.", "info");
        return;
      }
      activeFocus.focusSide();
    },
  });

  pi.registerCommand("btw", {
    description: "Open a private multi-turn agent with a snapshot of the current context",
    handler: async (args, ctx) => {
      if (ctx.mode !== "tui") {
        ctx.ui.notify("/btw requires the interactive TUI", "error");
        return;
      }
      if (active || transitioning || disposed) {
        ctx.ui.notify("A /btw side agent is already open. Close it before starting another.", "warning");
        return;
      }

      transitioning = true;
      let run: SideAgentRun | undefined;
      let lease: SideAgentLease | undefined;
      let pane: SideAgentPane | undefined;
      let workspace: SideAgentSplitWorkspace | undefined;
      let overlayHandle: { focus: () => void; unfocus: () => void } | undefined;
      let overlayTui: SplitViewportTui | undefined;
      let overlayMainFocus: Component | null = null;
      const overlayFocusCanSwitch = () => {
        if (!overlayTui || typeof overlayTui.getFocusedComponent !== "function") return false;
        return sideAgentFocusSwitchAllowed(overlayTui.getFocusedComponent(), overlayMainFocus, pane);
      };
      try {
        const currentEntries = ctx.sessionManager.getBranch();
        const parentEntries = selectSideAgentParentEntries({
          currentEntries,
          settledEntries: lastSettledLeafId ? ctx.sessionManager.getBranch(lastSettledLeafId) : [],
          parentRunActive,
          runtimeIdle: ctx.isIdle(),
        });
        const parentContext = redactSensitiveText(buildSideAgentParentContext({
          currentEntries,
          inheritedEntries: parentEntries,
          maximumChars: MAX_PARENT_CONTEXT_CHARS,
        }), MAX_PARENT_CONTEXT_CHARS);
        lease = await acquireSideAgentLease({
          registryDir: join(resolveUserPaths().stateRoot, "side-agent-leases"),
        }) as SideAgentLease;
        activeLease = lease;
        if (disposed) return;
        run = await createSideAgentRun(pi, String(args ?? ""), ctx, parentEntries, parentContext);
        active = run;
        if (disposed) return;
        ctx.ui.setStatus("hobot-btw", `btw: open ${lease.activeCount}/${lease.limit}`);
        run.start();
        await ctx.ui.custom<"close">(
          (tui, theme, _keybindings, done) => {
            overlayTui = tui as SplitViewportTui;
            overlayMainFocus = overlayTui.getFocusedComponent?.() ?? null;
            pane = new SideAgentPane(
              tui,
              theme,
              run,
              (result) => {
                workspace?.restore();
                done(result);
              },
              () => {
                if (workspace) workspace.focusMain();
                else if (overlayFocusCanSwitch()) overlayHandle?.unfocus();
              },
            );
            workspace = SideAgentSplitWorkspace.mount(tui, pane);
            activeWorkspace = workspace;
            activeFocus = {
              focusSide: () => {
                if (workspace) workspace.focusSide();
                else if (overlayFocusCanSwitch()) overlayHandle?.focus();
              },
              focusMain: () => {
                if (workspace) workspace.focusMain();
                else if (overlayFocusCanSwitch()) overlayHandle?.unfocus();
              },
            };
            return workspace ? new SideAgentCustomHost(pane) : pane;
          },
          {
            overlay: true,
            overlayOptions: () => workspace
              ? { nonCapturing: true, visible: () => false }
              : {
                  anchor: "right-center",
                  width: "50%",
                  maxHeight: "100%",
                  margin: 0,
                  nonCapturing: true,
                },
            onHandle: (handle) => {
              overlayHandle = handle;
              if (!workspace && overlayFocusCanSwitch()) handle.unfocus();
            },
          },
        );
      } catch (error) {
        if (!disposed) {
          try {
            ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
          } catch {
            // The TUI may already be tearing down; resource cleanup still runs below.
          }
        }
      } finally {
        workspace?.restore();
        if (activeWorkspace === workspace) activeWorkspace = undefined;
        if (pane) activeFocus = undefined;
        if (active === run) active = undefined;
        if (activeLease === lease) activeLease = undefined;
        const [cleanupResult] = await Promise.allSettled([run?.cleanup() ?? Promise.resolve()]);
        const [releaseResult] = await Promise.allSettled([lease?.release() ?? Promise.resolve()]);
        transitioning = false;

        if (!disposed) {
          const notify = (message: string, level: "info" | "warning") => {
            try {
              ctx.ui.notify(message, level);
            } catch {
              // UI reporting must not interfere with resource lifecycle.
            }
          };
          try {
            ctx.ui.setStatus("hobot-btw", undefined);
          } catch {
            // The session may have closed while cleanup was in progress.
          }
          if (run && cleanupResult.status === "fulfilled") {
            notify("Side-agent conversation deleted. Workspace and device changes were retained.", "info");
          }
          if (cleanupResult.status === "rejected") {
            notify(`Side-agent cleanup failed: ${String(cleanupResult.reason)}`, "warning");
          }
          if (releaseResult.status === "rejected") {
            notify(`Side-agent capacity release failed: ${String(releaseResult.reason)}`, "warning");
          }
        }
      }
    },
  });

  return async () => {
    disposed = true;
    activeWorkspace?.restore();
    activeWorkspace = undefined;
    activeFocus = undefined;
    const run = active;
    const lease = activeLease;
    active = undefined;
    activeLease = undefined;
    await Promise.allSettled([run?.cleanup() ?? Promise.resolve()]);
    await Promise.allSettled([lease?.release() ?? Promise.resolve()]);
  };
}
