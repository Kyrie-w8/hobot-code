from __future__ import annotations

import json
import os
import re
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Mapping, Optional


class ConfigError(ValueError):
    pass


_ENV_PATTERN = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}")


@dataclass(frozen=True)
class LoadedConfig:
    data: Dict[str, Any]
    source_path: Path
    board_path: Optional[Path] = None

    @property
    def board_profile(self) -> str:
        return str(self.data.get("board", {}).get("profile", "generic"))


def _read_document(path: Path) -> Dict[str, Any]:
    if not path.exists():
        raise ConfigError("configuration file not found: %s" % path)
    text = path.read_text(encoding="utf-8")
    if path.suffix.lower() == ".json":
        value = json.loads(text)
    else:
        try:
            import yaml
        except ImportError as exc:
            raise ConfigError(
                "PyYAML is required for YAML configuration; install edge-agent-runtime"
            ) from exc
        value = yaml.safe_load(text)
    if not isinstance(value, dict):
        raise ConfigError("configuration root must be an object: %s" % path)
    return value


def _load_with_extends(path: Path, stack: Optional[tuple] = None) -> Dict[str, Any]:
    chain = stack or ()
    if path in chain:
        raise ConfigError("configuration extends cycle: %s" % path)
    value = _read_document(path)
    extends = value.pop("extends", None)
    if not extends:
        return value
    parents = extends if isinstance(extends, list) else [extends]
    merged: Dict[str, Any] = {}
    for parent in parents:
        parent_path = (path.parent / str(parent)).expanduser().resolve()
        merged = deep_merge(merged, _load_with_extends(parent_path, chain + (path,)))
    return deep_merge(merged, value)


def deep_merge(base: Mapping[str, Any], override: Mapping[str, Any]) -> Dict[str, Any]:
    merged: Dict[str, Any] = deepcopy(dict(base))
    for key, value in override.items():
        if isinstance(value, Mapping) and isinstance(merged.get(key), Mapping):
            merged[key] = deep_merge(merged[key], value)
        else:
            merged[key] = deepcopy(value)
    return merged


def _expand_string(value: str, environ: Mapping[str, str]) -> str:
    def replace(match: re.Match) -> str:
        name, default = match.group(1), match.group(2)
        if name in environ:
            return environ[name]
        if default is not None:
            return default
        raise ConfigError("missing environment variable: %s" % name)

    return _ENV_PATTERN.sub(replace, value)


def expand_environment(value: Any, environ: Optional[Mapping[str, str]] = None) -> Any:
    env = environ if environ is not None else os.environ
    if isinstance(value, str):
        return _expand_string(value, env)
    if isinstance(value, list):
        return [expand_environment(item, env) for item in value]
    if isinstance(value, dict):
        return {key: expand_environment(item, env) for key, item in value.items()}
    return value


def validate_config(data: Mapping[str, Any]) -> None:
    runtime = data.get("runtime")
    model = data.get("model")
    if not isinstance(runtime, Mapping):
        raise ConfigError("runtime configuration is required")
    if not isinstance(model, Mapping):
        raise ConfigError("model configuration is required")
    if not model.get("provider"):
        raise ConfigError("model.provider is required")
    if not model.get("name"):
        raise ConfigError("model.name is required")
    max_steps = runtime.get("max_steps", 8)
    timeout = runtime.get("tool_timeout_seconds", 15)
    if not isinstance(max_steps, int) or max_steps < 1:
        raise ConfigError("runtime.max_steps must be a positive integer")
    if not isinstance(timeout, (int, float)) or timeout <= 0:
        raise ConfigError("runtime.tool_timeout_seconds must be positive")
    security = data.get("security", {})
    if not isinstance(security, Mapping):
        raise ConfigError("security must be an object")


def load_config(
    config_path: str,
    board_path: Optional[str] = None,
    environ: Optional[Mapping[str, str]] = None,
) -> LoadedConfig:
    source = Path(config_path).expanduser().resolve()
    data = _load_with_extends(source)
    resolved_board: Optional[Path] = None
    if board_path:
        resolved_board = Path(board_path).expanduser().resolve()
        data = deep_merge(data, _load_with_extends(resolved_board))
    data = expand_environment(data, environ)
    validate_config(data)
    return LoadedConfig(data=data, source_path=source, board_path=resolved_board)
