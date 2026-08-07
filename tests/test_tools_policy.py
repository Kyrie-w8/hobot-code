import asyncio
from pathlib import Path

from edge_agent.policy import PolicyEngine
from edge_agent.tools import ToolRegistry, ToolSpec, make_read_text_tool
from edge_agent.types import ToolCall


def test_tool_validation_and_policy():
    registry = ToolRegistry()
    registry.register(
        ToolSpec(
            name="demo.write",
            description="demo",
            parameters={
                "type": "object",
                "properties": {"value": {"type": "string"}},
                "required": ["value"],
                "additionalProperties": False,
            },
            handler=lambda value: value,
            risk="write",
        )
    )
    policy = PolicyEngine(["demo.*"])
    result = asyncio.run(
        registry.execute(ToolCall("1", "demo.write", {"value": "x"}), policy)
    )
    assert not result.ok
    assert "ApprovalRequired" in result.error


def test_approval_handler_allows_write():
    registry = ToolRegistry()
    registry.register(
        ToolSpec(
            name="demo.write",
            description="demo",
            parameters={"type": "object", "properties": {}, "additionalProperties": False},
            handler=lambda: {"changed": True},
            risk="write",
        )
    )
    policy = PolicyEngine(["demo.*"])
    result = asyncio.run(
        registry.execute(
            ToolCall("1", "demo.write", {}),
            policy,
            approval_handler=lambda call, spec: True,
        )
    )
    assert result.ok
    assert result.output == {"changed": True}


def test_read_file_stays_inside_roots(tmp_path: Path):
    allowed = tmp_path / "allowed"
    allowed.mkdir()
    target = allowed / "x.txt"
    target.write_text("hello", encoding="utf-8")
    registry = ToolRegistry()
    registry.register(make_read_text_tool([str(allowed)]))
    policy = PolicyEngine(["filesystem.*"])
    ok = asyncio.run(
        registry.execute(
            ToolCall("1", "filesystem.read_text", {"path": str(target)}), policy
        )
    )
    assert ok.ok and ok.output["content"] == "hello"
    denied = asyncio.run(
        registry.execute(
            ToolCall("2", "filesystem.read_text", {"path": "/etc/hosts"}), policy
        )
    )
    assert not denied.ok
    assert "outside configured roots" in denied.error
