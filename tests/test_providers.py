import asyncio
from typing import Any, Mapping

from edge_agent.providers.anthropic import AnthropicProvider
from edge_agent.providers.gemini import GeminiProvider
from edge_agent.providers.openai_compat import OpenAICompatibleProvider
from edge_agent.providers.openai_responses import OpenAIResponsesProvider
from edge_agent.types import Message, ProviderRequest, ToolCall


class FakeTransport:
    def __init__(self, response: Mapping[str, Any]):
        self.response = response
        self.calls = []

    async def post_json(self, url, headers, payload, timeout):
        self.calls.append((url, dict(headers), dict(payload), timeout))
        return self.response


def request(**kwargs):
    value = {
        "model": "test-model",
        "system_prompt": "system",
        "messages": [Message("user", "hello")],
        "tools": [
            {
                "name": "system.snapshot",
                "description": "status",
                "parameters": {
                    "type": "object",
                    "properties": {},
                    "additionalProperties": False,
                },
            }
        ],
        "settings": {},
        "state": {},
    }
    value.update(kwargs)
    return ProviderRequest(**value)


def test_openai_responses_payload_and_parse():
    transport = FakeTransport(
        {
            "id": "resp_1",
            "status": "completed",
            "output": [
                {
                    "type": "function_call",
                    "call_id": "call_1",
                    "name": "system.snapshot",
                    "arguments": "{}",
                }
            ],
            "usage": {"input_tokens": 10, "output_tokens": 2},
        }
    )
    provider = OpenAIResponsesProvider("key", transport=transport)
    response = asyncio.run(provider.complete(request()))
    assert response.tool_calls[0].name == "system.snapshot"
    payload = transport.calls[0][2]
    assert payload["tools"][0]["name"] == "system.snapshot"
    assert "function" not in payload["tools"][0]
    assert payload["store"] is False


def test_openai_responses_continuation_store_true():
    state = {
        "previous_response_id": "resp_1",
        "response_output": [
            {
                "type": "function_call",
                "call_id": "current",
                "name": "system.snapshot",
                "arguments": "{}",
            }
        ],
        "input_items": [{"role": "user", "content": "hello"}],
    }
    messages = [
        Message("tool", "old", "old.tool", "old"),
        Message("tool", "current-result", "system.snapshot", "current"),
    ]
    payload, _ = OpenAIResponsesProvider.build_payload(
        request(messages=messages, settings={"store": True}, state=state)
    )
    assert payload["previous_response_id"] == "resp_1"
    assert payload["input"] == [
        {
            "type": "function_call_output",
            "call_id": "current",
            "output": "current-result",
        }
    ]


def test_openai_compatible_tool_mapping():
    transport = FakeTransport(
        {
            "choices": [
                {
                    "finish_reason": "tool_calls",
                    "message": {
                        "content": None,
                        "tool_calls": [
                            {
                                "id": "c1",
                                "function": {
                                    "name": "system.snapshot",
                                    "arguments": "{}",
                                },
                            }
                        ],
                    },
                }
            ]
        }
    )
    provider = OpenAICompatibleProvider("", "http://local/v1", transport=transport)
    response = asyncio.run(provider.complete(request()))
    assert response.tool_calls[0].id == "c1"
    assert transport.calls[0][0] == "http://local/v1/chat/completions"
    assert transport.calls[0][2]["messages"][0]["role"] == "system"


def test_anthropic_groups_tool_results():
    messages = [
        Message(
            "assistant",
            tool_calls=[
                ToolCall("a", "one", {}),
                ToolCall("b", "two", {}),
            ],
        ),
        Message("tool", "1", "one", "a"),
        Message("tool", "2", "two", "b"),
    ]
    payload = AnthropicProvider.build_payload(request(messages=messages))
    assert len(payload["messages"]) == 2
    assert len(payload["messages"][1]["content"]) == 2
    response = AnthropicProvider.parse_response(
        {
            "content": [
                {"type": "text", "text": "checking"},
                {"type": "tool_use", "id": "x", "name": "one", "input": {}},
            ],
            "stop_reason": "tool_use",
        }
    )
    assert response.content == "checking"
    assert response.tool_calls[0].name == "one"


def test_gemini_function_mapping():
    payload, _ = GeminiProvider.build_payload(request())
    declaration = payload["tools"][0]
    assert declaration["name"] == "system.snapshot"
    response = GeminiProvider.parse_response(
        {
            "id": "int_1",
            "status": "requires_action",
            "steps": [
                {
                    "type": "model_output",
                    "content": [{"type": "text", "text": "ok"}],
                },
                {
                    "type": "function_call",
                    "id": "call_1",
                    "name": "system.snapshot",
                    "arguments": {},
                },
            ]
        }
    )
    assert response.content == "ok"
    assert response.tool_calls[0].name == "system.snapshot"
    assert response.tool_calls[0].id == "call_1"
