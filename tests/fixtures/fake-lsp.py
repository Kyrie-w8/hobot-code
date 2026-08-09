import json
import sys


def send(message):
    body = json.dumps({"jsonrpc": "2.0", **message}, separators=(",", ":")).encode()
    sys.stdout.buffer.write(f"Content-Length: {len(body)}\r\n\r\n".encode() + body)
    sys.stdout.buffer.flush()


while True:
    headers = {}
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            sys.exit(0)
        if line == b"\r\n":
            break
        name, value = line.decode().split(":", 1)
        headers[name.lower()] = value.strip()
    message = json.loads(sys.stdin.buffer.read(int(headers["content-length"])))
    method = message.get("method")
    if method == "initialize":
        send({"id": message["id"], "result": {"capabilities": {"hoverProvider": True}}})
    elif method == "shutdown":
        send({"id": message["id"], "result": None})
    elif method == "textDocument/didOpen":
        send({
            "method": "textDocument/publishDiagnostics",
            "params": {
                "uri": message["params"]["textDocument"]["uri"],
                "diagnostics": [{"severity": 2, "message": "fixture diagnostic"}],
            },
        })
    elif method == "textDocument/hover":
        send({"id": message["id"], "result": {"contents": {"kind": "plaintext", "value": "fixture hover"}}})
    elif method in ("textDocument/definition", "textDocument/references"):
        send({"id": message["id"], "result": []})
    elif method == "textDocument/documentSymbol":
        send({"id": message["id"], "result": [{"name": "fixtureSymbol", "kind": 12}]})
