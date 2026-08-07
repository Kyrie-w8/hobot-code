from __future__ import annotations

import asyncio
import inspect
import json
import os
import platform
import shutil
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Awaitable, Callable, Dict, Iterable, List, Mapping, Optional

from edge_agent.policy import PolicyEngine
from edge_agent.types import ToolCall


ToolHandler = Callable[..., Any]
ApprovalHandler = Callable[[ToolCall, "ToolSpec"], Any]


class ToolError(RuntimeError):
    pass


@dataclass
class ToolSpec:
    name: str
    description: str
    parameters: Dict[str, Any]
    handler: ToolHandler
    risk: str = "read"
    timeout_seconds: float = 15.0
    board_profiles: List[str] = field(default_factory=list)
    strict: bool = True

    def model_schema(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "description": self.description,
            "parameters": self.parameters,
            "strict": self.strict,
        }


@dataclass
class ToolExecution:
    call_id: str
    name: str
    ok: bool
    output: Any = None
    error: Optional[str] = None
    duration_ms: float = 0.0

    def to_dict(self) -> Dict[str, Any]:
        value = {
            "ok": self.ok,
            "tool": self.name,
            "duration_ms": round(self.duration_ms, 3),
        }
        if self.ok:
            value["output"] = self.output
        else:
            value["error"] = self.error
        return value

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), ensure_ascii=False, default=str)


def _matches_type(value: Any, expected: Any) -> bool:
    if isinstance(expected, list):
        return any(_matches_type(value, item) for item in expected)
    mapping = {
        "array": list,
        "boolean": bool,
        "integer": int,
        "null": type(None),
        "number": (int, float),
        "object": dict,
        "string": str,
    }
    target = mapping.get(expected)
    if target is None:
        return True
    if expected in ("integer", "number") and isinstance(value, bool):
        return False
    return isinstance(value, target)


def validate_arguments(arguments: Mapping[str, Any], schema: Mapping[str, Any]) -> None:
    if schema.get("type") == "object" and not isinstance(arguments, Mapping):
        raise ToolError("tool arguments must be an object")
    required = schema.get("required", [])
    for key in required:
        if key not in arguments:
            raise ToolError("missing required argument: %s" % key)
    properties = schema.get("properties", {})
    if schema.get("additionalProperties") is False:
        unknown = set(arguments) - set(properties)
        if unknown:
            raise ToolError("unknown arguments: %s" % ", ".join(sorted(unknown)))
    for key, value in arguments.items():
        rule = properties.get(key)
        if not isinstance(rule, Mapping):
            continue
        if not _matches_type(value, rule.get("type")):
            raise ToolError("argument %s has the wrong type" % key)
        if "enum" in rule and value not in rule["enum"]:
            raise ToolError("argument %s is not an allowed value" % key)
        if isinstance(value, str) and len(value) > int(rule.get("maxLength", len(value))):
            raise ToolError("argument %s is too long" % key)


class ToolRegistry:
    def __init__(self) -> None:
        self._tools: Dict[str, ToolSpec] = {}

    def register(self, spec: ToolSpec) -> None:
        if spec.name in self._tools:
            raise ValueError("tool already registered: %s" % spec.name)
        if spec.risk not in ("read", "write", "dangerous"):
            raise ValueError("invalid tool risk: %s" % spec.risk)
        self._tools[spec.name] = spec

    def get(self, name: str) -> ToolSpec:
        try:
            return self._tools[name]
        except KeyError as exc:
            raise ToolError("unknown tool: %s" % name) from exc

    def schemas(self, board_profile: str = "generic") -> List[Dict[str, Any]]:
        return [
            spec.model_schema()
            for spec in self._tools.values()
            if not spec.board_profiles or board_profile in spec.board_profiles
        ]

    def names(self) -> List[str]:
        return sorted(self._tools)

    def specs(self) -> List[ToolSpec]:
        return list(self._tools.values())

    async def execute(
        self,
        call: ToolCall,
        policy: PolicyEngine,
        board_profile: str = "generic",
        approval_handler: Optional[ApprovalHandler] = None,
    ) -> ToolExecution:
        started = time.monotonic()
        try:
            spec = self.get(call.name)
            if spec.board_profiles and board_profile not in spec.board_profiles:
                raise ToolError(
                    "%s is unavailable on board profile %s"
                    % (call.name, board_profile)
                )
            validate_arguments(call.arguments, spec.parameters)
            decision = policy.decide(spec.name, spec.risk)
            approved = False
            if decision.requires_approval and approval_handler:
                result = approval_handler(call, spec)
                approved = bool(await result) if inspect.isawaitable(result) else bool(result)
            policy.authorize(spec.name, spec.risk, approved=approved)

            async def invoke() -> Any:
                result = spec.handler(**call.arguments)
                if inspect.isawaitable(result):
                    return await result
                return result

            output = await asyncio.wait_for(invoke(), timeout=spec.timeout_seconds)
            return ToolExecution(
                call_id=call.id,
                name=call.name,
                ok=True,
                output=output,
                duration_ms=(time.monotonic() - started) * 1000,
            )
        except Exception as exc:
            return ToolExecution(
                call_id=call.id,
                name=call.name,
                ok=False,
                error="%s: %s" % (type(exc).__name__, exc),
                duration_ms=(time.monotonic() - started) * 1000,
            )


