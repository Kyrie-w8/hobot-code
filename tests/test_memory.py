from pathlib import Path

from edge_agent.memory import SessionStore
from edge_agent.types import Message, ToolCall


def test_session_roundtrip_and_sft_export(tmp_path: Path):
    store = SessionStore(str(tmp_path / "sessions.db"))
    try:
        session = store.ensure_session("s1")
        store.append_message(session, Message("user", "status"))
        store.append_message(
            session,
            Message(
                "assistant",
                tool_calls=[ToolCall("c1", "system.snapshot", {})],
            ),
        )
        store.append_message(
            session,
            Message("tool", "{\"ok\":true}", "system.snapshot", "c1"),
        )
        loaded = store.load_messages(session)
        assert loaded[1].tool_calls[0].name == "system.snapshot"
        exported = store.export_trajectory(session, tools=[{"name": "system.snapshot"}])
        assert len(exported["messages"]) == 3
        assert exported["tools"][0]["name"] == "system.snapshot"
    finally:
        store.close()
