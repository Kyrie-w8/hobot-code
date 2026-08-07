from __future__ import annotations

from typing import Any, Dict, List, Mapping, Optional

from edge_agent.providers.base import HttpProvider, JsonTransport, ProviderError
from edge_agent.types import ProviderRequest, ProviderResponse, ToolCall


class AnthropicProvider(HttpProvider):
    name = "anthropic"

    def __init__(
        self,
        api_key: str,
        base_url: str = "https://api.anthropic.com",
        timeout: float = 60.0,
        api_version: str = "2023-06-01",
        transport: Optional[JsonTransport] = None,
    ) -> None:
        super().__init__(base_url, api_key, timeout, transport)
        self.api_version = api_version

    @staticmethod
    def _messages(request: ProviderRequest) -> List[Dict[str, Any]]:
        messages: List[Dict[str, Any]] = []
        for message in request.messages:
            if message.role == "user":
                blocks: List[Dict[str, Any]] = [{"type": "text", "text": message.content}]
                messages.append({"role": "user", "content": blocks})
            elif message.role == "assistant":
                blocks = []
                if message.content:
                    blocks.append({"type": "text", "text": message.content})
                blocks.extend(
                    {
                        "type": "tool_use",
                        "id": call.id,
                        "name": call.name,
                        "input": call.arguments,
                    }
                    for call in message.tool_calls
                )
                messages.append({"role": "assistant", "content": blocks})
            elif message.role == "tool":
                block = {
                    "type": "tool_result",
                    "tool_use_id": message.tool_call_id,
                    "content": message.content,
                }
                if messages and messages[-1]["role"] == "user" and all(
                    item.get("type") == "tool_result"
                    for item in messages[-1]["content"]
                ):
                    messages[-1]["content"].append(block)
                else:
                    messages.append({"role": "user", "content": [block]})
        return messages

    @classmethod
    def build_payload(cls, request: ProviderRequest) -> Dict[str, Any]:
        payload: Dict[str, Any] = {
            "model": request.model,
            "system": request.system_prompt,
            "messages": cls._messages(request),
            "max_tokens": int(request.settings.get("max_tokens", 1024)),
        }
        if request.tools:
            payload["tools"] = [
                {
                    "name": tool["name"],
                    "description": tool.get("description", ""),
                    "input_schema": tool.get("parameters", {"type": "object"}),
                }
                for tool in request.tools
            ]
        for key in ("temperature", "tool_choice", "top_k", "top_p"):
            if key in request.settings:
                payload[key] = request.settings[key]
        return payload

    @staticmethod
    def parse_response(data: Mapping[str, Any]) -> ProviderResponse:
        content = data.get("content", [])
        if not isinstance(content, list):
            raise ProviderError("Anthropic response.content must be a list")
        text: List[str] = []
        calls: List[ToolCall] = []
        for block in content:
            if not isinstance(block, Mapping):
                continue
            if block.get("type") == "text":
                text.append(str(block.get("text") or ""))
            elif block.get("type") == "tool_use":
                value = block.get("input") or {}
                if not isinstance(value, Mapping):
                    raise ProviderError("Anthropic tool input must be an object")
                calls.append(
                    ToolCall(
                        id=str(block.get("id") or ""),
                        name=str(block.get("name") or ""),
                        arguments=dict(value),
                    )
                )
        usage = data.get("usage") if isinstance(data.get("usage"), Mapping) else {}
        return ProviderResponse(
            content="".join(text),
            tool_calls=calls,
            finish_reason=str(data.get("stop_reason") or "end_turn"),
            usage=dict(usage),
            raw=data,
        )

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        if not self.api_key:
            raise ProviderError("Anthropic API key is empty")
        data = await self.transport.post_json(
            self.base_url + "/v1/messages",
            {"x-api-key": self.api_key, "anthropic-version": self.api_version},
            self.build_payload(request),
            self.timeout,
        )
        return self.parse_response(data)
