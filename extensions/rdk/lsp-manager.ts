import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { accessSync, constants, readFileSync } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { extname, isAbsolute, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { inspectResolvedPath, sanitizedChildEnv, terminateProcessGroup } from "./runtime-safety.mjs";

export interface LspServerConfig {
  id: string;
  extensions: string[];
  languageId: string;
  command: string[];
  initializationOptions?: unknown;
}

export interface LspConfig {
  schemaVersion: 1;
  enabled: boolean;
  maxProcesses: number;
  maxMemoryMiB: number;
  idleTimeoutMs: number;
  requestTimeoutMs: number;
  diagnosticsWaitMs: number;
  servers: LspServerConfig[];
}

export type LspAction = "hover" | "definition" | "references" | "symbols" | "diagnostics";

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

interface DiagnosticSnapshot {
  value: unknown[];
  entries: number;
  bytes: number;
}

interface LspMessage {
  id?: number | string | null;
  method?: string;
  result?: unknown;
  error?: { code?: number; message?: string };
  params?: unknown;
}

export const LSP_MAX_HEADER_BYTES = 16 * 1024;
export const LSP_MAX_MESSAGE_BYTES = 4 * 1024 * 1024;
export const LSP_MAX_DOCUMENT_BYTES = 2 * 1024 * 1024;
export const LSP_MAX_OPEN_DOCUMENT_BYTES = 8 * 1024 * 1024;
export const LSP_MAX_OPEN_DOCUMENTS = 256;
export const LSP_MAX_DIAGNOSTIC_URIS = 128;
export const LSP_MAX_DIAGNOSTIC_ENTRIES = 2048;
export const LSP_MAX_DIAGNOSTIC_BYTES = 1024 * 1024;
export const LSP_SHUTDOWN_TIMEOUT_MS = 1000;
const LSP_MAX_URI_CHARS = 4096;
const LSP_MAX_BUFFER_BYTES = LSP_MAX_HEADER_BYTES + 4 + LSP_MAX_MESSAGE_BYTES;
const LSP_MAX_OUTBOUND_BYTES = 2 * LSP_MAX_MESSAGE_BYTES;

function executableAvailable(command: string): boolean {
  const candidates = isAbsolute(command)
    ? [command]
    : (process.env.PATH ?? "").split(":").filter(Boolean).map((directory) => resolve(directory, command));
  return candidates.some((candidate) => {
    try {
      accessSync(candidate, constants.X_OK);
      return true;
    } catch {
      return false;
    }
  });
}

function processMemoryMiB(pid: number): number | undefined {
  try {
    const status = readFileSync(`/proc/${pid}/status`, "utf8");
    const match = status.match(/^VmRSS:\s+(\d+)\s+kB$/m);
    return match ? Math.round(Number(match[1]) / 1024) : undefined;
  } catch {
    return undefined;
  }
}

function processTreeMemoryMiB(pid: number, visited = new Set<number>()): number | undefined {
  if (visited.has(pid) || visited.size >= 128) return 0;
  visited.add(pid);
  const own = processMemoryMiB(pid);
  if (own === undefined) return undefined;
  let total = own;
  try {
    const children = readFileSync(`/proc/${pid}/task/${pid}/children`, "utf8")
      .trim().split(/\s+/).filter(Boolean).map(Number).filter(Number.isFinite);
    for (const child of children) total += processTreeMemoryMiB(child, visited) ?? 0;
  } catch {
    // Kernels without the children file still retain the parent RSS limit.
  }
  return total;
}

class LspClient {
  private readonly child: ChildProcessWithoutNullStreams;
  private readonly pending = new Map<number, PendingRequest>();
  private readonly opened = new Map<string, { version: number; text: string; bytes: number }>();
  private readonly opening = new Map<string, Promise<string>>();
  private readonly diagnostics = new Map<string, DiagnosticSnapshot>();
  private openQueue: Promise<void> = Promise.resolve();
  private buffer = Buffer.alloc(0);
  private openedBytes = 0;
  private diagnosticEntries = 0;
  private diagnosticBytes = 0;
  private nextId = 1;
  private idleTimer?: ReturnType<typeof setTimeout>;
  private memoryTimer?: ReturnType<typeof setInterval>;
  private initialization?: Promise<void>;
  private stopping?: Promise<void>;
  private closed = false;
  private failure?: string;
  lastUsedAt = Date.now();

  readonly server: LspServerConfig;
  readonly root: string;
  private readonly config: LspConfig;
  private readonly onStopped: (reason: string) => void;

  constructor(
    server: LspServerConfig,
    root: string,
    config: LspConfig,
    onStopped: (reason: string) => void,
  ) {
    this.server = server;
    this.root = root;
    this.config = config;
    this.onStopped = onStopped;
    this.child = spawn(server.command[0]!, server.command.slice(1), {
      cwd: root,
      env: sanitizedChildEnv(),
      detached: process.platform !== "win32",
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk: Buffer) => this.consume(chunk));
    let stderr = "";
    this.child.stderr.on("data", (chunk: Buffer) => {
      stderr = (stderr + chunk.toString("utf8")).slice(-2000);
    });
    this.child.stdin.on("error", (error) => this.fail(`language server stdin failed: ${error.message}`));
    this.child.on("error", (error) => this.fail(error.message));
    this.child.on("close", (code) => this.fail(stderr.trim() || `language server exited with ${code}`));
    this.armIdleTimer();
    this.memoryTimer = setInterval(() => {
      const pid = this.child.pid;
      if (!pid) return;
      const memory = processTreeMemoryMiB(pid);
      if (memory !== undefined && memory > this.config.maxMemoryMiB) {
        this.fail(`language server exceeded ${this.config.maxMemoryMiB} MiB (RSS ${memory} MiB)`);
      }
    }, 2000);
    this.memoryTimer.unref?.();
  }

  initialize(): Promise<void> {
    this.initialization ??= this.initializeOnce();
    return this.initialization;
  }

  private async initializeOnce(): Promise<void> {
    await this.request("initialize", {
      processId: process.pid,
      rootUri: pathToFileURL(this.root).href,
      capabilities: {
        textDocument: {
          hover: {},
          definition: {},
          references: {},
          documentSymbol: {},
          publishDiagnostics: { relatedInformation: true },
        },
      },
      initializationOptions: this.server.initializationOptions,
      workspaceFolders: [{ uri: pathToFileURL(this.root).href, name: this.root.split("/").at(-1) || "workspace" }],
    });
    this.notify("initialized", {});
  }

  private consume(chunk: Buffer): void {
    if (this.closed) return;
    if (chunk.length > LSP_MAX_BUFFER_BYTES || this.buffer.length + chunk.length > LSP_MAX_BUFFER_BYTES) {
      this.fail(`language server protocol buffer exceeds ${LSP_MAX_BUFFER_BYTES} bytes`);
      return;
    }
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      if (headerEnd < 0) {
        if (this.buffer.length > LSP_MAX_HEADER_BYTES) {
          this.fail(`language server header exceeds ${LSP_MAX_HEADER_BYTES} bytes`);
        }
        return;
      }
      if (headerEnd > LSP_MAX_HEADER_BYTES) {
        this.fail(`language server header exceeds ${LSP_MAX_HEADER_BYTES} bytes`);
        return;
      }
      const headers = this.buffer.subarray(0, headerEnd).toString("ascii");
      const matches = [...headers.matchAll(/^Content-Length:\s*(\d+)\s*$/gim)];
      if (matches.length !== 1) {
        this.fail("language server sent an invalid Content-Length header");
        return;
      }
      const length = Number(matches[0]![1]);
      if (!Number.isSafeInteger(length) || length < 0 || length > LSP_MAX_MESSAGE_BYTES) {
        this.fail(`language server message exceeds ${LSP_MAX_MESSAGE_BYTES} bytes`);
        return;
      }
      const bodyStart = headerEnd + 4;
      if (this.buffer.length < bodyStart + length) return;
      const body = this.buffer.subarray(bodyStart, bodyStart + length).toString("utf8");
      this.buffer = this.buffer.subarray(bodyStart + length);
      let message: LspMessage;
      try {
        message = JSON.parse(body) as LspMessage;
      } catch {
        this.fail("language server sent invalid JSON");
        return;
      }
      try {
        this.handle(message);
      } catch (error) {
        this.fail(error instanceof Error ? error.message : String(error));
        return;
      }
    }
  }

  private handle(message: LspMessage): void {
    if (message.method && message.id !== undefined && message.id !== null) {
      this.handleServerRequest(message);
      return;
    }
    if (typeof message.id === "number") {
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      clearTimeout(pending.timer);
      if (message.error) pending.reject(new Error(message.error.message || `LSP error ${message.error.code}`));
      else pending.resolve(message.result);
      return;
    }
    if (message.method === "textDocument/publishDiagnostics") {
      const params = message.params as { uri?: string; diagnostics?: unknown } | undefined;
      this.updateDiagnostics(params);
    }
  }

  private updateDiagnostics(params: { uri?: string; diagnostics?: unknown } | undefined): void {
    const uri = params?.uri;
    if (typeof uri !== "string" || uri.length === 0 || uri.length > LSP_MAX_URI_CHARS) return;

    // Only retain diagnostics for files that this client opened after workspace path validation.
    if (!this.opened.has(uri)) return;
    if (!Array.isArray(params?.diagnostics)) {
      throw new Error("language server diagnostics must be an array");
    }

    const value = params.diagnostics;
    const entries = value.length;
    if (entries > LSP_MAX_DIAGNOSTIC_ENTRIES) {
      throw new Error(`language server diagnostics exceed ${LSP_MAX_DIAGNOSTIC_ENTRIES} entries`);
    }
    const bytes = Buffer.byteLength(JSON.stringify(value));
    if (bytes > LSP_MAX_DIAGNOSTIC_BYTES) {
      throw new Error(`language server diagnostics exceed ${LSP_MAX_DIAGNOSTIC_BYTES} bytes`);
    }

    const previous = this.diagnostics.get(uri);
    if (entries === 0) {
      if (previous) {
        this.diagnostics.delete(uri);
        this.diagnosticEntries -= previous.entries;
        this.diagnosticBytes -= previous.bytes;
      }
      return;
    }
    const nextUris = this.diagnostics.size + (previous ? 0 : 1);
    const nextEntries = this.diagnosticEntries - (previous?.entries ?? 0) + entries;
    const nextBytes = this.diagnosticBytes - (previous?.bytes ?? 0) + bytes;
    if (nextUris > LSP_MAX_DIAGNOSTIC_URIS) {
      throw new Error(`language server diagnostics exceed ${LSP_MAX_DIAGNOSTIC_URIS} document URIs`);
    }
    if (nextEntries > LSP_MAX_DIAGNOSTIC_ENTRIES) {
      throw new Error(`language server diagnostics exceed ${LSP_MAX_DIAGNOSTIC_ENTRIES} total entries`);
    }
    if (nextBytes > LSP_MAX_DIAGNOSTIC_BYTES) {
      throw new Error(`language server diagnostics exceed ${LSP_MAX_DIAGNOSTIC_BYTES} total bytes`);
    }

    this.diagnostics.set(uri, { value, entries, bytes });
    this.diagnosticEntries = nextEntries;
    this.diagnosticBytes = nextBytes;
  }

  private handleServerRequest(message: LspMessage): void {
    const id = message.id!;
    let result: unknown;
    if (message.method === "workspace/configuration") {
      const items = (message.params as { items?: unknown[] } | undefined)?.items;
      result = Array.isArray(items) ? items.map(() => null) : [];
    } else if (message.method === "workspace/workspaceFolders") {
      result = [{ uri: pathToFileURL(this.root).href, name: this.root.split("/").at(-1) || "workspace" }];
    } else if ([
      "client/registerCapability",
      "client/unregisterCapability",
      "window/workDoneProgress/create",
      "window/showMessageRequest",
    ].includes(message.method)) {
      result = null;
    } else if (message.method === "workspace/applyEdit") {
      result = { applied: false, failureReason: "Hobot Code does not allow language servers to edit the workspace" };
    } else {
      this.write({ id, error: { code: -32601, message: `Unsupported language server request: ${message.method}` } });
      return;
    }
    this.write({ id, result });
  }

  private write(message: Record<string, unknown>): void {
    if (this.closed) throw new Error(this.failure || "language server is closed");
    const body = JSON.stringify({ jsonrpc: "2.0", ...message });
    const bodyBytes = Buffer.byteLength(body);
    if (bodyBytes > LSP_MAX_MESSAGE_BYTES) {
      throw new Error(`language server request exceeds ${LSP_MAX_MESSAGE_BYTES} bytes`);
    }
    const frame = Buffer.from(`Content-Length: ${bodyBytes}\r\n\r\n${body}`);
    if (this.child.stdin.writableLength + frame.length > LSP_MAX_OUTBOUND_BYTES) {
      throw new Error(`language server outbound buffer exceeds ${LSP_MAX_OUTBOUND_BYTES} bytes`);
    }
    this.child.stdin.write(frame, (error) => {
      if (error) this.fail(`language server stdin failed: ${error.message}`);
    });
    this.touch();
  }

  private request(method: string, params: unknown, timeoutMs = this.config.requestTimeoutMs): Promise<unknown> {
    const id = this.nextId++;
    return new Promise((resolveRequest, rejectRequest) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        rejectRequest(new Error(`${method} timed out after ${timeoutMs} ms`));
      }, timeoutMs);
      this.pending.set(id, { resolve: resolveRequest, reject: rejectRequest, timer });
      try {
        this.write({ id, method, params });
      } catch (error) {
        clearTimeout(timer);
        this.pending.delete(id);
        rejectRequest(error instanceof Error ? error : new Error(String(error)));
      }
    });
  }

  private notify(method: string, params: unknown): void {
    this.write({ method, params });
  }

  private touch(): void {
    this.lastUsedAt = Date.now();
    this.armIdleTimer();
  }

  private armIdleTimer(): void {
    if (this.idleTimer) clearTimeout(this.idleTimer);
    this.idleTimer = setTimeout(() => void this.stop("idle timeout"), this.config.idleTimeoutMs);
    this.idleTimer.unref?.();
  }

  private fail(reason: string): void {
    if (this.closed) return;
    this.failure = reason;
    this.closed = true;
    if (this.idleTimer) clearTimeout(this.idleTimer);
    if (this.memoryTimer) clearInterval(this.memoryTimer);
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new Error(reason));
    }
    this.pending.clear();
    this.buffer = Buffer.alloc(0);
    this.opened.clear();
    this.openedBytes = 0;
    this.diagnostics.clear();
    this.diagnosticEntries = 0;
    this.diagnosticBytes = 0;
    terminateProcessGroup(this.child, "SIGKILL");
    this.onStopped(reason);
  }

  private async serializeOpen<T>(operation: () => Promise<T>): Promise<T> {
    const previous = this.openQueue;
    let release!: () => void;
    this.openQueue = new Promise<void>((resolveQueue) => {
      release = resolveQueue;
    });
    await previous;
    try {
      return await operation();
    } finally {
      release();
    }
  }

  async open(path: string): Promise<string> {
    const uri = pathToFileURL(path).href;
    if (uri.length > LSP_MAX_URI_CHARS) throw new Error(`LSP document URI exceeds ${LSP_MAX_URI_CHARS} characters`);
    const inFlight = this.opening.get(uri);
    if (inFlight) return await inFlight;

    const operation = this.serializeOpen(() => this.openSerialized(path, uri));
    this.opening.set(uri, operation);
    try {
      return await operation;
    } finally {
      if (this.opening.get(uri) === operation) this.opening.delete(uri);
    }
  }

  private async openSerialized(path: string, uri: string): Promise<string> {
    if (this.closed) throw new Error(this.failure || "language server is closed");
    const existing = this.opened.get(uri);
    if (!existing && this.opened.size >= LSP_MAX_OPEN_DOCUMENTS) {
      throw new Error(`LSP open documents exceed ${LSP_MAX_OPEN_DOCUMENTS} files`);
    }
    const info = await stat(path);
    if (!info.isFile()) throw new Error("LSP path must identify a regular file");
    if (info.size > LSP_MAX_DOCUMENT_BYTES) {
      throw new Error(`LSP document exceeds ${LSP_MAX_DOCUMENT_BYTES} bytes`);
    }
    const contents = await readFile(path);
    if (contents.length > LSP_MAX_DOCUMENT_BYTES) {
      throw new Error(`LSP document exceeds ${LSP_MAX_DOCUMENT_BYTES} bytes`);
    }
    if (this.closed) throw new Error(this.failure || "language server is closed");
    const text = contents.toString("utf8");
    const nextOpenedBytes = this.openedBytes - (existing?.bytes ?? 0) + contents.length;
    if (nextOpenedBytes > LSP_MAX_OPEN_DOCUMENT_BYTES) {
      throw new Error(`LSP open documents exceed ${LSP_MAX_OPEN_DOCUMENT_BYTES} bytes`);
    }
    if (!existing) {
      this.notify("textDocument/didOpen", {
        textDocument: { uri, languageId: this.server.languageId, version: 1, text },
      });
      this.opened.set(uri, { version: 1, text, bytes: contents.length });
      this.openedBytes = nextOpenedBytes;
    } else if (existing.text !== text) {
      const version = existing.version + 1;
      this.notify("textDocument/didChange", {
        textDocument: { uri, version },
        contentChanges: [{ text }],
      });
      this.opened.set(uri, { version, text, bytes: contents.length });
      this.openedBytes = nextOpenedBytes;
    }
    this.touch();
    return uri;
  }

  async query(action: LspAction, path: string, line = 1, column = 1): Promise<unknown> {
    const uri = await this.open(path);
    const position = { line: Math.max(0, line - 1), character: Math.max(0, column - 1) };
    if (action === "diagnostics") {
      await new Promise((resolveWait) => setTimeout(resolveWait, this.config.diagnosticsWaitMs));
      return this.diagnostics.get(uri)?.value ?? [];
    }
    if (action === "hover") return await this.request("textDocument/hover", { textDocument: { uri }, position });
    if (action === "definition") return await this.request("textDocument/definition", { textDocument: { uri }, position });
    if (action === "references") {
      return await this.request("textDocument/references", {
        textDocument: { uri }, position, context: { includeDeclaration: true },
      });
    }
    return await this.request("textDocument/documentSymbol", { textDocument: { uri } });
  }

  status(): Record<string, unknown> {
    const pid = this.child.pid;
    return {
      server: this.server.id,
      pid,
      memoryMiB: pid ? processTreeMemoryMiB(pid) : undefined,
      openedDocuments: this.opened.size,
      openedDocumentBytes: this.openedBytes,
      diagnosticDocuments: this.diagnostics.size,
      diagnosticEntries: this.diagnosticEntries,
      diagnosticBytes: this.diagnosticBytes,
      idleMs: Date.now() - this.lastUsedAt,
      failure: this.failure,
    };
  }

  stop(_reason = "requested"): Promise<void> {
    if (this.closed) return Promise.resolve();
    this.stopping ??= this.stopOnce();
    return this.stopping;
  }

  private async stopOnce(): Promise<void> {
    try {
      await this.request("shutdown", null, LSP_SHUTDOWN_TIMEOUT_MS);
      this.notify("exit", null);
    } catch {
      // Shutdown remains best effort.
    }
    terminateProcessGroup(this.child, "SIGTERM");
    this.fail("stopped");
  }
}

