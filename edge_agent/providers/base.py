from __future__ import annotations

import asyncio
import json
import urllib.error
import urllib.request
from abc import ABC, abstractmethod
from typing import Any, Dict, Mapping, Optional, Protocol

from edge_agent.types import ProviderRequest, ProviderResponse


class ProviderError(RuntimeError):
    pass


class JsonTransport(Protocol):
    async def post_json(
        self,
        url: str,
        headers: Mapping[str, str],
        payload: Mapping[str, Any],
        timeout: float,
    ) -> Mapping[str, Any]:
        ...


class UrllibJsonTransport:
    async def post_json(
        self,
        url: str,
        headers: Mapping[str, str],
        payload: Mapping[str, Any],
        timeout: float,
    ) -> Mapping[str, Any]:
        loop = asyncio.get_running_loop()
        return await loop.run_in_executor(
            None, self._post_json, url, headers, payload, timeout
        )

    @staticmethod
    def _post_json(
        url: str,
        headers: Mapping[str, str],
        payload: Mapping[str, Any],
        timeout: float,
    ) -> Mapping[str, Any]:
        body = json.dumps(payload).encode("utf-8")
        request = urllib.request.Request(
            url,
            data=body,
            headers={"Content-Type": "application/json", **dict(headers)},
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                decoded = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise ProviderError("provider HTTP %s: %s" % (exc.code, detail)) from exc
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as exc:
            raise ProviderError("provider request failed: %s" % exc) from exc
        if not isinstance(decoded, dict):
            raise ProviderError("provider returned a non-object JSON response")
        return decoded


class ModelProvider(ABC):
    name = "base"

    @abstractmethod
    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        raise NotImplementedError


class HttpProvider(ModelProvider):
    def __init__(
        self,
        base_url: str,
        api_key: str,
        timeout: float = 60.0,
        transport: Optional[JsonTransport] = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self.transport = transport or UrllibJsonTransport()
