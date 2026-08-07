from __future__ import annotations

import json
from typing import Any, Dict, List, Mapping, Optional, Tuple

from edge_agent.providers.base import HttpProvider, JsonTransport, ProviderError
from edge_agent.types import Message, ProviderRequest, ProviderResponse, ToolCall


class OpenAIResponsesProvider(HttpProvider):
    name = "openai_responses"

    def __init__(
        self,
        api_key: str,
        base_url: str = "https://api.openai.com/v1",
        timeout: float = 60.0,
        transport: Optional[JsonTransport] = None,
    ) -> None:
        super().__init__(base_url, api_key, timeout, transport)

    @staticmethod
    def _message_items(messages: List[Message]) -> List[Dict[str, Any]]:
        items: List[Dict[str, Any]] = []
        for message in messages:
            if message.role in ("user", "assistant") and message.content:
                items.append({"role": message.role, "content": message.content})
            if message.role == "assistant":
                for call in message.tool_calls:
                    items.append(
                        {
                            "type": "function_call",
                            "call_id": call.id,
                            "name": call.name,
                            "arguments": json.dumps(call.arguments, ensure_ascii=False),
                        }
                    )
            elif message.role == "tool":
                items.append(
                    {
                        "type": "function_call_output",
                        "call_id": message.tool_call_id,
                        "output": message.content,
                    }
                )
        return items

    @staticmethod
    def _tool_output_items(messages: List[Message]) -> List[Dict[str, Any]]:
        return [
            {
                "type": "function_call_output",
                "call_id": message.tool_call_id,
                "output": message.content,
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
        prior = request.state
        pending_ids = {
            str(item.get("call_id") or item.get("id") or "")
            for item in request.state.get("response_output", [])
            if isinstance(item, Mapping) and item.get("type") == "function_call"
        }
        outputs = [
            item
            for item in cls._tool_output_items(request.messages)
            if not pending_ids or item.get("call_id") in pending_ids
        ]
        payload: Dict[str, Any] = {
            "model": request.model,
            "instructions": request.system_prompt,
            "store": store,
        }
        input_items: List[Dict[str, Any]]
        if prior.get("response_output") and outputs:
            if store and prior.get("previous_response_id"):
                input_items = outputs
                payload["previous_response_id"] = prior["previous_response_id"]
            else:
                input_items = (
                    list(prior.get("input_items", []))
                    + list(prior.get("response_output", []))
                    + outputs
                )
        else:
            input_items = cls._message_items(request.messages)
        payload["input"] = input_items
        if request.tools:
            payload["tools"] = [
                {
                    "type": "function",
                    "name": tool["name"],
                    "description": tool.get("description", ""),
                    "parameters": tool.get("parameters", {"type": "object"}),
                    "strict": bool(tool.get("strict", True)),
                }
                for tool in request.tools
            ]
        for key in (
            "max_output_tokens",
            "parallel_tool_calls",
            "reasoning",
            "temperature",
            "text",
            "tool_choice",
            "top_p",
        ):
            if key in settings:
                payload[key] = settings[key]
        return payload, input_items

    @staticmethod
    def parse_response(
        data: Mapping[str, Any], input_items: List[Dict[str, Any]], store: bool
    ) -> ProviderResponse:
        output = data.get("output", [])
        if not isinstance(output, list):
            raise ProviderError("OpenAI response.output must be a list")
        text_parts: List[str] = []
        tool_calls: List[ToolCall] = []
        for item in output:
            if not isinstance(item, Mapping):
                continue
            item_type = item.get("type")
            if item_type == "function_call":
                arguments = item.get("arguments") or "{}"
                try:
                    parsed = json.loads(arguments) if isinstance(arguments, str) else arguments
                except json.JSONDecodeError as exc:
                    raise ProviderError("invalid OpenAI tool arguments") from exc
                if not isinstance(parsed, Mapping):
                    raise ProviderError("OpenAI tool arguments must be an object")
                tool_calls.append(
                    ToolCall(
                        id=str(item.get("call_id") or item.get("id") or ""),
                        name=str(item.get("name") or ""),
                        arguments=dict(parsed),
                    )
                )
            elif item_type == "message":
                for part in item.get("content", []):
                    if isinstance(part, Mapping) and part.get("type") == "output_text":
                        text_parts.append(str(part.get("text") or ""))
        if not text_parts and data.get("output_text"):
            text_parts.append(str(data["output_text"]))
        usage = data.get("usage") if isinstance(data.get("usage"), Mapping) else {}
        state = {
            "previous_response_id": data.get("id"),
            "response_output": output,
            "input_items": input_items,
            "store": store,
        }
        return ProviderResponse(
            content="".join(text_parts),
            tool_calls=tool_calls,
            finish_reason=str(data.get("status") or "completed"),
            usage=dict(usage),
            state=state,
            raw=data,
        )

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        if not self.api_key:
            raise ProviderError("OpenAI API key is empty")
        payload, input_items = self.build_payload(request)
        data = await self.transport.post_json(
            self.base_url + "/responses",
            {"Authorization": "Bearer " + self.api_key},
            payload,
            self.timeout,
        )
        return self.parse_response(data, input_items, bool(payload.get("store")))