export class LspManager {
  private readonly clients = new Map<string, LspClient>();
  private lastFailure?: string;

  private config: LspConfig;

  constructor(config: LspConfig) {
    this.config = config;
  }

  setConfig(config: LspConfig): void {
    this.config = config;
  }

  private serverFor(path: string): LspServerConfig {
    const extension = extname(path).toLowerCase();
    const server = this.config.servers.find((candidate) => candidate.extensions.includes(extension));
    if (!server) throw new Error(`No language server is configured for ${extension || "this file"}`);
    if (!executableAvailable(server.command[0]!)) {
      throw new Error(`Language server ${server.id} is configured but ${server.command[0]} is not installed`);
    }
    return server;
  }

  private async clientFor(server: LspServerConfig, root: string): Promise<LspClient> {
    const key = `${server.id}:${root}`;
    const existing = this.clients.get(key);
    if (existing) {
      await existing.initialize();
      return existing;
    }
    while (this.clients.size >= this.config.maxProcesses) {
      const oldest = [...this.clients.entries()].sort((left, right) => left[1].lastUsedAt - right[1].lastUsedAt)[0];
      if (!oldest) break;
      await oldest[1].stop("process limit");
      this.clients.delete(oldest[0]);
    }
    const client = new LspClient(server, root, this.config, (reason) => {
      if (this.clients.get(key) === client) this.clients.delete(key);
      if (reason !== "stopped") this.lastFailure = reason;
    });
    this.clients.set(key, client);
    try {
      await client.initialize();
      return client;
    } catch (error) {
      if (this.clients.get(key) === client) this.clients.delete(key);
      await client.stop("initialization failed");
      this.lastFailure = error instanceof Error ? error.message : String(error);
      throw error;
    }
  }

