#!/usr/bin/env python3
"""Exercise packaged Pi provider adapters through agentd's Unix-socket broker.

This is a release acceptance test for Linux ARM64 boards. It uses only an
isolated temporary home, fake credentials, and a localhost mock gateway; the
installed Hobot Code runtime and its task history are never touched.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import platform
import pty
import re
import shlex
import shutil
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import threading
import time
import termios
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from pathlib import PurePosixPath
from typing import Any


PROTOCOL = 1
MAX_RPC_RESPONSE = 8 * 1024 * 1024
MAX_MANIFEST_BYTES = 4 * 1024 * 1024
MAX_MANIFEST_FILES = 4096
MANIFEST_LINE = re.compile(r"^([0-9a-f]{64})  ([^\x00\r\n]+)$")
FAKE_CREDENTIALS = {
    "anthropic-test": "hobot-fake-anthropic",
    "chat-test": "hobot-fake-chat",
    "responses-test": "hobot-fake-responses",
}
PROVIDER_ENV = {
    "anthropic-test": "HOBOT_CODE_PROVIDER_KEY_ANTHROPIC_TEST",
    "chat-test": "HOBOT_CODE_PROVIDER_KEY_CHAT_TEST",
    "responses-test": "HOBOT_CODE_PROVIDER_KEY_RESPONSES_TEST",
}
PROVIDER_API = {
    "anthropic-test": "anthropic-messages",
    "chat-test": "openai-completions",
    "responses-test": "openai-responses",
}
APPROVAL_ALLOW_ONCE = "Allow once"
RUNTIME_PROBE_CHECKS = {
    "rpc-lifecycle", "model-selection", "tool-call", "tool-result", "continuation", "settled",
    "parallel-tools", "invalid-argument-recovery", "thinking-stream", "approval-flow", "image-input",
    "context-compaction", "interrupted-session-recovery",
}
TUI_ACCEPTANCE_CHECKS = (
    "ordinary-user-tui",
    "chinese-input",
    "thinking-stream",
    "editor-edit",
    "persistent-detach",
)
TUI_COLUMNS = 140
TUI_ROWS = 42
MAX_PTY_CAPTURE_BYTES = 1024 * 1024


class VerificationError(RuntimeError):
    pass


class GatewayState:
    def __init__(self) -> None:
        self.received = {provider: threading.Event() for provider in FAKE_CREDENTIALS}
        self.release = {provider: threading.Event() for provider in FAKE_CREDENTIALS}
        self.lock = threading.Lock()
        self.requests: list[dict[str, str]] = []
        self.errors: list[str] = []

    def fail(self, message: str) -> None:
        with self.lock:
            self.errors.append(message)


def anthropic_stream(text: str = "OK", input_tokens: int = 1) -> bytes:
    events = [
        ("message_start", {"type": "message_start", "message": {"id": "msg_hobot_test", "type": "message", "role": "assistant", "content": [], "model": "anthropic-test", "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": input_tokens, "output_tokens": 0}}}),
        ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}}),
        ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": text}}),
        ("content_block_stop", {"type": "content_block_stop", "index": 0}),
        ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 1}}),
        ("message_stop", {"type": "message_stop"}),
    ]
    return b"".join(
        f"event: {name}\ndata: {json.dumps(payload, separators=(',', ':'))}\n\n".encode()
        for name, payload in events
    )


def anthropic_thinking_stream(thinking: str, text: str, input_tokens: int = 1) -> bytes:
    events = [
        ("message_start", {"type": "message_start", "message": {"id": "msg_hobot_thinking", "type": "message", "role": "assistant", "content": [], "model": "anthropic-test", "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": input_tokens, "output_tokens": 0}}}),
        ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "thinking", "thinking": "", "signature": ""}}),
        ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "thinking_delta", "thinking": thinking}}),
        ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "signature_delta", "signature": "hobot-test-signature"}}),
        ("content_block_stop", {"type": "content_block_stop", "index": 0}),
        ("content_block_start", {"type": "content_block_start", "index": 1, "content_block": {"type": "text", "text": ""}}),
        ("content_block_delta", {"type": "content_block_delta", "index": 1, "delta": {"type": "text_delta", "text": text}}),
        ("content_block_stop", {"type": "content_block_stop", "index": 1}),
        ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 2}}),
        ("message_stop", {"type": "message_stop"}),
    ]
    return b"".join(
        f"event: {name}\ndata: {json.dumps(payload, separators=(',', ':'))}\n\n".encode()
        for name, payload in events
    )


def anthropic_tool_stream(calls: list[tuple[str, str, dict[str, str]]] | None = None) -> bytes:
    if calls is None:
        calls = [("toolu_hobot_rpc", "bash", {"command": "printf 'HOBOT_RPC_EXECUTED\\n' >> rpc-proof.txt"})]
    events: list[tuple[str, dict[str, Any]]] = [
        ("message_start", {"type": "message_start", "message": {"id": "msg_hobot_tool", "type": "message", "role": "assistant", "content": [], "model": "anthropic-test", "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 0}}}),
    ]
    for index, (call_id, name, arguments) in enumerate(calls):
        events.extend([
            ("content_block_start", {"type": "content_block_start", "index": index, "content_block": {"type": "tool_use", "id": call_id, "name": name, "input": {}}}),
            ("content_block_delta", {"type": "content_block_delta", "index": index, "delta": {"type": "input_json_delta", "partial_json": json.dumps(arguments, separators=(",", ":"))}}),
            ("content_block_stop", {"type": "content_block_stop", "index": index}),
        ])
    events.extend([
        ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "tool_use", "stop_sequence": None}, "usage": {"output_tokens": len(calls)}}),
        ("message_stop", {"type": "message_stop"}),
    ])
    return b"".join(
        f"event: {name}\ndata: {json.dumps(payload, separators=(',', ':'))}\n\n".encode()
        for name, payload in events
    )


def anthropic_response(payload: dict[str, Any], state: GatewayState) -> bytes:
    messages = payload.get("messages")
    if not isinstance(messages, list) or not messages:
        raise VerificationError("Anthropic request omitted messages")
    last = messages[-1]
    if not isinstance(last, dict):
        raise VerificationError("Anthropic request has an invalid final message")
    content = last.get("content")
    encoded_history = json.dumps(messages, separators=(",", ":"), ensure_ascii=True)
    estimated_input_tokens = max(1, min(1_000_000, len(encoded_history.encode("utf-8")) // 4))

    def text_response(text: str = "OK") -> bytes:
        return anthropic_stream(text, estimated_input_tokens)

    if isinstance(content, list) and any(isinstance(item, dict) and item.get("type") == "tool_result" for item in content):
        tool_result_ids = {
            item.get("tool_use_id") for item in content
            if isinstance(item, dict) and item.get("type") == "tool_result" and isinstance(item.get("tool_use_id"), str)
        }
        if tool_result_ids == {"toolu_hobot_rpc"}:
            return text_response("HOBOT_RPC_TOOL_OK")
        if tool_result_ids == {"toolu_runtime_basic"}:
            return text_response("HOBOT_RUNTIME_PROBE_COMPLETE")
        if tool_result_ids == {"toolu_runtime_parallel_a", "toolu_runtime_parallel_b"}:
            return text_response("HOBOT_RUNTIME_PARALLEL_COMPLETE")
        if tool_result_ids == {"toolu_runtime_recovery_1"}:
            return anthropic_tool_stream([("toolu_runtime_recovery_2", "hobot_runtime_probe", {"stage": "recovery", "nonce": "repaired-after-error"})])
        if tool_result_ids == {"toolu_runtime_recovery_2"}:
            return text_response("HOBOT_RUNTIME_RECOVERY_COMPLETE")
        if tool_result_ids == {"toolu_runtime_approval"}:
            return text_response("HOBOT_RUNTIME_APPROVAL_COMPLETE")
        if tool_result_ids == {"toolu_extension_holder"}:
            return text_response("HOBOT_EXTENSION_HOLDER_OK")
        if tool_result_ids == {"toolu_extension_contender"}:
            encoded_content = json.dumps(content, separators=(",", ":"), ensure_ascii=True)
            if "Workspace writes are busy" not in encoded_content:
                state.fail("overlapping extension tool call was not rejected by the workspace write lease")
                raise VerificationError("overlapping extension tool call bypassed the workspace write lease")
            return text_response("HOBOT_EXTENSION_LEASE_BLOCKED")
        raise VerificationError("unrecognized runtime-probe tool result sequence")
    if isinstance(content, str):
        prompt = content
    elif isinstance(content, list):
        prompt = "\n".join(str(item.get("text", "")) for item in content if isinstance(item, dict) and item.get("type") == "text")
    else:
        prompt = ""
    if "HOBOT_RPC_APPROVAL" in prompt:
        return anthropic_tool_stream()
    if "HOBOT_EXTENSION_LEASE_HOLDER" in prompt:
        return anthropic_tool_stream([(
            "toolu_extension_holder", "bash",
            {"command": "printf 'HOBOT_EXTENSION_HOLDER\\n' >> extension-holder.txt; while [ ! -f .hobot-extension-release ]; do sleep 0.1; done"},
        )])
    if "HOBOT_EXTENSION_LEASE_CONTENDER" in prompt:
        return anthropic_tool_stream([(
            "toolu_extension_contender", "bash",
            {"command": "printf 'UNEXPECTED_EXTENSION_WRITE\\n' >> extension-contender.txt"},
        )])
    runtime_calls = {
        "stage basic and nonce hobot-runtime-probe-v1": [("toolu_runtime_basic", "hobot_runtime_probe", {"stage": "basic", "nonce": "hobot-runtime-probe-v1"})],
        "stage recovery and nonce invalid-on-purpose": [("toolu_runtime_recovery_1", "hobot_runtime_probe", {"stage": "recovery", "nonce": "invalid-on-purpose"})],
        "stage approval and nonce confirm-read-only": [("toolu_runtime_approval", "hobot_runtime_probe", {"stage": "approval", "nonce": "confirm-read-only"})],
        "stage interrupt and nonce wait-for-termination": [("toolu_runtime_interrupt", "hobot_runtime_probe", {"stage": "interrupt", "nonce": "wait-for-termination"})],
    }
    for marker, calls in runtime_calls.items():
        if marker in prompt:
            return anthropic_tool_stream(calls)
    if "stage parallel and nonce parallel-a" in prompt and "stage parallel and nonce parallel-b" in prompt:
        return anthropic_tool_stream([
            ("toolu_runtime_parallel_a", "hobot_runtime_probe", {"stage": "parallel", "nonce": "parallel-a"}),
            ("toolu_runtime_parallel_b", "hobot_runtime_probe", {"stage": "parallel", "nonce": "parallel-b"}),
        ])
    if "Preserve exact opaque identifiers and user constraints" in prompt and "HOBOT_COMPACTION_CANARY_7F3C9A2D" in prompt:
        return text_response("Preserve HOBOT_COMPACTION_CANARY_7F3C9A2D and the user's constraints. The newer padding turn was acknowledged.")
    if "structured reasoning channel" in prompt and "HOBOT_RUNTIME_THINKING_COMPLETE" in prompt:
        return anthropic_thinking_stream(
            "HOBOT_RUNTIME_STRUCTURED_THINKING",
            "HOBOT_RUNTIME_THINKING_COMPLETE",
            estimated_input_tokens,
        )
    if "HOBOT_TUI_CHINESE" in prompt:
        if "你好，地瓜开发板" not in prompt:
            state.fail("TUI did not preserve the exact Chinese input")
            raise VerificationError("TUI corrupted the Chinese input")
        return anthropic_thinking_stream(
            "HOBOT_TUI_THINKING_CHINESE",
            "HOBOT_TUI_CHINESE_OK",
            estimated_input_tokens,
        )
    if "HOBOT_TUI_EDIT_BAD" in prompt:
        state.fail("TUI submitted text that should have been removed by the editor")
        raise VerificationError("TUI editor submitted deleted text")
    if "HOBOT_TUI_EDIT_OK" in prompt:
        if "HOBOT_TUI_EDIT_OK 中文编辑" not in prompt:
            state.fail("TUI did not preserve the edited Chinese input")
            raise VerificationError("TUI corrupted the edited input")
        return anthropic_thinking_stream(
            "HOBOT_TUI_THINKING_EDIT",
            "HOBOT_TUI_EDIT_OK_RESPONSE",
            estimated_input_tokens,
        )
    if "HOBOT_TUI_AFTER_DETACH" in prompt:
        return anthropic_thinking_stream(
            "HOBOT_TUI_THINKING_REATTACHED",
            "HOBOT_TUI_AFTER_DETACH_OK",
            estimated_input_tokens,
        )
    responses = {
        "HOBOT_RPC_SECOND": "HOBOT_RPC_SECOND_OK",
        "HOBOT_RPC_IMAGE": "HOBOT_RPC_IMAGE_OK",
        "HOBOT_SIDE_FIRST": "HOBOT_SIDE_FIRST_OK",
        "HOBOT_SIDE_SECOND": "HOBOT_SIDE_SECOND_OK",
        "HOBOT_SIDE_PEER": "HOBOT_SIDE_PEER_OK",
        "HOBOT_MAIN_AFTER_SIDE": "HOBOT_MAIN_AFTER_SIDE_OK",
        "HOBOT_EDIT_ORIGINAL": "HOBOT_EDIT_ORIGINAL_OK",
        "HOBOT_EDIT_REPLACEMENT": "HOBOT_EDIT_REPLACEMENT_OK",
        "HOBOT_RUNTIME_MEMORY_STORED": "HOBOT_RUNTIME_MEMORY_STORED",
        "HOBOT_RUNTIME_FILLER_STORED": "HOBOT_RUNTIME_FILLER_STORED",
        "four-quadrant image": "red green blue yellow",
        "exact opaque session token": "HOBOT_COMPACTION_CANARY_7F3C9A2D",
        "exact opaque recovery token": "HOBOT_RECOVERY_CANARY_4B8E1D6A",
    }
    for marker, response in responses.items():
        if marker in prompt:
            if marker.startswith("HOBOT_SIDE_") and "HOBOT_RPC_SECOND" not in encoded_history:
                state.fail("Side Agent request did not inherit the settled parent context")
                raise VerificationError("Side Agent request did not inherit the settled parent context")
            return text_response(response)
    return text_response()


def chat_stream() -> bytes:
    created = int(time.time())
    chunks = [
        {"id": "chatcmpl_hobot_test", "object": "chat.completion.chunk", "created": created, "model": "chat-test", "choices": [{"index": 0, "delta": {"role": "assistant", "content": "OK"}, "finish_reason": None}]},
        {"id": "chatcmpl_hobot_test", "object": "chat.completion.chunk", "created": created, "model": "chat-test", "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}},
    ]
    result = b"".join(f"data: {json.dumps(chunk, separators=(',', ':'))}\n\n".encode() for chunk in chunks)
    return result + b"data: [DONE]\n\n"


def responses_stream() -> bytes:
    created = int(time.time())
    item = {
        "id": "msg_hobot_test",
        "type": "message",
        "status": "completed",
        "role": "assistant",
        "content": [{"type": "output_text", "text": "OK", "annotations": []}],
        "phase": "final_answer",
    }
    response = {
        "id": "resp_hobot_test",
        "object": "response",
        "created_at": created,
        "status": "completed",
        "error": None,
        "incomplete_details": None,
        "model": "responses-test",
        "output": [item],
        "usage": {
            "input_tokens": 1,
            "input_tokens_details": {"cached_tokens": 0},
            "output_tokens": 1,
            "output_tokens_details": {"reasoning_tokens": 0},
            "total_tokens": 2,
        },
    }
    events = [
        {"type": "response.created", "response": {**response, "status": "in_progress", "output": [], "usage": None}},
        {"type": "response.output_item.added", "output_index": 0, "item": {**item, "status": "in_progress", "content": []}},
        {"type": "response.output_text.delta", "item_id": item["id"], "output_index": 0, "content_index": 0, "delta": "OK"},
        {"type": "response.output_item.done", "output_index": 0, "item": item},
        {"type": "response.completed", "response": response},
    ]
    return b"".join(
        f"event: {event['type']}\ndata: {json.dumps(event, separators=(',', ':'))}\n\n".encode()
        for event in events
    )


class MockGateway(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, state: GatewayState) -> None:
        super().__init__(("127.0.0.1", 0), MockGatewayHandler)
        self.state = state


class MockGatewayHandler(BaseHTTPRequestHandler):
    server: MockGateway

    def log_message(self, _format: str, *args: object) -> None:
        return

    def do_POST(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler API
        routes = {
            "/anthropic/v1/messages": "anthropic-test",
            "/chat/v1/chat/completions": "chat-test",
            "/responses/v1/responses": "responses-test",
        }
        provider = routes.get(self.path)
        try:
            if provider is None:
                raise VerificationError(f"unexpected mock gateway path: {self.path}")
            length = int(self.headers.get("Content-Length", "0"))
            if length < 1 or length > 32 * 1024 * 1024:
                raise VerificationError(f"invalid request size for {provider}")
            payload = json.loads(self.rfile.read(length))
            if payload.get("model") != provider or payload.get("stream") is not True:
                raise VerificationError(f"invalid model request for {provider}")
            credential = FAKE_CREDENTIALS[provider]
            if provider == "anthropic-test":
                if self.headers.get("X-Api-Key") != credential or self.headers.get("Authorization"):
                    raise VerificationError("Anthropic credential header was not rewritten correctly")
            elif self.headers.get("Authorization") != f"Bearer {credential}":
                raise VerificationError(f"Bearer credential header was not rewritten for {provider}")
            with self.server.state.lock:
                self.server.state.requests.append({"provider": provider, "path": self.path})
            self.server.state.received[provider].set()
            if not self.server.state.release[provider].wait(15):
                raise VerificationError(f"timed out waiting to release {provider} response")
            body = anthropic_response(payload, self.server.state) if provider == "anthropic-test" else {
                "chat-test": chat_stream,
                "responses-test": responses_stream,
            }[provider]()
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            self.wfile.flush()
        except Exception as error:  # The harness reports the specific failure later.
            self.server.state.fail(str(error))
            try:
                self.send_error(400, "invalid test request")
            except (BrokenPipeError, ConnectionResetError):
                pass


def private_json(path: Path, value: Any) -> None:
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")
    path.chmod(0o600)


def prepare_environment(package: Path, root: Path, gateway_port: int) -> tuple[dict[str, str], Path, Path]:
    config_root = root / "config"
    agent_dir = config_root / "agent"
    state_root = root / "state"
    session_dir = state_root / "sessions"
    workspace = root / "workspace"
    home = root / "home"
    for directory in (config_root, agent_dir, state_root, session_dir, workspace, home):
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        directory.chmod(0o700)

    for source in (package / "config").glob("*.json"):
        destination = agent_dir / source.name
        shutil.copyfile(source, destination)
        destination.chmod(0o600)

    providers = []
    prefixes = {"anthropic-test": "anthropic", "chat-test": "chat/v1", "responses-test": "responses/v1"}
    for provider in FAKE_CREDENTIALS:
        providers.append({
            "id": provider,
            "name": provider,
            "baseUrl": f"http://127.0.0.1:{gateway_port}/{prefixes[provider]}",
            "api": PROVIDER_API[provider],
            "credentialEnv": PROVIDER_ENV[provider],
            "models": [{
                "id": provider,
                "reasoning": provider == "anthropic-test",
                "input": ["text", "image"],
                "contextWindow": 8192,
                "maxTokens": 512,
            }],
        })
    private_json(agent_dir / "providers.json", {"schemaVersion": 1, "providers": providers})
    private_json(agent_dir / "settings.json", {
        "defaultProvider": "anthropic-test",
        "defaultModel": "anthropic-test",
        "defaultThinkingLevel": "off",
        "hideThinkingBlock": False,
        "tuiMode": "fullscreen",
        "quietStartup": True,
        "collapseChangelog": True,
        "defaultProjectTrust": "ask",
        "enableInstallTelemetry": False,
        "retry": {"enabled": False, "maxRetries": 0, "provider": {"timeoutMs": 15000, "maxRetries": 0}},
        "httpIdleTimeoutMs": 15000,
        "enabledModels": [f"{provider}/{provider}" for provider in FAKE_CREDENTIALS],
        "extensions": [str(package / "extensions" / "rdk" / "index.ts")],
        "skills": [str(package / "skills")],
        "enableSkillCommands": True,
    })

    env = os.environ.copy()
    for name in list(env):
        if name.startswith("HOBOT_CODE_PROVIDER_KEY_") or name in {
            "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
            "HOBOT_CODE_GATEWAY_TOKEN_FD", "HOBOT_CODE_GATEWAY_CREDENTIAL_FD",
        }:
            env.pop(name, None)
    socket_path = root / "a.sock"
    env.update({
        "HOME": str(home),
        "PATH": f"{package / 'managed-bin'}:{env.get('PATH', '')}",
        "XDG_CONFIG_HOME": str(root / "xdg-config"),
        "XDG_STATE_HOME": str(root / "xdg-state"),
        "HOBOT_CODE_CONFIG_DIR": str(config_root),
        "HOBOT_CODING_AGENT_DIR": str(agent_dir),
        "HOBOT_CODE_STATE_DIR": str(state_root),
        "HOBOT_CODING_AGENT_SESSION_DIR": str(session_dir),
        "HOBOT_CODE_AGENTD_SOCKET": str(socket_path),
        "HOBOT_CODE_AGENT_BINARY": str(package / "runtime" / "hobot"),
        "HOBOT_CODE_EXTENSION_CATALOG": str(package / "extensions" / "catalog.json"),
        "HOBOT_CODE_MANAGED_PROVIDER_CONFIG": str(agent_dir / "providers.json"),
        "HOBOT_CODE_PERMISSION_POLICY": str(agent_dir / "permissions.json"),
        "HOBOT_CODE_MEMORY_CONFIG": str(agent_dir / "memory.json"),
        "HOBOT_CODE_MEMORY_DB": str(state_root / "memory" / "memory.db"),
        "HOBOT_CODE_GOAL_CONFIG": str(agent_dir / "goals.json"),
        "HOBOT_CODE_GOAL_DB": str(state_root / "goals" / "goals.db"),
        "HOBOT_CODE_HOOK_CONFIG": str(agent_dir / "hooks.json"),
        "HOBOT_CODE_HOOK_AUDIT": str(state_root / "audit" / "hooks.jsonl"),
        "HOBOT_CODE_NOTIFICATION_CONFIG": str(agent_dir / "notifications.json"),
        "HOBOT_CODE_LSP_CONFIG": str(agent_dir / "lsp.json"),
        "HOBOT_CODE_RDK_KNOWLEDGE_DIR": str(package / "knowledge"),
        "HOBOT_CODE_RDK_EXPERT_PROMPT": str(package / "prompts" / "rdk-expert.md"),
        "HOBOT_CODE_BWRAP": shutil.which("bwrap") or "/usr/bin/bwrap",
        "HOBOT_CODE_MAX_BACKGROUND_TASKS": "3",
        "HOBOT_CODE_VERSION": (package / "VERSION").read_text(encoding="utf-8").strip(),
        "PI_SKIP_VERSION_CHECK": "1",
        "API_TIMEOUT_MS": "15000",
    })
    for provider, value in FAKE_CREDENTIALS.items():
        env[PROVIDER_ENV[provider]] = value
    return env, socket_path, workspace


def rpc_response(socket_path: Path, method: str, params: dict[str, Any], request_id: int, timeout: float = 10) -> dict[str, Any]:
    request = json.dumps({"protocol": PROTOCOL, "id": f"verify-{request_id}", "method": method, "params": params}, separators=(",", ":")).encode() + b"\n"
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(timeout)
        connection.connect(str(socket_path))
        connection.sendall(request)
        response = bytearray()
        while len(response) <= MAX_RPC_RESPONSE:
            chunk = connection.recv(64 * 1024)
            if not chunk:
                break
            response.extend(chunk)
            if b"\n" in response:
                break
    if len(response) > MAX_RPC_RESPONSE:
        raise VerificationError(f"oversized response from {method}")
    try:
        document = json.loads(bytes(response).split(b"\n", 1)[0])
    except Exception as error:
        raise VerificationError(f"invalid response from {method}: {error}") from error
    if not isinstance(document, dict):
        raise VerificationError(f"invalid response from {method}: expected an object")
    return document


def rpc(socket_path: Path, method: str, params: dict[str, Any], request_id: int, timeout: float = 10) -> Any:
    document = rpc_response(socket_path, method, params, request_id, timeout)
    if document.get("protocol") != PROTOCOL or not document.get("ok"):
        detail = document.get("error", {}).get("message", "unknown RPC failure")
        raise VerificationError(f"{method} failed: {detail}")
    return document.get("result")


def rpc_error(socket_path: Path, method: str, params: dict[str, Any], request_id: int, timeout: float = 10) -> str:
    document = rpc_response(socket_path, method, params, request_id, timeout)
    if document.get("protocol") != PROTOCOL or document.get("ok") is not False:
        raise VerificationError(f"{method} unexpectedly succeeded or returned an invalid error envelope")
    error = document.get("error")
    detail = error.get("message") if isinstance(error, dict) else None
    if not isinstance(detail, str) or not detail:
        raise VerificationError(f"{method} returned an empty error")
    return detail


def wait_for_path(path: Path, process: subprocess.Popen[Any], timeout: float = 10) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if path.exists():
            return
        if process.poll() is not None:
            raise VerificationError(f"agentd exited before creating {path.name}")
        time.sleep(0.05)
    raise VerificationError(f"timed out waiting for {path.name}")


def worker_process_environments(root_pid: int) -> list[tuple[int, bytes]]:
    parent_by_pid: dict[int, int] = {}
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            status = (entry / "status").read_text(encoding="utf-8", errors="replace")
            parent_line = next(line for line in status.splitlines() if line.startswith("PPid:"))
            parent_by_pid[int(entry.name)] = int(parent_line.split()[1])
        except (OSError, StopIteration, ValueError):
            continue
    descendants = {root_pid}
    changed = True
    while changed:
        changed = False
        for pid, parent in parent_by_pid.items():
            if parent in descendants and pid not in descendants:
                descendants.add(pid)
                changed = True
    result: list[tuple[int, bytes]] = []
    for pid in sorted(descendants):
        try:
            result.append((pid, Path(f"/proc/{pid}/environ").read_bytes()))
        except OSError:
            continue
    if not result:
        raise VerificationError("cannot inspect the worker process tree credential boundary")
    return result


def task_failure_detail(state_root: Path, task_id: str, current: dict[str, Any]) -> str:
    detail = str(current.get("lastError", "")).strip()
    worker_log = sanitized_log(state_root / "agentd" / "tasks" / task_id / "worker.stderr.log").strip()
    if worker_log:
        detail = f"{detail}; worker log: {worker_log}" if detail else f"worker log: {worker_log}"
    return detail


def task_events(socket_path: Path, task_id: str, after: int, sequence: int) -> tuple[dict[str, Any], int]:
    page = rpc(socket_path, "task.events", {"taskId": task_id, "after": after, "limit": 500}, sequence)
    events = page.get("events", [])
    if not isinstance(events, list):
        raise VerificationError("task events response is invalid")
    last = after
    for event in events:
        if isinstance(event, dict) and isinstance(event.get("sequence"), int):
            last = max(last, event["sequence"])
    return page, last


def event_text(page: dict[str, Any]) -> str:
    return "".join(
        str(event.get("normalized", {}).get("data", {}).get("delta", ""))
        for event in page.get("events", [])
        if isinstance(event, dict) and event.get("normalized", {}).get("type") == "assistant.text.delta"
    )


def value_locations(value: Any, needle: str, path: str = "$") -> list[str]:
    locations: list[str] = []
    if isinstance(value, str):
        if needle in value:
            locations.append(path)
    elif isinstance(value, list):
        for index, item in enumerate(value):
            locations.extend(value_locations(item, needle, f"{path}[{index}]"))
    elif isinstance(value, dict):
        for key, item in value.items():
            locations.extend(value_locations(item, needle, f"{path}.{key}"))
    return locations[:16]


def validate_diagnostic_report(report: Any) -> dict[str, dict[str, Any]]:
    expected = {"schemaVersion", "capturedAt", "status", "summary", "checks", "findings", "repairs"}
    if not isinstance(report, dict) or set(report) != expected or report.get("schemaVersion") != 1:
        raise VerificationError("readiness diagnostics returned an unsupported report")
    if report.get("status") not in {"healthy", "attention", "action-required"}:
        raise VerificationError("readiness diagnostics returned an invalid status")
    captured_at = report.get("capturedAt")
    try:
        if not isinstance(captured_at, str) or re.fullmatch(
            r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z", captured_at,
        ) is None:
            raise ValueError
        base = captured_at.split(".", 1)[0].removesuffix("Z")
        datetime.strptime(base, "%Y-%m-%dT%H:%M:%S")
    except ValueError as error:
        raise VerificationError("readiness diagnostics returned an invalid timestamp") from error

    summary = report.get("summary")
    if not isinstance(summary, dict) or set(summary) != {"pass", "info", "warn", "fail"}:
        raise VerificationError("readiness diagnostics returned an invalid summary")
    if any(not isinstance(summary[name], int) or isinstance(summary[name], bool) or summary[name] < 0 for name in summary):
        raise VerificationError("readiness diagnostics returned invalid summary counts")

    checks = report.get("checks")
    if not isinstance(checks, list) or not 1 <= len(checks) <= 64:
        raise VerificationError("readiness diagnostics returned an invalid check set")
    names: set[str] = set()
    counts = {"pass": 0, "info": 0, "warn": 0, "fail": 0}
    for check in checks:
        if not isinstance(check, dict) or set(check) != {"name", "status", "summary"}:
            raise VerificationError("readiness diagnostics returned a malformed check")
        name, status, detail = check["name"], check["status"], check["summary"]
        if not isinstance(name, str) or not name or name in names or not isinstance(detail, str) or not detail or status not in counts:
            raise VerificationError("readiness diagnostics returned an ambiguous check")
        names.add(name)
        counts[status] += 1
    if not {"configuration-current", "model-configuration", "release-integrity", "board-target", "task-lifecycle"}.issubset(names):
        raise VerificationError("readiness diagnostics omitted required product checks")
    if counts != summary:
        raise VerificationError("readiness diagnostics summary does not match its checks")
    expected_status = "action-required" if counts["fail"] else "attention" if counts["warn"] else "healthy"
    if report["status"] != expected_status:
        raise VerificationError("readiness diagnostics status does not match its checks")

    findings = report.get("findings")
    if not isinstance(findings, list) or len(findings) > 32:
        raise VerificationError("readiness diagnostics returned an invalid finding set")
    for finding in findings:
        required = {"code", "severity", "scope", "title", "summary", "action"}
        if not isinstance(finding, dict) or not required.issubset(finding) or not set(finding).issubset(required | {"count"}):
            raise VerificationError("readiness diagnostics returned a malformed finding")
        if any(not isinstance(finding[name], str) or not finding[name] for name in required):
            raise VerificationError("readiness diagnostics returned an empty finding")
        if finding["severity"] not in {"info", "warning", "error"}:
            raise VerificationError("readiness diagnostics returned an invalid finding severity")
        if "count" in finding and (not isinstance(finding["count"], int) or isinstance(finding["count"], bool) or finding["count"] <= 0):
            raise VerificationError("readiness diagnostics returned an invalid finding count")

    repairs = report.get("repairs")
    if not isinstance(repairs, list) or len(repairs) > 4:
        raise VerificationError("readiness diagnostics returned an invalid repair set")
    by_id: dict[str, dict[str, Any]] = {}
    for repair in repairs:
        if not isinstance(repair, dict) or set(repair) != {"id", "executor", "status", "requiresConfirmation", "summary", "reason"}:
            raise VerificationError("readiness diagnostics returned a malformed repair")
        identifier = repair["id"]
        if not isinstance(identifier, str) or not identifier or identifier in by_id:
            raise VerificationError("readiness diagnostics returned an ambiguous repair")
        if repair["executor"] not in {"agentd", "client"} or repair["status"] not in {"available", "blocked"} or repair["requiresConfirmation"] is not True:
            raise VerificationError("readiness diagnostics returned an unsafe repair")
        if not isinstance(repair["summary"], str) or not repair["summary"] or not isinstance(repair["reason"], str) or not repair["reason"]:
            raise VerificationError("readiness diagnostics returned an empty repair description")
        by_id[identifier] = repair
    return by_id


def private_file_names(root: Path) -> tuple[str, ...]:
    if not root.exists():
        return ()
    return tuple(sorted(path.relative_to(root).as_posix() for path in root.rglob("*") if path.is_file()))


def verify_readiness_diagnostics(
    package: Path, socket_path: Path, environment: dict[str, str], workspace: Path,
    state: GatewayState, sequence: int,
) -> tuple[int, list[dict[str, str]]]:
    state_root = Path(environment["HOBOT_CODE_STATE_DIR"])
    support_root = state_root / "support"
    support_before = private_file_names(support_root)
    with state.lock:
        gateway_before = len(state.requests)

    initial = rpc(socket_path, "diagnostics.inspect", {}, sequence)
    sequence += 1
    initial_repairs = validate_diagnostic_report(initial)
    if initial_repairs:
        raise VerificationError("readiness diagnostics advertised a repair before a fault was introduced")
    model_check = next((check for check in initial["checks"] if check["name"] == "model-configuration"), None)
    if not isinstance(model_check, dict) or model_check.get("status") != "pass":
        raise VerificationError("readiness diagnostics did not recognize the isolated model configuration")

    completed = subprocess.run(
        [str(package / "agentd"), "doctor", "--json"], env=environment,
        text=True, capture_output=True, timeout=15, check=False,
    )
    if completed.returncode != 0:
        raise VerificationError(f"hobot doctor --json failed: {completed.stderr.strip()[-1000:]}")
    try:
        cli_report = json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise VerificationError("hobot doctor --json returned invalid JSON") from error
    validate_diagnostic_report(cli_report)

    sentinel = workspace / "readiness-sentinel.txt"
    sentinel.write_text("outside-managed-runtime\n", encoding="utf-8")
    sentinel.chmod(0o644)
    state_root.chmod(0o755)
    faulted = rpc(socket_path, "diagnostics.inspect", {}, sequence)
    sequence += 1
    repairs = validate_diagnostic_report(faulted)
    repair = repairs.get("private-runtime-permissions")
    if not repair or repair.get("executor") != "agentd" or repair.get("status") != "available":
        raise VerificationError("readiness diagnostics did not advertise the bounded permission repair")

    detail = rpc_error(socket_path, "diagnostics.repair", {
        "action": "private-runtime-permissions", "confirm": False,
    }, sequence)
    sequence += 1
    if "explicit confirmation" not in detail or stat.S_IMODE(state_root.stat().st_mode) != 0o755:
        raise VerificationError("readiness repair did not fail closed without confirmation")

    result = rpc(socket_path, "diagnostics.repair", {
        "action": "private-runtime-permissions", "confirm": True,
    }, sequence)
    sequence += 1
    if not isinstance(result, dict) or set(result) != {"schemaVersion", "action", "changed", "report"}:
        raise VerificationError("readiness repair returned an invalid result")
    if result.get("schemaVersion") != 1 or result.get("action") != "private-runtime-permissions":
        raise VerificationError("readiness repair returned an unexpected identity")
    changed = result.get("changed")
    if not isinstance(changed, int) or isinstance(changed, bool) or not 1 <= changed <= 32:
        raise VerificationError("readiness repair returned an invalid change count")
    repaired = validate_diagnostic_report(result.get("report"))
    if "private-runtime-permissions" in repaired or stat.S_IMODE(state_root.stat().st_mode) != 0o700:
        raise VerificationError("readiness repair did not restore the private runtime boundary")
    if sentinel.read_text(encoding="utf-8") != "outside-managed-runtime\n" or stat.S_IMODE(sentinel.stat().st_mode) != 0o644:
        raise VerificationError("readiness repair changed a path outside its managed allowlist")

    support_after = private_file_names(support_root)
    with state.lock:
        gateway_after = len(state.requests)
    encoded = json.dumps([initial, cli_report, faulted, result], separators=(",", ":"), ensure_ascii=True)
    forbidden = [str(state_root.parent), str(workspace), *FAKE_CREDENTIALS.values(), "outside-managed-runtime"]
    if support_after != support_before or gateway_after != gateway_before or any(value in encoded for value in forbidden):
        raise VerificationError("readiness diagnostics leaked content, called a model, or created a support file")
    return sequence, [
        {"name": "read-only-inspection", "status": "pass"},
        {"name": "cli-json", "status": "pass"},
        {"name": "confirmation-required", "status": "pass"},
        {"name": "bounded-permission-repair", "status": "pass"},
        {"name": "privacy-no-support-file", "status": "pass"},
    ]


def wait_for_task_status(socket_path: Path, state_root: Path, task_id: str, expected: set[str], sequence: int, timeout: float = 25) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        current = rpc(socket_path, "task.get", {"taskId": task_id}, sequence)
        status = current.get("status")
        if status in expected:
            return current
        if status in {"failed", "interrupted", "stopped"}:
            raise VerificationError(f"task {task_id} ended as {status}: {task_failure_detail(state_root, task_id, current)}")
        time.sleep(0.1)
    raise VerificationError(f"task {task_id} did not reach {sorted(expected)}")


def prompt_and_expect(socket_path: Path, state_root: Path, task_id: str, prompt: str, expected: str, after: int, sequence: int, images: list[dict[str, str]] | None = None) -> tuple[int, int]:
    command: dict[str, Any] = {"id": f"accept-{sequence}", "type": "prompt", "message": prompt}
    if images:
        command["images"] = images
    rpc(socket_path, "task.command", {"taskId": task_id, "command": command}, sequence)
    wait_for_task_status(socket_path, state_root, task_id, {"idle"}, sequence + 1)
    page, last = task_events(socket_path, task_id, after, sequence + 2)
    if event_text(page).strip() != expected:
        raise VerificationError(f"task {task_id} returned an unexpected continuation for {prompt}")
    return sequence + 3, last


def verify_task(socket_path: Path, state_root: Path, workspace: Path, provider: str, state: GatewayState, sequence: int, keep_running: bool = False) -> tuple[int, str, int]:
    task = rpc(socket_path, "task.start", {
        "name": f"model-egress-{provider}",
        "cwd": str(workspace),
        "prompt": "Reply with exactly OK.",
        "approve": False,
        "model": f"{provider}/{provider}",
        "permissionMode": "ask" if keep_running else "developer",
        "workspaceMode": "shared",
        "sandboxMode": "system",
        "networkMode": "model-only",
    }, sequence)
    task_id = task["id"]
    if not state.received[provider].wait(12):
        current = rpc(socket_path, "task.get", {"taskId": task_id}, sequence + 1)
        raise VerificationError(f"{provider} did not reach the broker; task status={current.get('status')} error={task_failure_detail(state_root, task_id, current)}")
    current = rpc(socket_path, "task.get", {"taskId": task_id}, sequence + 2)
    pid = int(current.get("pid", 0))
    if pid <= 0:
        raise VerificationError(f"{provider} worker PID is unavailable during the request")
    worker_environments = worker_process_environments(pid)
    for _process_id, worker_env in worker_environments:
        for configured_provider, name in PROVIDER_ENV.items():
            credential = FAKE_CREDENTIALS[configured_provider]
            if name.encode() + b"=" in worker_env or credential.encode() in worker_env:
                raise VerificationError(f"{provider} worker inherited a model credential")
    capable_workers = [
        worker_env for _process_id, worker_env in worker_environments
        if f"HOBOT_CODE_BACKGROUND_TASK_ID={task_id}".encode() in worker_env
        and b"HOBOT_CODE_MODEL_EGRESS_SOCKET=" in worker_env
        and b"HOBOT_CODE_MODEL_EGRESS_PROVIDERS=" in worker_env
    ]
    if not capable_workers:
        raise VerificationError(f"{provider} worker is missing its constrained broker capability")
    state.release[provider].set()

    wait_for_task_status(socket_path, state_root, task_id, {"idle"}, sequence + 3, 20)

    page, last = task_events(socket_path, task_id, 0, sequence + 4)
    text = event_text(page)
    event_types = {event.get("normalized", {}).get("type") for event in page.get("events", [])}
    if text.strip() != "OK" or "assistant.message.completed" not in event_types or "task.idle" not in event_types:
        raise VerificationError(f"{provider} produced an incomplete event lifecycle: text={text!r} events={sorted(str(value) for value in event_types)}")
    if not keep_running:
        rpc(socket_path, "task.stop", {"taskId": task_id}, sequence + 5)
    return sequence + 6, task_id, last


def stop_task(socket_path: Path, task_id: str, sequence: int) -> int:
    rpc(socket_path, "task.stop", {"taskId": task_id}, sequence)
    return sequence + 1


def approval_allow_once_response(approval: dict[str, Any]) -> dict[str, Any]:
    request_id = approval.get("id")
    method = approval.get("method")
    if not isinstance(request_id, str) or not request_id:
        raise VerificationError("approval request has no ID")
    response: dict[str, Any] = {"type": "extension_ui_response", "id": request_id}
    if method == "confirm":
        response["confirmed"] = True
        return response
    if method == "select":
        options = approval.get("options")
        if not isinstance(options, list) or any(not isinstance(option, str) for option in options):
            raise VerificationError("select approval has invalid options")
        if options.count(APPROVAL_ALLOW_ONCE) != 1:
            raise VerificationError("select approval does not offer exactly one safe allow-once option")
        response["value"] = APPROVAL_ALLOW_ONCE
        return response
    raise VerificationError(f"approval method cannot be accepted automatically: {method!r}")


def verify_rpc_background(socket_path: Path, state_root: Path, workspace: Path, parent_task_id: str, after: int, sequence: int) -> tuple[int, list[dict[str, str]]]:
    rpc(socket_path, "task.command", {
        "taskId": parent_task_id,
        "command": {"id": "rpc-approval", "type": "prompt", "message": "HOBOT_RPC_APPROVAL"},
    }, sequence)
    current = wait_for_task_status(socket_path, state_root, parent_task_id, {"waiting"}, sequence + 1)
    approvals = rpc(socket_path, "task.approvals", {"taskId": parent_task_id}, sequence + 2)
    active = [approval for approval in approvals if isinstance(approval, dict) and approval.get("active") is True]
    if len(active) != 1 or not isinstance(active[0].get("id"), str):
        raise VerificationError(f"approval queue is not exact: {current.get('status')}")
    rpc(socket_path, "task.command", {
        "taskId": parent_task_id,
        "command": approval_allow_once_response(active[0]),
    }, sequence + 3)
    wait_for_task_status(socket_path, state_root, parent_task_id, {"idle"}, sequence + 4)
    page, after = task_events(socket_path, parent_task_id, after, sequence + 5)
    if event_text(page).strip() != "HOBOT_RPC_TOOL_OK":
        raise VerificationError("approved tool turn did not complete")
    proof = workspace / "rpc-proof.txt"
    try:
        lines = proof.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        tool_summary = []
        for event in page.get("events", []):
            normalized = event.get("normalized", {}) if isinstance(event, dict) else {}
            if normalized.get("type") == "tool.completed":
                data = normalized.get("data", {})
                tool_summary.append({
                    "tool": data.get("toolName"), "failed": data.get("isError"),
                    "output": str(data.get("outputPreview", ""))[:512],
                })
        raise VerificationError(f"approved tool did not produce its isolated proof: {error}; tool={json.dumps(tool_summary, ensure_ascii=True)}") from error
    if lines != ["HOBOT_RPC_EXECUTED"]:
        raise VerificationError("approved tool was missing or executed more than once")
    sequence += 6

    sequence, after = prompt_and_expect(socket_path, state_root, parent_task_id, "HOBOT_RPC_SECOND", "HOBOT_RPC_SECOND_OK", after, sequence)
    image_data = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
    sequence, after = prompt_and_expect(socket_path, state_root, parent_task_id, "HOBOT_RPC_IMAGE", "HOBOT_RPC_IMAGE_OK", after, sequence, [{
        "type": "image", "data": image_data, "mimeType": "image/png", "name": "acceptance.png",
    }])
    page, _ = task_events(socket_path, parent_task_id, 0, sequence)
    locations = value_locations(page, image_data)
    if locations:
        raise VerificationError(f"task history retained the image payload at {locations}")
    sequence += 1

    side_one = rpc(socket_path, "task.fork", {
        "taskId": parent_task_id, "kind": "side", "name": "acceptance-side-one", "prompt": "HOBOT_SIDE_FIRST",
    }, sequence)
    side_one_id = side_one.get("id")
    if not isinstance(side_one_id, str):
        raise VerificationError("first Side Agent has no task ID")
    wait_for_task_status(socket_path, state_root, side_one_id, {"idle"}, sequence + 1)
    side_page, side_after = task_events(socket_path, side_one_id, 0, sequence + 2)
    if event_text(side_page).strip() != "HOBOT_SIDE_FIRST_OK" or side_one.get("parentTaskId") != parent_task_id or side_one.get("branchKind") != "side":
        raise VerificationError("first Side Agent did not inherit one flat parent context")
    sequence += 3
    sequence, side_after = prompt_and_expect(socket_path, state_root, side_one_id, "HOBOT_SIDE_SECOND", "HOBOT_SIDE_SECOND_OK", side_after, sequence)

    side_two = rpc(socket_path, "task.fork", {
        "taskId": side_one_id, "kind": "side", "name": "acceptance-side-peer", "prompt": "HOBOT_SIDE_PEER",
    }, sequence)
    side_two_id = side_two.get("id")
    if not isinstance(side_two_id, str):
        raise VerificationError("peer Side Agent has no task ID")
    wait_for_task_status(socket_path, state_root, side_two_id, {"idle"}, sequence + 1)
    side_page, _ = task_events(socket_path, side_two_id, 0, sequence + 2)
    if event_text(side_page).strip() != "HOBOT_SIDE_PEER_OK" or side_two.get("parentTaskId") != parent_task_id:
        raise VerificationError("Side Agents formed a nested hierarchy")
    sequence += 3
    sequence, after = prompt_and_expect(socket_path, state_root, parent_task_id, "HOBOT_MAIN_AFTER_SIDE", "HOBOT_MAIN_AFTER_SIDE_OK", after, sequence)

    for task_id in (side_two_id, side_one_id, parent_task_id):
        sequence = stop_task(socket_path, task_id, sequence)
    return sequence, [
        {"name": "persistent-task", "status": "pass"},
        {"name": "tool-approval", "status": "pass"},
        {"name": "second-turn", "status": "pass"},
        {"name": "image-input", "status": "pass"},
        {"name": "reconnect-no-duplicate", "status": "pass"},
        {"name": "side-agent-multiturn", "status": "pass"},
        {"name": "side-agent-flat-parent", "status": "pass"},
        {"name": "main-agent-remains-active", "status": "pass"},
    ]


def validate_runtime_recovery_result(result: Any) -> list[dict[str, str]]:
    if not isinstance(result, dict) or result.get("schemaVersion") != 1 or result.get("scope") != "agent-runtime-partial":
        raise VerificationError("session recovery probe returned an invalid contract")
    if result.get("provider") != "anthropic-test" or result.get("model") != "anthropic-test" or result.get("status") != "partial":
        summary = {
            "provider": result.get("provider"),
            "model": result.get("model"),
            "status": result.get("status"),
            "category": result.get("category"),
            "checks": [
                {"name": check.get("name"), "status": check.get("status"), "message": check.get("message")}
                for check in result.get("checks", [])
                if isinstance(check, dict) and check.get("status") != "passed"
            ],
        }
        raise VerificationError(
            "session recovery probe returned the wrong model or evidence level: "
            + json.dumps(summary, separators=(",", ":"), ensure_ascii=True)
        )
    if result.get("reasoningDeclared") is not True or result.get("imageInputDeclared") is not True or result.get("pending") != ["rdk-task-suite"]:
        raise VerificationError("session recovery probe overstated or omitted its declared coverage")
    raw_checks = result.get("checks")
    if not isinstance(raw_checks, list):
        raise VerificationError("session recovery probe omitted its check set")
    checks: dict[str, str] = {}
    for check in raw_checks:
        if not isinstance(check, dict) or set(check) != {"name", "status", "message"}:
            raise VerificationError("session recovery probe returned a malformed check")
        name, status, message = check["name"], check["status"], check["message"]
        if not isinstance(name, str) or name in checks or not isinstance(status, str) or not isinstance(message, str) or not message:
            raise VerificationError("session recovery probe returned an ambiguous check")
        checks[name] = status
    if set(checks) != RUNTIME_PROBE_CHECKS:
        raise VerificationError("session recovery probe returned an incomplete check set")
    for name, status in checks.items():
        if status != "passed":
            raise VerificationError(f"session recovery runtime check failed: {name}")
    return [
        {"name": "context-compaction", "status": "pass"},
        {"name": "interrupted-session-recovery", "status": "pass"},
    ]


def run_runtime_probe(socket_path: Path, sequence: int) -> tuple[int, dict[str, Any], list[dict[str, str]]]:
    result = rpc(socket_path, "models.runtime-probe", {"model": "anthropic-test/anthropic-test"}, sequence, timeout=240)
    checks = validate_runtime_recovery_result(result)
    return sequence + 1, result, checks


def runtime_probe_check_passed(result: dict[str, Any], name: str) -> bool:
    return any(
        isinstance(check, dict) and check.get("name") == name and check.get("status") == "passed"
        for check in result.get("checks", [])
    )


def verify_session_recovery(
    socket_path: Path, state_root: Path, workspace: Path, sequence: int, checks: list[dict[str, str]],
) -> tuple[int, list[dict[str, str]]]:
    checks = list(checks)

    source = rpc(socket_path, "task.start", {
        "name": "session-edit-source", "cwd": str(workspace), "prompt": "HOBOT_EDIT_ORIGINAL",
        "approve": False, "model": "anthropic-test/anthropic-test", "permissionMode": "review",
        "workspaceMode": "shared", "sandboxMode": "review", "networkMode": "model-only",
    }, sequence)
    source_id = source.get("id")
    if not isinstance(source_id, str):
        raise VerificationError("history edit source has no task ID")
    wait_for_task_status(socket_path, state_root, source_id, {"idle"}, sequence + 1)
    source_page, _ = task_events(socket_path, source_id, 0, sequence + 2)
    source_prompts = [
        event for event in source_page.get("events", [])
        if isinstance(event, dict) and event.get("normalized", {}).get("type") == "user.message"
        and event.get("normalized", {}).get("data", {}).get("text") == "HOBOT_EDIT_ORIGINAL"
    ]
    if len(source_prompts) != 1 or not isinstance(source_prompts[0].get("sequence"), int):
        raise VerificationError("history edit source prompt is not uniquely addressable")
    sequence += 3

    edited = rpc(socket_path, "task.fork", {
        "taskId": source_id, "sequence": source_prompts[0]["sequence"], "kind": "edit",
        "name": "session-edit-replacement", "prompt": "HOBOT_EDIT_REPLACEMENT",
    }, sequence)
    edited_id = edited.get("id")
    if not isinstance(edited_id, str) or edited.get("branchKind") != "edit" or edited.get("parentTaskId") != source_id:
        raise VerificationError("history edit did not create the expected private branch")
    wait_for_task_status(socket_path, state_root, edited_id, {"idle"}, sequence + 1)
    edited_page, _ = task_events(socket_path, edited_id, 0, sequence + 2)
    edited_document = json.dumps(edited_page, separators=(",", ":"), ensure_ascii=True)
    edited_prompts = [
        event.get("normalized", {}).get("data", {}).get("text")
        for event in edited_page.get("events", [])
        if isinstance(event, dict) and event.get("normalized", {}).get("type") == "user.message"
    ]
    if edited_prompts != ["HOBOT_EDIT_REPLACEMENT"] or event_text(edited_page).strip() != "HOBOT_EDIT_REPLACEMENT_OK":
        raise VerificationError("history edit retained the replaced timeline or lost its replacement")
    if "HOBOT_EDIT_ORIGINAL" in edited_document:
        raise VerificationError("history edit exposed the replaced user turn")
    sequence += 3
    for task_id in (edited_id, source_id):
        current = rpc(socket_path, "task.get", {"taskId": task_id}, sequence)
        sequence += 1
        if current.get("status") != "stopped":
            sequence = stop_task(socket_path, task_id, sequence)
    checks.extend([
        {"name": "history-edit-branch", "status": "pass"},
        {"name": "fresh-client-connections", "status": "pass"},
    ])
    return sequence, checks


def verify_extension_safety(
    socket_path: Path, state_root: Path, workspace: Path, sequence: int, runtime_result: dict[str, Any],
) -> tuple[int, list[dict[str, str]]]:
    catalog = rpc(socket_path, "extensions.list", {}, sequence)
    sequence += 1
    if not isinstance(catalog, dict) or catalog.get("schemaVersion") != 1 or catalog.get("apiVersion") != "hobot.extensions/v1":
        raise VerificationError("extension inventory returned an invalid contract")
    policy = catalog.get("policy")
    if not isinstance(policy, dict) or policy.get("inventoryOnly") is not True or policy.get("executionAuthority") != "pi-runtime" or policy.get("permissionAuthority") != "board":
        raise VerificationError("extension inventory overstated its execution or permission authority")
    entries = catalog.get("entries")
    if not isinstance(entries, list):
        raise VerificationError("extension inventory omitted packaged resources")
    by_id = {entry.get("id"): entry for entry in entries if isinstance(entry, dict) and isinstance(entry.get("id"), str)}
    expected_resources = {
        "hobot.rdk-core": ("extension", "pi-extension", True),
        "hobot.skill.rdk-board": ("skill", "pi-skill", False),
        "hobot.skill.system-info": ("skill", "pi-skill", False),
        "hobot.skill.workspace-coding": ("skill", "pi-skill", False),
    }
    for resource_id, (kind, runtime, required) in expected_resources.items():
        entry = by_id.get(resource_id)
        if not isinstance(entry, dict) or entry.get("kind") != kind or entry.get("runtime") != runtime or entry.get("required") is not required or entry.get("status") != "included":
            raise VerificationError(f"packaged resource inventory is incomplete: {resource_id}")
    if not runtime_probe_check_passed(runtime_result, "parallel-tools"):
        raise VerificationError("extension runtime did not preserve a parallel tool batch")
    if not runtime_probe_check_passed(runtime_result, "approval-flow"):
        raise VerificationError("extension permission hook did not preserve correlated approval")

    holder_id = ""
    contender_id = ""
    release_path = workspace / ".hobot-extension-release"
    holder_path = workspace / "extension-holder.txt"
    contender_path = workspace / "extension-contender.txt"
    try:
        holder = rpc(socket_path, "task.start", {
            "name": "extension-lease-holder", "cwd": str(workspace), "prompt": "HOBOT_EXTENSION_LEASE_HOLDER",
            "approve": True, "model": "anthropic-test/anthropic-test", "permissionMode": "developer",
            "workspaceMode": "shared", "sandboxMode": "workspace", "networkMode": "model-only",
        }, sequence)
        sequence += 1
        holder_id = holder.get("id", "")
        if not isinstance(holder_id, str) or not holder_id:
            raise VerificationError("extension lease holder has no task ID")

        deadline = time.monotonic() + 15
        lease_seen = False
        while time.monotonic() < deadline:
            leases = rpc(socket_path, "workspace.writes", {}, sequence)
            sequence += 1
            raw_leases = leases.get("leases", []) if isinstance(leases, dict) else []
            if isinstance(raw_leases, list) and len(raw_leases) == 1 and isinstance(raw_leases[0], dict) and raw_leases[0].get("taskId") == holder_id:
                lease_seen = True
                break
            current = rpc(socket_path, "task.get", {"taskId": holder_id}, sequence)
            sequence += 1
            if current.get("status") in {"failed", "stopped"}:
                raise VerificationError("extension lease holder exited before acquiring the workspace lease")
            time.sleep(0.05)
        if not lease_seen:
            raise VerificationError("extension lease holder did not publish its workspace lease")

        contender = rpc(socket_path, "task.start", {
            "name": "extension-lease-contender", "cwd": str(workspace), "prompt": "HOBOT_EXTENSION_LEASE_CONTENDER",
            "approve": True, "model": "anthropic-test/anthropic-test", "permissionMode": "developer",
            "workspaceMode": "shared", "sandboxMode": "workspace", "networkMode": "model-only",
        }, sequence)
        sequence += 1
        contender_id = contender.get("id", "")
        if not isinstance(contender_id, str) or not contender_id:
            raise VerificationError("extension lease contender has no task ID")
        wait_for_task_status(socket_path, state_root, contender_id, {"idle"}, sequence)
        sequence += 1
        contender_events, _ = task_events(socket_path, contender_id, 0, sequence)
        sequence += 1
        if event_text(contender_events).strip() != "HOBOT_EXTENSION_LEASE_BLOCKED" or contender_path.exists():
            raise VerificationError("overlapping extension task was not blocked before its workspace write")
    finally:
        release_path.write_text("release\n", encoding="utf-8")

    wait_for_task_status(socket_path, state_root, holder_id, {"idle"}, sequence)
    sequence += 1
    holder_events, _ = task_events(socket_path, holder_id, 0, sequence)
    sequence += 1
    if event_text(holder_events).strip() != "HOBOT_EXTENSION_HOLDER_OK" or holder_path.read_text(encoding="utf-8") != "HOBOT_EXTENSION_HOLDER\n":
        raise VerificationError("extension lease holder did not complete exactly once")
    leases = rpc(socket_path, "workspace.writes", {}, sequence)
    sequence += 1
    if not isinstance(leases, dict) or leases.get("leases") != []:
        raise VerificationError("workspace write lease remained after the Agent turn settled")
    for task_id in (contender_id, holder_id):
        if task_id:
            sequence = stop_task(socket_path, task_id, sequence)
    return sequence, [
        {"name": "packaged-resource-discovery", "status": "pass"},
        {"name": "parallel-extension-tools", "status": "pass"},
        {"name": "permission-hook", "status": "pass"},
        {"name": "workspace-write-lease", "status": "pass"},
    ]


def sanitized_log(path: Path) -> str:
    try:
        output = path.read_text(encoding="utf-8", errors="replace")[-12000:]
    except OSError:
        return ""
    for secret in FAKE_CREDENTIALS.values():
        output = output.replace(secret, "[redacted]")
    return output


class PtyAttachment:
    def __init__(self, command: list[str], environment: dict[str, str]) -> None:
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", TUI_ROWS, TUI_COLUMNS, 0, 0))
        try:
            self.process = subprocess.Popen(
                command,
                env=environment,
                stdin=slave,
                stdout=slave,
                stderr=slave,
                close_fds=True,
            )
        finally:
            os.close(slave)
        self.master = master
        self.capture = bytearray()
        self.capture_lock = threading.Lock()
        self.reader = threading.Thread(target=self._drain, name="tmux-pty-drain", daemon=True)
        self.reader.start()

    def _drain(self) -> None:
        while True:
            try:
                chunk = os.read(self.master, 64 * 1024)
            except OSError:
                return
            if not chunk:
                return
            with self.capture_lock:
                self.capture.extend(chunk)
                if len(self.capture) > MAX_PTY_CAPTURE_BYTES:
                    del self.capture[:-MAX_PTY_CAPTURE_BYTES]

    def wait(self, timeout: float) -> int:
        try:
            return self.process.wait(timeout=timeout)
        except subprocess.TimeoutExpired as error:
            raise VerificationError("tmux client did not detach within the bounded timeout") from error

    def close(self) -> None:
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=2)
        try:
            os.close(self.master)
        except OSError:
            pass
        self.reader.join(timeout=1)

    def diagnostic(self) -> str:
        with self.capture_lock:
            raw = bytes(self.capture[-12000:])
        return raw.decode("utf-8", errors="replace")


def tmux_run(config: Path, environment: dict[str, str], arguments: list[str], timeout: float = 10) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            ["tmux", "-L", "hobot-code", "-f", str(config), *arguments],
            env=environment,
            text=True,
            capture_output=True,
            check=True,
            timeout=timeout,
        )
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        stdout = getattr(error, "stdout", "") or ""
        stderr = getattr(error, "stderr", "") or ""
        detail = (stdout + stderr).strip()[-2000:]
        raise VerificationError(f"tmux command failed: {detail or type(error).__name__}") from error


def tmux_capture(config: Path, environment: dict[str, str], session: str) -> str:
    return tmux_run(
        config, environment,
        ["capture-pane", "-p", "-J", "-S", "-1000", "-t", f"={session}:0.0"],
        5,
    ).stdout


def wait_for_tmux_text(config: Path, environment: dict[str, str], session: str, marker: str, timeout: float = 25) -> str:
    deadline = time.monotonic() + timeout
    last = ""
    while time.monotonic() < deadline:
        try:
            last = tmux_capture(config, environment, session)
        except VerificationError:
            pass
        if marker in last:
            return last
        time.sleep(0.1)
    raise VerificationError(f"TUI did not render expected marker {marker}; pane={last[-2000:]!r}")


def tmux_send_literal(config: Path, environment: dict[str, str], session: str, value: str) -> None:
    tmux_run(config, environment, ["send-keys", "-t", f"={session}:0.0", "-l", value], 5)


def tmux_send_keys(config: Path, environment: dict[str, str], session: str, *keys: str) -> None:
    tmux_run(config, environment, ["send-keys", "-t", f"={session}:0.0", *keys], 5)


def verify_tui_basics(package: Path, environment: dict[str, str], workspace: Path, state: GatewayState) -> list[dict[str, str]]:
    if os.geteuid() == 0:
        raise VerificationError("tui-basics must run as an ordinary non-root user")
    if shutil.which("tmux") is None:
        raise VerificationError("tmux is required for tui-basics acceptance")
    config = package / "config" / "tmux.conf"
    if not config.is_file():
        raise VerificationError("candidate package is missing its tmux configuration")

    session = f"hobot-code-acceptance-{os.getpid()}-{os.urandom(3).hex()}"
    tmux_environment = dict(environment)
    for name in list(tmux_environment):
        if name.startswith("HOBOT_CODE_PROVIDER_KEY_") or name in {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}:
            tmux_environment.pop(name, None)
    tmux_environment.pop("TMUX", None)
    tmux_environment.pop("TMUX_PANE", None)
    tmux_runtime = workspace.parent / "tmux"
    tmux_runtime.mkdir(mode=0o700)
    tmux_runtime.chmod(0o700)
    tmux_environment["TMUX_TMPDIR"] = str(tmux_runtime)
    tmux_environment.update({"TERM": "xterm-256color", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"})
    credential_file = workspace.parent / "tui-credentials.env"
    credential_file.write_text("".join(
        f"{PROVIDER_ENV[provider]}={shlex.quote(value)}\n"
        for provider, value in FAKE_CREDENTIALS.items()
    ), encoding="utf-8")
    credential_file.chmod(0o600)
    private_launcher = workspace.parent / "launch-tui"
    private_launcher.write_text(
        '#!/bin/sh\nset -eu\nset -a\n. "$1"\nset +a\nshift\nexec "$@"\n',
        encoding="utf-8",
    )
    private_launcher.chmod(0o700)
    command = [
        str(private_launcher), str(credential_file),
        str(package / "agentd"), "tui", "--sandbox", "off", "--network", "shared", "--",
        "--no-session", "--model", "anthropic-test/anthropic-test", "--thinking", "high",
        "--tui-mode", "fullscreen", "--no-context-files", "--no-skills", "--no-prompt-templates", "--no-approve",
    ]
    first_client: PtyAttachment | None = None
    second_client: PtyAttachment | None = None
    try:
        tmux_run(config, tmux_environment, [
            "new-session", "-d", "-x", str(TUI_COLUMNS), "-y", str(TUI_ROWS),
            "-s", session, "-c", str(workspace), shlex.join(command),
        ], 10)
        server_environment = tmux_run(config, tmux_environment, ["show-environment", "-g"], 5).stdout
        for name, secret in [
            *((name, "") for name in ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN")),
            *((PROVIDER_ENV[provider], value) for provider, value in FAKE_CREDENTIALS.items()),
        ]:
            if f"{name}=" in server_environment or (secret and secret in server_environment):
                raise VerificationError("persistent tmux server retained a model credential")
        first_client = PtyAttachment(
            ["tmux", "-L", "hobot-code", "-f", str(config), "attach-session", "-t", f"={session}"],
            tmux_environment,
        )
        wait_for_tmux_text(config, tmux_environment, session, "anthropic-test", 20)

        tmux_send_literal(config, tmux_environment, session, "HOBOT_TUI_CHINESE 你好，地瓜开发板")
        tmux_send_keys(config, tmux_environment, session, "Enter")
        chinese = wait_for_tmux_text(config, tmux_environment, session, "HOBOT_TUI_CHINESE_OK", 30)
        if "HOBOT_TUI_THINKING_CHINESE" not in chinese:
            raise VerificationError("TUI did not display the structured thinking block")

        tmux_send_literal(config, tmux_environment, session, "HOBOT_TUI_EDIT_BAD")
        tmux_send_keys(config, tmux_environment, session, "BSpace", "BSpace", "BSpace")
        tmux_send_literal(config, tmux_environment, session, "OK 中文编辑")
        tmux_send_keys(config, tmux_environment, session, "Enter")
        wait_for_tmux_text(config, tmux_environment, session, "HOBOT_TUI_EDIT_OK_RESPONSE", 30)

        tmux_send_literal(config, tmux_environment, session, "/detach")
        tmux_send_keys(config, tmux_environment, session, "Enter")
        if first_client.wait(10) != 0:
            raise VerificationError("the first tmux client exited unsuccessfully after /detach")
        tmux_run(config, tmux_environment, ["has-session", "-t", f"={session}"], 5)
        pane = tmux_run(config, tmux_environment, [
            "list-panes", "-t", f"={session}", "-F", "#{pane_dead}\t#{pane_pid}\t#{pane_current_command}",
        ], 5).stdout.strip()
        if not pane.startswith("0\t"):
            raise VerificationError("/detach terminated the persistent TUI pane")

        second_client = PtyAttachment(
            ["tmux", "-L", "hobot-code", "-f", str(config), "attach-session", "-t", f"={session}"],
            tmux_environment,
        )
        wait_for_tmux_text(config, tmux_environment, session, "HOBOT_TUI_EDIT_OK_RESPONSE", 10)
        tmux_send_literal(config, tmux_environment, session, "HOBOT_TUI_AFTER_DETACH")
        tmux_send_keys(config, tmux_environment, session, "Enter")
        wait_for_tmux_text(config, tmux_environment, session, "HOBOT_TUI_AFTER_DETACH_OK", 30)

        with state.lock:
            failures = list(state.errors)
        if failures:
            raise VerificationError("mock gateway validation failed: " + "; ".join(failures))
        return [{"name": name, "status": "pass"} for name in TUI_ACCEPTANCE_CHECKS]
    except Exception:
        diagnostics = []
        for client in (first_client, second_client):
            if client is not None:
                detail = client.diagnostic().strip()
                if detail:
                    diagnostics.append(detail[-4000:])
        if diagnostics:
            print("--- isolated TUI PTY output ---", file=sys.stderr)
            print("\n".join(diagnostics), file=sys.stderr)
        raise
    finally:
        for client in (second_client, first_client):
            if client is not None:
                client.close()
        try:
            tmux_run(config, tmux_environment, ["kill-session", "-t", f"={session}"], 5)
        except VerificationError:
            pass


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_manifest(package: Path) -> str:
    manifest_path = package / "MANIFEST.sha256"
    try:
        manifest_info = manifest_path.lstat()
        if not stat.S_ISREG(manifest_info.st_mode) or manifest_info.st_size <= 0 or manifest_info.st_size > MAX_MANIFEST_BYTES:
            raise VerificationError("package manifest is not a bounded regular file")
        content = manifest_path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        raise VerificationError(f"cannot read package manifest: {error}") from error

    expected: dict[str, str] = {}
    lines = content.splitlines()
    if not lines or len(lines) > MAX_MANIFEST_FILES:
        raise VerificationError("package manifest has an invalid file count")
    for line_number, line in enumerate(lines, start=1):
        match = MANIFEST_LINE.fullmatch(line)
        if match is None:
            raise VerificationError(f"package manifest line {line_number} is malformed")
        relative_name = match.group(2)
        relative_path = PurePosixPath(relative_name)
        if relative_path.is_absolute() or relative_name != relative_path.as_posix() or any(part in {"", ".", ".."} for part in relative_path.parts):
            raise VerificationError(f"package manifest line {line_number} has an unsafe path")
        if relative_name == "MANIFEST.sha256" or relative_name in expected:
            raise VerificationError(f"package manifest line {line_number} is duplicated or self-referential")
        expected[relative_name] = match.group(1)

    actual: set[str] = set()
    for directory, directories, files in os.walk(package, followlinks=False):
        base = Path(directory)
        for name in directories:
            path = base / name
            if path.is_symlink():
                raise VerificationError(f"package contains a symbolic link: {path.relative_to(package).as_posix()}")
        for name in files:
            path = base / name
            relative_name = path.relative_to(package).as_posix()
            info = path.lstat()
            if not stat.S_ISREG(info.st_mode):
                raise VerificationError(f"package contains a non-regular file: {relative_name}")
            if relative_name == "MANIFEST.sha256":
                continue
            actual.add(relative_name)
            expected_digest = expected.get(relative_name)
            if expected_digest is None:
                raise VerificationError(f"package file is missing from the manifest: {relative_name}")
            if sha256_file(path) != expected_digest:
                raise VerificationError(f"package file checksum mismatch: {relative_name}")
    missing = sorted(set(expected) - actual)
    if missing:
        raise VerificationError(f"package manifest references a missing file: {missing[0]}")
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def report_destination(path: Path) -> Path:
    requested = path.expanduser()
    return requested.parent.resolve(strict=False) / requested.name


def validate_report_destination(package: Path, path: Path) -> None:
    destination = report_destination(path)
    try:
        destination.relative_to(package)
    except ValueError:
        return
    raise VerificationError("report output must be outside the candidate package root")


def private_report(path: Path, report: dict[str, Any]) -> None:
    destination = report_destination(path)
    destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    try:
        current = destination.lstat()
    except FileNotFoundError:
        current = None
    if current is not None and (not stat.S_ISREG(current.st_mode) or stat.S_ISLNK(current.st_mode) or current.st_uid != os.getuid()):
        raise VerificationError("report destination must be a regular file owned by the current user")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{destination.name}.", dir=destination.parent)
    temporary_path = Path(temporary_name)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(report, output, ensure_ascii=True, indent=2, sort_keys=True)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary_path, destination)
        destination.chmod(0o600)
    except Exception:
        try:
            os.close(descriptor)
        except OSError:
            pass
        temporary_path.unlink(missing_ok=True)
        raise


def target_summary(snapshot: Any) -> dict[str, str]:
    source = snapshot if isinstance(snapshot, dict) else {}
    result = {"architecture": platform.machine().lower()}
    board_id = source.get("boardId")
    if board_id in {"x5", "s100", "s600"}:
        result["boardId"] = board_id
    release = source.get("rdkOsVersion")
    if isinstance(release, str) and 0 < len(release) <= 64 and all(character.isalnum() or character in ".-_" for character in release):
        result["rdkOsVersion"] = release
    return result


def acceptance_report(package: Path, snapshot: Any, manifest_sha256: str, scenario: str, checks: list[dict[str, str]]) -> dict[str, Any]:
    return {
        "schema": "hobot.pi-board-compatibility/v1",
        "scenario": scenario,
        "status": "pass",
        "capturedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "target": target_summary(snapshot),
        "build": {
            "version": (package / "VERSION").read_text(encoding="utf-8").strip(),
            "agentdSha256": sha256_file(package / "agentd"),
            "manifestSha256": manifest_sha256,
            "piCompatibilitySha256": sha256_file(package / "PI_COMPATIBILITY.json"),
        },
        "checks": checks,
    }


def verify(
    package: Path, run_rpc_background: bool = False, run_session_recovery: bool = False,
    run_extension_safety: bool = False, run_tui_basics: bool = False,
    run_readiness_diagnostics: bool = False,
) -> dict[str, dict[str, Any]]:
    if platform.system() != "Linux" or platform.machine() not in {"aarch64", "arm64"}:
        raise VerificationError("this packaged-runtime test must run on a Linux ARM64 board")
    required = [
        package / "agentd", package / "runtime" / "hobot", package / "extensions" / "catalog.json",
        package / "config" / "settings.json", package / "VERSION", package / "PI_COMPATIBILITY.json",
        package / "MANIFEST.sha256",
    ]
    missing = [str(path) for path in required if not path.is_file()]
    if missing:
        raise VerificationError("package is incomplete: " + ", ".join(missing))
    if shutil.which("bwrap") is None:
        raise VerificationError("bubblewrap is required to verify model-only isolation")
    manifest_sha256 = verify_manifest(package)

    state = GatewayState()
    gateway = MockGateway(state)
    gateway_thread = threading.Thread(target=gateway.serve_forever, name="mock-model-gateway", daemon=True)
    gateway_thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="hce-") as temporary:
            root = Path(temporary)
            root.chmod(0o700)
            env, socket_path, workspace = prepare_environment(package, root, gateway.server_port)
            log_path = root / "agentd.console.log"
            with log_path.open("wb") as log:
                process = subprocess.Popen([str(package / "agentd"), "serve"], env=env, stdout=log, stderr=subprocess.STDOUT)
                try:
                    wait_for_path(socket_path, process)
                    sequence = 1
                    ping = rpc(socket_path, "ping", {}, sequence)
                    sequence += 1
                    snapshot = rpc(socket_path, "system.snapshot", {}, sequence)
                    sequence += 1
                    capabilities = ping.get("capabilities", {}).get("capabilities", [])
                    if "models.egress-broker.v1" not in capabilities:
                        raise VerificationError("agentd did not advertise the model egress broker")
                    readiness_checks = None
                    if run_readiness_diagnostics:
                        if not {"diagnostics.inspect.v1", "diagnostics.repair.v1"}.issubset(capabilities):
                            raise VerificationError("agentd did not advertise the readiness diagnostics contract")
                        sequence, readiness_checks = verify_readiness_diagnostics(
                            package, socket_path, env, workspace, state, sequence,
                        )
                        print("PASS readiness diagnostics: read-only inspection, CLI JSON, confirmation, bounded repair, privacy")
                    checks = []
                    rpc_parent = ""
                    rpc_after = 0
                    for provider in FAKE_CREDENTIALS:
                        keep_running = run_rpc_background and provider == "anthropic-test"
                        sequence, task_id, last = verify_task(socket_path, Path(env["HOBOT_CODE_STATE_DIR"]), workspace, provider, state, sequence, keep_running)
                        if keep_running:
                            rpc_parent, rpc_after = task_id, last
                        checks.append({"provider": provider, "protocol": PROVIDER_API[provider], "status": "pass"})
                        print(f"PASS {provider}: native adapter, broker route, event lifecycle, credential isolation")
                    with state.lock:
                        failures = list(state.errors)
                        requests = list(state.requests)
                    if failures:
                        raise VerificationError("mock gateway validation failed: " + "; ".join(failures))
                    if [request["provider"] for request in requests[:len(FAKE_CREDENTIALS)]] != list(FAKE_CREDENTIALS) or len(requests) != len(FAKE_CREDENTIALS):
                        raise VerificationError(f"unexpected provider request order/count: {requests}")
                    print("PASS packaged Linux ARM64 runtime: all managed provider adapters use model-only egress")
                    reports = {"model-egress-runtime": acceptance_report(package, snapshot, manifest_sha256, "model-egress-runtime", checks)}
                    if readiness_checks is not None:
                        reports["readiness-diagnostics"] = acceptance_report(
                            package, snapshot, manifest_sha256, "readiness-diagnostics", readiness_checks,
                        )
                    if run_rpc_background:
                        if not rpc_parent:
                            raise VerificationError("RPC acceptance parent task is unavailable")
                        sequence, rpc_checks = verify_rpc_background(
                            socket_path, Path(env["HOBOT_CODE_STATE_DIR"]), workspace, rpc_parent, rpc_after, sequence,
                        )
                        with state.lock:
                            failures = list(state.errors)
                        if failures:
                            raise VerificationError("mock gateway validation failed: " + "; ".join(failures))
                        reports["rpc-background"] = acceptance_report(package, snapshot, manifest_sha256, "rpc-background", rpc_checks)
                        print("PASS RPC background: approval, multi-turn, image, reconnect, Side Agents, no duplicate execution")
                    runtime_result = None
                    runtime_checks = None
                    if run_session_recovery or run_extension_safety:
                        sequence, runtime_result, runtime_checks = run_runtime_probe(socket_path, sequence)
                    if run_session_recovery:
                        sequence, session_checks = verify_session_recovery(
                            socket_path, Path(env["HOBOT_CODE_STATE_DIR"]), workspace, sequence, runtime_checks or [],
                        )
                        with state.lock:
                            failures = list(state.errors)
                        if failures:
                            raise VerificationError("mock gateway validation failed: " + "; ".join(failures))
                        reports["session-recovery"] = acceptance_report(package, snapshot, manifest_sha256, "session-recovery", session_checks)
                        print("PASS session recovery: compaction, forced interruption, exact resume, history edit")
                    if run_extension_safety:
                        if runtime_result is None:
                            raise VerificationError("extension safety runtime evidence is unavailable")
                        sequence, extension_checks = verify_extension_safety(
                            socket_path, Path(env["HOBOT_CODE_STATE_DIR"]), workspace, sequence, runtime_result,
                        )
                        with state.lock:
                            failures = list(state.errors)
                        if failures:
                            raise VerificationError("mock gateway validation failed: " + "; ".join(failures))
                        reports["extension-safety"] = acceptance_report(package, snapshot, manifest_sha256, "extension-safety", extension_checks)
                        print("PASS extension safety: packaged resources, parallel tools, permission hook, workspace write lease")
                    if run_tui_basics:
                        tui_checks = verify_tui_basics(package, env, workspace, state)
                        reports["tui-basics"] = acceptance_report(package, snapshot, manifest_sha256, "tui-basics", tui_checks)
                        print("PASS TUI basics: ordinary user, Chinese input, thinking, editor, persistent detach and reattach")
                    return reports
                except Exception:
                    log.flush()
                    detail = sanitized_log(log_path)
                    if detail:
                        print("--- isolated agentd log ---", file=sys.stderr)
                        print(detail, file=sys.stderr)
                    raise
                finally:
                    for event in state.release.values():
                        event.set()
                    if process.poll() is None and socket_path.exists():
                        try:
                            rpc(socket_path, "daemon.shutdown", {"force": True}, 99999)
                        except Exception:
                            pass
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.terminate()
                        try:
                            process.wait(timeout=2)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait(timeout=2)
    finally:
        for event in state.release.values():
            event.set()
        gateway.shutdown()
        gateway.server_close()
        gateway_thread.join(timeout=2)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package-root", required=True, type=Path, help="extracted hobot-code Linux ARM64 package")
    parser.add_argument("--output", type=Path, help="write the private sanitized model-egress report")
    parser.add_argument("--rpc-output", type=Path, help="also run RPC/Side Agent acceptance and write its private report")
    parser.add_argument("--session-output", type=Path, help="also run compaction/session recovery acceptance and write its private report")
    parser.add_argument("--extension-output", type=Path, help="also run extension/resource safety acceptance and write its private report")
    parser.add_argument("--tui-output", type=Path, help="also run ordinary-user PTY/TUI acceptance and write its private report")
    parser.add_argument("--readiness-output", type=Path, help="also run read-only diagnostics and bounded repair acceptance")
    args = parser.parse_args()
    try:
        package_root = args.package_root.resolve()
        if args.output:
            validate_report_destination(package_root, args.output)
        if args.rpc_output:
            validate_report_destination(package_root, args.rpc_output)
        if args.session_output:
            validate_report_destination(package_root, args.session_output)
        if args.extension_output:
            validate_report_destination(package_root, args.extension_output)
        if args.tui_output:
            validate_report_destination(package_root, args.tui_output)
        if args.readiness_output:
            validate_report_destination(package_root, args.readiness_output)
        destinations = [
            report_destination(path)
            for path in (args.output, args.rpc_output, args.session_output, args.extension_output, args.tui_output, args.readiness_output)
            if path
        ]
        if len(destinations) != len(set(destinations)):
            raise VerificationError("acceptance reports must use different output paths")
        reports = verify(
            package_root,
            run_rpc_background=args.rpc_output is not None,
            run_session_recovery=args.session_output is not None,
            run_extension_safety=args.extension_output is not None,
            run_tui_basics=args.tui_output is not None,
            run_readiness_diagnostics=args.readiness_output is not None,
        )
        if args.output:
            private_report(args.output, reports["model-egress-runtime"])
            print(f"WROTE {args.output}")
        if args.rpc_output:
            private_report(args.rpc_output, reports["rpc-background"])
            print(f"WROTE {args.rpc_output}")
        if args.session_output:
            private_report(args.session_output, reports["session-recovery"])
            print(f"WROTE {args.session_output}")
        if args.extension_output:
            private_report(args.extension_output, reports["extension-safety"])
            print(f"WROTE {args.extension_output}")
        if args.tui_output:
            private_report(args.tui_output, reports["tui-basics"])
            print(f"WROTE {args.tui_output}")
        if args.readiness_output:
            private_report(args.readiness_output, reports["readiness-diagnostics"])
            print(f"WROTE {args.readiness_output}")
    except VerificationError as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