def _read_text(path: Path) -> Optional[str]:
    try:
        return path.read_text(encoding="utf-8").replace("\x00", "").strip()
    except (OSError, UnicodeDecodeError):
        return None


def system_snapshot() -> Dict[str, Any]:
    model = _read_text(Path("/proc/device-tree/model")) or platform.machine()
    meminfo: Dict[str, int] = {}
    try:
        for line in Path("/proc/meminfo").read_text(encoding="utf-8").splitlines():
            key, raw = line.split(":", 1)
            if key in ("MemTotal", "MemAvailable"):
                meminfo[key] = int(raw.strip().split()[0]) * 1024
    except (OSError, ValueError):
        pass
    root = shutil.disk_usage("/")
    bpu_cores = 0
    bpu_class = Path("/sys/class/bpu")
    if bpu_class.exists():
        bpu_cores = len(list(bpu_class.glob("bpu_core*")))
    if bpu_cores == 0:
        bpu_cores = len(list(Path("/sys/class/devfreq").glob("*.bpu")))
    temperatures = []
    for zone in Path("/sys/class/thermal").glob("thermal_zone*/temp"):
        try:
            temperatures.append(int(zone.read_text().strip()) / 1000.0)
        except (OSError, ValueError):
            continue
    return {
        "hostname": platform.node(),
        "board_model": model,
        "architecture": platform.machine(),
        "kernel": platform.release(),
        "python": platform.python_version(),
        "cpu_count": os.cpu_count(),
        "memory_total_bytes": meminfo.get("MemTotal"),
        "memory_available_bytes": meminfo.get("MemAvailable"),
        "root_disk_free_bytes": root.free,
        "bpu_cores": bpu_cores,
        "max_temperature_c": max(temperatures) if temperatures else None,
    }


def _inside(path: Path, roots: Iterable[Path]) -> bool:
    return any(path == root or root in path.parents for root in roots)


def make_read_text_tool(allowed_roots: Iterable[str]) -> ToolSpec:
    roots = [Path(root).expanduser().resolve() for root in allowed_roots]

    def read_text(path: str, max_bytes: int = 65536) -> Dict[str, Any]:
        target = Path(path).expanduser().resolve()
        if not _inside(target, roots):
            raise ToolError("path is outside configured roots")
        if max_bytes < 1 or max_bytes > 1_048_576:
            raise ToolError("max_bytes must be between 1 and 1048576")
        with target.open("rb") as handle:
            content = handle.read(max_bytes + 1)
        truncated = len(content) > max_bytes
        return {
            "path": str(target),
            "content": content[:max_bytes].decode("utf-8", errors="replace"),
            "truncated": truncated,
        }

    return ToolSpec(
        name="filesystem.read_text",
        description="Read a UTF-8 text file inside an explicitly allowed directory.",
        parameters={
            "type": "object",
            "properties": {
                "path": {"type": "string", "maxLength": 4096},
                "max_bytes": {"type": "integer"},
            },
            "required": ["path"],
            "additionalProperties": False,
        },
        handler=read_text,
        risk="read",
        strict=False,
    )


def create_builtin_registry(config: Mapping[str, Any]) -> ToolRegistry:
    runtime = config.get("runtime", {})
    timeout = float(runtime.get("tool_timeout_seconds", 15))
    registry = ToolRegistry()
    registry.register(
        ToolSpec(
            name="system.snapshot",
            description="Read current board, CPU, memory, disk, BPU, and temperature status.",
            parameters={
                "type": "object",
                "properties": {},
                "required": [],
                "additionalProperties": False,
            },
            handler=system_snapshot,
            risk="read",
            timeout_seconds=timeout,
        )
    )
    roots = config.get("security", {}).get("filesystem_roots", [])
    if roots:
        file_tool = make_read_text_tool(roots)
        file_tool.timeout_seconds = timeout
        registry.register(file_tool)
    return registry
