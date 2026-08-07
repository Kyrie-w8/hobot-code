from __future__ import annotations

from typing import Iterable, List, Optional

from edge_agent.providers.base import ModelProvider
from edge_agent.types import ProviderRequest, ProviderResponse


class EchoProvider(ModelProvider):
    """Offline provider used for installation checks and local demos."""

    name = "mock"

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        last_user = next(
            (message.content for message in reversed(request.messages) if message.role == "user"),
            "",
        )
        return ProviderResponse(content="[offline mock] %s" % last_user)


class ScriptedProvider(ModelProvider):
    def __init__(self, responses: Iterable[ProviderResponse]) -> None:
        self.responses: List[ProviderResponse] = list(responses)
        self.requests: List[ProviderRequest] = []

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        self.requests.append(request)
        if not self.responses:
            raise RuntimeError("scripted provider has no response left")
        return self.responses.pop(0)
