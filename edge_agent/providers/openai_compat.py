from __future__ import annotations

import json
from typing import Any, Dict, List, Mapping, Optional

from edge_agent.providers.base import HttpProvider, JsonTransport, ProviderError
from edge_agent.types import Message, ProviderRequest, ProviderResponse, ToolCall


class OpenAICompatibleProvider(HttpProvider):
    name = "openai_compatible"

    def __init__(
        self,
        api_key: str,
        base_url: str,
        timeout: float = 60.0,
        transport: Optional[JsonTransport] = None,
    ) -> None:
        super().__init__(base_url, api_key, timeout, transport)

    @staticmethod
    def _message(message: Message) -> Dict[str, Any]:
        value: Dict[str, Any] = {"role": message.role, "content": message.content}
        if message.name:
            value["name"] = message.name
        if message.tool_call_id:
            value["tool_call_id"] = message.tool_call_id
        if message.tool_calls:
            value["tool_calls"] = [
                {
                    "id": call.id,
                    "type": "function",
                    "function": {
                        "name": call.name,
                        "arguments": json.dumps(call.arguments, ensure_ascii=False),
                    },
                }
                for call in message.tool_calls
            ]
        return value

    @classmethod
    def build_payload(cls, request: ProviderRequest) -> Dict[str, Any]:
        messages = []
        if request.system_prompt:
            messages.append({"role": "system", "content": request.system_prompt})
        messages.extend(cls._message(message) for message in request.messages)
        payload: Dict[str, Any] = {"model": request.model, "messages": messages}
        if request.tools:
            payload["tools"] = [
                {
                    "type": "function",
                    "function": {
                        "name": tool["name"],
                        "description": tool.get("description", ""),
                        "parameters": tool.get("parameters", {"type": "object"}),
                        "strict": bool(tool.get("strict", True)),
                    },
                }
                for tool in request.tools
            ]
        for key in (
            "frequency_penalty",
            "max_tokens",
            "parallel_tool_calls",
            "presence_penalty",
            "seed",
            "temperature",
            "tool_choice",
            "top_p",
        ):
            if key in request.settings:
                payload[key] = request.settings[key]
        return payload

    @staticmethod
    def parse_response(data: Mapping[str, Any]) -> ProviderResponse:
        choices = data.get("choices")
        if not isinstance(choices, list) or not choices:
            raise ProviderError("OpenAI-compatible response has no choices")
        choice = choices[0]
        message = choice.get("message", {}) if isinstance(choice, Mapping) else {}
        tool_calls: List[ToolCall] = []
        for item in message.get("tool_calls", []):
            function = item.get("function", {})
            arguments = function.get("arguments") or "{}"
            try:
                parsed = json.loads(arguments) if isinstance(arguments, str) else arguments
            except json.JSONDecodeError as exc:
                raise ProviderError("invalid compatible-provider tool arguments") from exc
            if not isinstance(parsed, Mapping):
                raise ProviderError("compatible-provider tool arguments must be an object")
            tool_calls.append(
                ToolCall(
                    id=str(item.get("id") or ""),
                    name=str(function.get("name") or ""),
                    arguments=dict(parsed),
                )
            )
        usage = data.get("usage") if isinstance(data.get("usage"), Mapping) else {}
        return ProviderResponse(
            content=str(message.get("content") or ""),
            tool_calls=tool_calls,
            finish_reason=str(choice.get("finish_reason") or "stop"),
            usage=dict(usage),
            raw=data,
        )

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        headers = {"Authorization": "Bearer " + self.api_key} if self.api_key else {}
        data = await self.transport.post_json(
            self.base_url + "/chat/completions",
            headers,
            self.build_payload(request),
            self.timeout,
        )
        return self.parse_response(data)
