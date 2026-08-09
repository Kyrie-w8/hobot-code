import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { accessSync, constants, readFileSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { extname, isAbsolute, resolve } from "node:path";
import { pathToFileURL } from "node:url";

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

interface LspMessage {
  id?: number;
  method?: string;
  result?: unknown;
  error?: { code?: number; message?: string };
  params?: unknown;
}

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

class LspClient {
  private readonly child: ChildProcessWithoutNullStreams;
  private readonly pending = new Map<number, PendingRequest>();
  private readonly opened = new Map<string, { version: number; text: string }>();
  private readonly diagnostics = new Map<string, unknown>();
  private buffer = Buffer.alloc(0);
  private nextId = 1;
  private idleTimer?: ReturnType<typeof setTimeout>;
  private memoryTimer?: ReturnType<typeof setInterval>;
  private closed = false;
  private failure?: string;
  lastUsedAt = Date.now();

  constructor(
    readonly server: LspServerConfig,
    readonly root: string,
    private readonly config: LspConfig,
    private readonly onStopped: () => void,
  ) {
    this.child = spawn(server.command[0]!, server.command.slice(1), {
      cwd: root,
      env: process.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk: Buffer) => this.consume(chunk));
    let stderr = "";
    this.child.stderr.on("data", (chunk: Buffer) => {
      stderr = (stderr + chunk.toString("utf8")).slice(-2000);
    });
    this.child.on("error", (error) => this.fail(error.message));
    this.child.on("close", (code) => this.fail(stderr.trim() || `language server exited with ${code}`));
    this.armIdleTimer();
    this.memoryTimer = setInterval(() => {
      const pid = this.child.pid;
      if (!pid) return;
      const memory = processMemoryMiB(pid);
      if (memory !== undefined && memory > this.config.maxMemoryMiB) {
        this.fail(`language server exceeded ${this.config.maxMemoryMiB} MiB (RSS ${memory} MiB)`);
        this.child.kill("SIGKILL");
      }
    }, 2000);
    this.memoryTimer.unref?.();
  }

  async initialize(): Promise<void> {
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
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      if (headerEnd < 0) return;
      const headers = this.buffer.subarray(0, headerEnd).toString("ascii");
      const match = headers.match(/Content-Length:\s*(\d+)/i);
      if (!match) {
        this.fail("language server sent a message without Content-Length");
        return;
      }
      const length = Number(match[1]);
      const bodyStart = headerEnd + 4;
      if (this.buffer.length < bodyStart + length) return;
      const body = this.buffer.subarray(bodyStart, bodyStart + length).toString("utf8");
      this.buffer = this.buffer.subarray(bodyStart + length);
      try {
        this.handle(JSON.parse(body) as LspMessage);
      } catch {
        this.fail("language server sent invalid JSON");
        return;
      }
    }
  }

  private handle(message: LspMessage): void {
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
      if (params?.uri) this.diagnostics.set(params.uri, params.diagnostics ?? []);
    }
  }

  private write(message: Record<string, unknown>): void {
    if (this.closed) throw new Error(this.failure || "language server is closed");
    const body = JSON.stringify({ jsonrpc: "2.0", ...message });
    this.child.stdin.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
    this.touch();
  }

  private request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextId++;
    return new Promise((resolveRequest, rejectRequest) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        rejectRequest(new Error(`${method} timed out after ${this.config.requestTimeoutMs} ms`));
      }, this.config.requestTimeoutMs);
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
    this.onStopped();
  }

  async open(path: string): Promise<string> {
    const uri = pathToFileURL(path).href;
    const text = await readFile(path, "utf8");
    const existing = this.opened.get(uri);
    if (!existing) {
      this.opened.set(uri, { version: 1, text });
      this.notify("textDocument/didOpen", {
        textDocument: { uri, languageId: this.server.languageId, version: 1, text },
      });
    } else if (existing.text !== text) {
      const version = existing.version + 1;
      this.opened.set(uri, { version, text });
      this.notify("textDocument/didChange", {
        textDocument: { uri, version },
        contentChanges: [{ text }],
      });
    }
    this.touch();
    return uri;
  }

  async query(action: LspAction, path: string, line = 1, column = 1): Promise<unknown> {
    const uri = await this.open(path);
    const position = { line: Math.max(0, line - 1), character: Math.max(0, column - 1) };
    if (action === "diagnostics") {
      await new Promise((resolveWait) => setTimeout(resolveWait, this.config.diagnosticsWaitMs));
      return this.diagnostics.get(uri) ?? [];
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
      memoryMiB: pid ? processMemoryMiB(pid) : undefined,
      openedDocuments: this.opened.size,
      idleMs: Date.now() - this.lastUsedAt,
      failure: this.failure,
    };
  }

  async stop(_reason = "requested"): Promise<void> {
    if (this.closed) return;
    try {
      await this.request("shutdown", null);
      this.notify("exit", null);
    } catch {
      // Shutdown remains best effort.
    }
    this.child.kill("SIGTERM");
    this.fail("stopped");
  }
}

export class LspManager {
  private readonly clients = new Map<string, LspClient>();
  private lastFailure?: string;

  constructor(private config: LspConfig) {}

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
    if (existing) return existing;
    while (this.clients.size >= this.config.maxProcesses) {
      const oldest = [...this.clients.entries()].sort((left, right) => left[1].lastUsedAt - right[1].lastUsedAt)[0];
      if (!oldest) break;
      await oldest[1].stop("process limit");
      this.clients.delete(oldest[0]);
    }
    const client = new LspClient(server, root, this.config, () => this.clients.delete(key));
    this.clients.set(key, client);
    try {
      await client.initialize();
      return client;
    } catch (error) {
      this.clients.delete(key);
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
    const root = resolve(options.root);
    const path = resolve(root, options.path);
    if (path !== root && !path.startsWith(`${root}/`)) {
      throw new Error("LSP path must stay within the current workspace");
    }
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
