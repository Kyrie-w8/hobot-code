from __future__ import annotations

import fnmatch
from dataclasses import dataclass
from typing import Any, Iterable, Mapping


class PolicyDenied(PermissionError):
    pass


class ApprovalRequired(PermissionError):
    pass


@dataclass(frozen=True)
class PolicyDecision:
    allowed: bool
    requires_approval: bool
    reason: str


class PolicyEngine:
    def __init__(
        self,
        allowed_tools: Iterable[str],
        denied_tools: Iterable[str] = (),
        approval_tools: Iterable[str] = (),
        approve_write_tools: bool = False,
    ) -> None:
        self.allowed_tools = tuple(allowed_tools)
        self.denied_tools = tuple(denied_tools)
        self.approval_tools = tuple(approval_tools)
        self.approve_write_tools = approve_write_tools

    @classmethod
    def from_config(cls, config: Mapping[str, Any]) -> "PolicyEngine":
        return cls(
            allowed_tools=config.get("allowed_tools", ["system.*"]),
            denied_tools=config.get("denied_tools", []),
            approval_tools=config.get("approval_tools", []),
            approve_write_tools=bool(config.get("approve_write_tools", False)),
        )

    @staticmethod
    def _matches(name: str, patterns: Iterable[str]) -> bool:
        return any(fnmatch.fnmatchcase(name, pattern) for pattern in patterns)

    def decide(self, name: str, risk: str) -> PolicyDecision:
        if self._matches(name, self.denied_tools):
            return PolicyDecision(False, False, "tool is explicitly denied")
        if not self._matches(name, self.allowed_tools):
            return PolicyDecision(False, False, "tool is not on the allowlist")
        approval = self._matches(name, self.approval_tools)
        if risk in ("write", "dangerous") and not self.approve_write_tools:
            approval = True
        return PolicyDecision(True, approval, "allowed by policy")

    def authorize(self, name: str, risk: str, approved: bool = False) -> None:
        decision = self.decide(name, risk)
        if not decision.allowed:
            raise PolicyDenied("%s: %s" % (name, decision.reason))
        if decision.requires_approval and not approved:
            raise ApprovalRequired("%s requires approval" % name)
