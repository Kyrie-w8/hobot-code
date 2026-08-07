from __future__ import annotations

from typing import Any, Dict, List, Mapping, Optional, Tuple

from edge_agent.providers.base import HttpProvider, JsonTransport, ProviderError
from edge_agent.types import Message, ProviderRequest, ProviderResponse, ToolCall


class GeminiProvider(HttpProvider):
    """Google Gemini Interactions API adapter using the current steps schema."""

    name = "gemini"

    def __init__(
        self,
        api_key: str,
        base_url: str = "https://generativelanguage.googleapis.com/v1beta",
        timeout: float = 60.0,
        api_revision: str = "",
        transport: Optional[JsonTransport] = None,
    ) -> None:
        super().__init__(base_url, api_key, timeout, transport)
        self.api_revision = api_revision

    @staticmethod
    def _history(messages: List[Message]) -> List[Dict[str, Any]]:
        steps: List[Dict[str, Any]] = []
        for message in messages:
            if message.role == "user":
                steps.append(
                    {
                        "type": "user_input",
                        "content": [{"type": "text", "text": message.content}],
                    }
                )
            elif message.role == "assistant":
                if message.content:
                    steps.append(
                        {
                            "type": "model_output",
                            "content": [{"type": "text", "text": message.content}],
                        }
                    )
                steps.extend(
                    {
                        "type": "function_call",
                        "id": call.id,
                        "name": call.name,
                        "arguments": call.arguments,
                    }
                    for call in message.tool_calls
                )
            elif message.role == "tool":
                steps.append(
                    {
                        "type": "function_result",
                        "name": message.name or "tool",
                        "call_id": message.tool_call_id,
                        "result": [{"type": "text", "text": message.content}],
                    }
                )
        return steps

    @staticmethod
    def _tool_results(messages: List[Message]) -> List[Dict[str, Any]]:
        return [
            {
                "type": "function_result",
                "name": message.name or "tool",
                "call_id": message.tool_call_id,
                "result": [{"type": "text", "text": message.content}],
            }
            for message in messages
            if message.role == "tool" and message.tool_call_id
        ]

    @classmethod
    def build_payload(
        cls, request: ProviderRequest
    ) -> Tuple[Dict[str, Any], List[Dict[str, Any]]]:
        settings = dict(request.settings)
        store = bool(settings.get("store", False))
        pending_ids = {
            str(step.get("id") or "")
            for step in request.state.get("steps", [])
            if isinstance(step, Mapping) and step.get("type") == "function_call"
        }
        results = [
            item
            for item in cls._tool_results(request.messages)
            if not pending_ids or item.get("call_id") in pending_ids
        ]
        payload: Dict[str, Any] = {
            "model": request.model,
            "system_instruction": request.system_prompt,
            "store": store,
        }
        if request.state.get("steps") and results:
            if store and request.state.get("previous_interaction_id"):
                input_items = results
                payload["previous_interaction_id"] = request.state[
                    "previous_interaction_id"
                ]
            else:
                input_items = (
                    list(request.state.get("input_items", []))
                    + list(request.state.get("steps", []))
                    + results
                )
        else:
            input_items = cls._history(request.messages)
        payload["input"] = input_items
        if request.tools:
            payload["tools"] = [
                {
                    "type": "function",
                    "name": tool["name"],
                    "description": tool.get("description", ""),
                    "parameters": tool.get("parameters", {"type": "object"}),
                }
                for tool in request.tools
            ]
        generation: Dict[str, Any] = {}
        mapping = {
            "max_tokens": "max_output_tokens",
            "seed": "seed",
            "thinking_level": "thinking_level",
        }
        for source, target in mapping.items():
            if source in settings:
                generation[target] = settings[source]
        if generation:
            payload["generation_config"] = generation
        return payload, input_items

    @staticmethod
    def parse_response(
        data: Mapping[str, Any], input_items: Optional[List[Dict[str, Any]]] = None
    ) -> ProviderResponse:
        steps = data.get("steps")
        if not isinstance(steps, list):
            raise ProviderError("Gemini interaction.steps must be a list")
        text: List[str] = []
        calls: List[ToolCall] = []
        for step in steps:
            if not isinstance(step, Mapping):
                continue
            if step.get("type") == "model_output":
                for part in step.get("content", []):
                    if isinstance(part, Mapping) and part.get("type") == "text":
                        text.append(str(part.get("text") or ""))
            elif step.get("type") == "function_call":
                arguments = step.get("arguments") or {}
                if not isinstance(arguments, Mapping):
                    raise ProviderError("Gemini function arguments must be an object")
                calls.append(
                    ToolCall(
                        id=str(step.get("id") or ""),
                        name=str(step.get("name") or ""),
                        arguments=dict(arguments),
                    )
                )
        usage = data.get("usage") if isinstance(data.get("usage"), Mapping) else {}
        return ProviderResponse(
            content="".join(text),
            tool_calls=calls,
            finish_reason=str(data.get("status") or "completed"),
            usage=dict(usage),
            state={
                "previous_interaction_id": data.get("id"),
                "steps": steps,
                "input_items": list(input_items or []),
            },
            raw=data,
        )

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        if not self.api_key:
            raise ProviderError("Gemini API key is empty")
        payload, input_items = self.build_payload(request)
        headers = {"x-goog-api-key": self.api_key}
        if self.api_revision:
            headers["Api-Revision"] = self.api_revision
        data = await self.transport.post_json(
            self.base_url + "/interactions",
            headers,
            payload,
            self.timeout,
        )
        return self.parse_response(data, input_items)
