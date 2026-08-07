import asyncio
from pathlib import Path

import pytest

from edge_agent.agent import AgentLimitError, AgentRuntime
from edge_agent.memory import SessionStore
from edge_agent.policy import PolicyEngine
from edge_agent.prompt import PromptComposer
from edge_agent.providers.mock import ScriptedProvider
from edge_agent.skills import SkillCatalog
from edge_agent.tools import create_builtin_registry
from edge_agent.types import ProviderResponse, ToolCall


def runtime_config(tmp_path: Path):
    return {
        "runtime": {"max_steps": 3, "tool_timeout_seconds": 5},
        "board": {"profile": "x5", "model": "test"},
        "agent": {"name": "TestAgent", "skills": []},
        "model": {"provider": "mock", "name": "mock", "settings": {}},
        "memory": {"path": str(tmp_path / "memory.db")},
        "security": {"allowed_tools": ["system.*"]},
    }


def test_agent_executes_tool_and_records_trajectory(tmp_path: Path):
    config = runtime_config(tmp_path)
    provider = ScriptedProvider(
        [
            ProviderResponse(tool_calls=[ToolCall("c1", "system.snapshot", {})]),
            ProviderResponse(content="snapshot complete"),
        ]
    )
    store = SessionStore(config["memory"]["path"])
    prompt = PromptComposer()
    prompt.add("base", 1, "Agent {{agent_name}} on {{board_profile}}")
    runtime = AgentRuntime(
        config,
        provider,
        create_builtin_registry(config),
        PolicyEngine.from_config(config["security"]),
        store,
        prompt,
        SkillCatalog({}),
    )
    try:
        result = asyncio.run(runtime.run("inspect"))
        assert result.content == "snapshot complete"
        assert result.steps == 2
        assert [message.role for message in result.messages] == [
            "user",
            "assistant",
            "tool",
            "assistant",
        ]
        assert len(store.load_messages(result.session_id)) == 4
        assert provider.requests[1].messages[-1].role == "tool"
    finally:
        store.close()


def test_agent_stops_at_step_limit(tmp_path: Path):
    config = runtime_config(tmp_path)
    config["runtime"]["max_steps"] = 1
    provider = ScriptedProvider(
        [ProviderResponse(tool_calls=[ToolCall("c1", "system.snapshot", {})])]
    )
    store = SessionStore(config["memory"]["path"])
    runtime = AgentRuntime(
        config,
        provider,
        create_builtin_registry(config),
        PolicyEngine.from_config(config["security"]),
        store,
        PromptComposer(),
    )
    try:
        with pytest.raises(AgentLimitError):
            asyncio.run(runtime.run("inspect"))
    finally:
        store.close()
