from __future__ import annotations

from typing import Any, Mapping

from edge_agent.providers.anthropic import AnthropicProvider
from edge_agent.providers.base import ModelProvider
from edge_agent.providers.gemini import GeminiProvider
from edge_agent.providers.mock import EchoProvider, ScriptedProvider
from edge_agent.providers.openai_compat import OpenAICompatibleProvider
from edge_agent.providers.openai_responses import OpenAIResponsesProvider


def create_provider(config: Mapping[str, Any]) -> ModelProvider:
    provider = str(config.get("provider") or "").lower()
    api_key = str(config.get("api_key") or "")
    base_url = str(config.get("base_url") or "")
    timeout = float(config.get("timeout_seconds", 60))
    if provider == "mock":
        return EchoProvider()
    if provider == "openai_responses":
        return OpenAIResponsesProvider(
            api_key=api_key,
            base_url=base_url or "https://api.openai.com/v1",
            timeout=timeout,
        )
    if provider == "openai_compatible":
        if not base_url:
            raise ValueError("openai_compatible requires model.base_url")
        return OpenAICompatibleProvider(
            api_key=api_key, base_url=base_url, timeout=timeout
        )
    if provider == "anthropic":
        return AnthropicProvider(
            api_key=api_key,
            base_url=base_url or "https://api.anthropic.com",
            timeout=timeout,
            api_version=str(config.get("api_version") or "2023-06-01"),
        )
    if provider == "gemini":
        return GeminiProvider(
            api_key=api_key,
            base_url=base_url
            or "https://generativelanguage.googleapis.com/v1beta",
            timeout=timeout,
            api_revision=str(config.get("api_revision") or ""),
        )
    raise ValueError("unsupported model provider: %s" % provider)


__all__ = [
    "AnthropicProvider",
    "EchoProvider",
    "GeminiProvider",
    "ModelProvider",
    "OpenAICompatibleProvider",
    "OpenAIResponsesProvider",
    "ScriptedProvider",
    "create_provider",
]