  async query(options: {
    action: LspAction;
    path: string;
    root: string;
    line?: number;
    column?: number;
  }): Promise<unknown> {
    if (!this.config.enabled) throw new Error("LSP support is disabled");
    const inspected = await inspectResolvedPath(options.root, options.path);
    if (!inspected.withinWorkspace) {
      throw new Error("LSP path must stay within the current workspace");
    }
    const root = inspected.workspaceRoot;
    const path = inspected.target;
    const server = this.serverFor(path);
    const client = await this.clientFor(server, root);
    return await client.query(options.action, path, options.line, options.column);
  }

  status(): Record<string, unknown> {
    return {
      enabled: this.config.enabled,
      limits: {
        maxProcesses: this.config.maxProcesses,
        maxMemoryMiB: this.config.maxMemoryMiB,
        idleTimeoutMs: this.config.idleTimeoutMs,
      },
      servers: this.config.servers.map((server) => ({
        id: server.id,
        command: server.command[0],
        installed: executableAvailable(server.command[0]!),
        extensions: server.extensions,
      })),
      running: [...this.clients.values()].map((client) => client.status()),
      lastFailure: this.lastFailure,
    };
  }

  async stopAll(): Promise<void> {
    const clients = [...this.clients.values()];
    this.clients.clear();
    await Promise.all(clients.map((client) => client.stop()));
  }
}
