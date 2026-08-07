from __future__ import annotations

from dataclasses import asdict, dataclass, field
from typing import Any, Dict, List, Mapping, Optional


@dataclass
class ToolCall:
    id: str
    name: str
    arguments: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> "ToolCall":
        arguments = value.get("arguments") or {}
        if not isinstance(arguments, dict):
            raise ValueError("tool-call arguments must be an object")
        return cls(
            id=str(value.get("id") or ""),
            name=str(value.get("name") or ""),
            arguments=dict(arguments),
        )


@dataclass
class Message:
    role: str
    content: str = ""
    name: Optional[str] = None
    tool_call_id: Optional[str] = None
    tool_calls: List[ToolCall] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        value: Dict[str, Any] = {"role": self.role, "content": self.content}
        if self.name:
            value["name"] = self.name
        if self.tool_call_id:
            value["tool_call_id"] = self.tool_call_id
        if self.tool_calls:
            value["tool_calls"] = [call.to_dict() for call in self.tool_calls]
        return value

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> "Message":
        return cls(
            role=str(value.get("role") or ""),
            content=str(value.get("content") or ""),
            name=str(value["name"]) if value.get("name") else None,
            tool_call_id=(
                str(value["tool_call_id"]) if value.get("tool_call_id") else None
            ),
            tool_calls=[
                ToolCall.from_dict(item) for item in value.get("tool_calls", [])
            ],
        )


@dataclass
class ProviderRequest:
    model: str
    system_prompt: str
    messages: List[Message]
    tools: List[Dict[str, Any]] = field(default_factory=list)
    settings: Dict[str, Any] = field(default_factory=dict)
    state: Dict[str, Any] = field(default_factory=dict)


@dataclass
class ProviderResponse:
    content: str = ""
    tool_calls: List[ToolCall] = field(default_factory=list)
    finish_reason: str = "stop"
    usage: Dict[str, Any] = field(default_factory=dict)
    state: Dict[str, Any] = field(default_factory=dict)
    raw: Optional[Mapping[str, Any]] = None

    def as_message(self) -> Message:
        return Message(
            role="assistant", content=self.content, tool_calls=list(self.tool_calls)
        )


@dataclass
class AgentResult:
    session_id: str
    content: str
    messages: List[Message]
    steps: int
    usage: Dict[str, Any] = field(default_factory=dict)
