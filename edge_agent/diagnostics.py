from __future__ import annotations

import importlib.util
import shutil
from pathlib import Path
from typing import Any, Dict, Mapping

from edge_agent.skills import SkillCatalog
from edge_agent.tools import ToolRegistry, system_snapshot


def diagnose(
    config: Mapping[str, Any], tools: ToolRegistry, skills: SkillCatalog
) -> Dict[str, Any]:
    provider = config.get("model", {}).get("provider")
    key_required = provider in ("openai_responses", "anthropic", "gemini")
    key_present = bool(config.get("model", {}).get("api_key"))
    checks = {
        "yaml_available": importlib.util.find_spec("yaml") is not None,
        "provider_key_configured": (not key_required) or key_present,
        "memory_parent_exists": Path(
            config.get("memory", {}).get("path", "./var/sessions.db")
        ).expanduser().resolve().parent.exists(),
    }
    return {
        "ok": all(checks.values()),
        "board": system_snapshot(),
        "profile": config.get("board", {}).get("profile", "generic"),
        "provider": provider,
        "model": config.get("model", {}).get("name"),
        "tools": tools.names(),
        "skills": skills.names(),
        "executables": {
            "docker": shutil.which("docker"),
            "hrt_model_exec": shutil.which("hrt_model_exec"),
            "ros2": shutil.which("ros2"),
        },
        "checks": checks,
    }
