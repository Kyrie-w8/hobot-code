let buffer = Buffer.alloc(0);
let initializeId;
let initialized = false;
let initializeCount = 0;
let openedDocuments = 0;
let duplicateDidOpen = 0;
const openedUris = new Set();
const emitDiagnostics = process.argv.includes("--diagnostics");
const ignoreShutdown = process.argv.includes("--ignore-shutdown");
const diagnosticCount = Number(process.argv.find((value) => value.startsWith("--diagnostic-count="))?.split("=")[1] ?? 0);
const diagnosticMessageBytes = Number(process.argv.find((value) => value.startsWith("--diagnostic-message-bytes="))?.split("=")[1] ?? 0);

function send(message) {
  const body = JSON.stringify({ jsonrpc: "2.0", ...message });
  process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
}

function handle(message) {
  if (message.id === "server-config" && !message.method) {
    if (!Array.isArray(message.result) || message.result.length !== 1) process.exit(3);
    initialized = true;
    send({ id: initializeId, result: { capabilities: { hoverProvider: true, documentSymbolProvider: true } } });
    return;
  }
  if (message.method === "initialize") {
    initializeCount += 1;
    initializeId = message.id;
    send({
      id: "server-config",
      method: "workspace/configuration",
      params: { items: [{ section: "hobot" }] },
    });
    return;
  }
  if (message.method === "initialized") return;
  if (!initialized && message.method) {
    if (message.id !== undefined) send({ id: message.id, error: { code: -32002, message: "server not initialized" } });
    return;
  }
  if (message.method === "textDocument/didOpen") {
    openedDocuments += 1;
    const uri = message.params?.textDocument?.uri;
    if (openedUris.has(uri)) duplicateDidOpen += 1;
    else openedUris.add(uri);
    if (emitDiagnostics && uri) {
      send({ method: "textDocument/publishDiagnostics", params: { uri: "file:///etc/passwd", diagnostics: [{ message: "foreign" }] } });
      send({ method: "textDocument/publishDiagnostics", params: { uri: "https://example.invalid/source.ts", diagnostics: [{ message: "remote" }] } });
      send({ method: "textDocument/publishDiagnostics", params: { uri, diagnostics: [{ message: "fixture diagnostic", severity: 2 }] } });
    } else if (diagnosticCount > 0 && uri) {
      send({ method: "textDocument/publishDiagnostics", params: { uri, diagnostics: Array.from({ length: diagnosticCount }, () => ({ message: "entry" })) } });
    } else if (diagnosticMessageBytes > 0 && uri) {
      send({ method: "textDocument/publishDiagnostics", params: { uri, diagnostics: [{ message: "x".repeat(diagnosticMessageBytes) }] } });
    }
    return;
  }
  if (message.method === "textDocument/hover" || message.method === "textDocument/documentSymbol") {
    send({
      id: message.id,
      result: { method: message.method, initializeCount, openedDocuments, duplicateDidOpen },
    });
    return;
  }
  if (message.method === "shutdown") {
    if (ignoreShutdown) return;
    send({ id: message.id, result: null });
    return;
  }
  if (message.method === "exit") process.exit(0);
}

process.stdin.on("data", (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  while (true) {
    const headerEnd = buffer.indexOf("\r\n\r\n");
    if (headerEnd < 0) return;
    const headers = buffer.subarray(0, headerEnd).toString("ascii");
    const match = headers.match(/Content-Length:\s*(\d+)/i);
    if (!match) process.exit(2);
    const length = Number(match[1]);
    const bodyStart = headerEnd + 4;
    if (buffer.length < bodyStart + length) return;
    const body = buffer.subarray(bodyStart, bodyStart + length).toString("utf8");
    buffer = buffer.subarray(bodyStart + length);
    handle(JSON.parse(body));
  }
});
