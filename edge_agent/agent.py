from __future__ import annotations

from typing import Any, Callable, Dict, Iterable, List, Mapping, Optional

from edge_agent.memory import SessionStore
from edge_agent.policy import PolicyEngine
from edge_agent.prompt import PromptComposer
from edge_agent.providers.base import ModelProvider
from edge_agent.skills import SkillCatalog
from edge_agent.tools import ApprovalHandler, ToolRegistry
from edge_agent.types import AgentResult, Message, ProviderRequest


class AgentLimitError(RuntimeError):
    pass


def _merge_usage(total: Dict[str, Any], update: Mapping[str, Any]) -> None:
    for key, value in update.items():
        if isinstance(value, (int, float)) and not isinstance(value, bool):
            total[key] = total.get(key, 0) + value


class AgentRuntime:
    def __init__(
        self,
        config: Mapping[str, Any],
        provider: ModelProvider,
        tools: ToolRegistry,
        policy: PolicyEngine,
        memory: SessionStore,
        prompt: PromptComposer,
        skills: Optional[SkillCatalog] = None,
        approval_handler: Optional[ApprovalHandler] = None,
    ) -> None:
        self.config = config
        self.provider = provider
        self.tools = tools
        self.policy = policy
        self.memory = memory
        self.prompt = prompt
        self.skills = skills or SkillCatalog({})
        self.approval_handler = approval_handler

    def _tool_schemas(self, board_profile: str) -> List[Dict[str, Any]]:
        return [
            spec.model_schema()
            for spec in self.tools.specs()
            if (not spec.board_profiles or board_profile in spec.board_profiles)
            and self.policy.decide(spec.name, spec.risk).allowed
        ]

    async def run(
        self,
        user_input: str,
        session_id: Optional[str] = None,
        skill_names: Optional[Iterable[str]] = None,
    ) -> AgentResult:
        board = self.config.get("board", {})
        profile = str(board.get("profile", "generic"))
        session = self.memory.ensure_session(
            session_id,
            metadata={"board_profile": profile, "model": self.config["model"]["name"]},
        )
        history = self.memory.load_messages(session)
        user_message = Message(role="user", content=user_input)
        history.append(user_message)
        self.memory.append_message(session, user_message)

        selected_names = list(skill_names or self.config.get("agent", {}).get("skills", []))
        loaded_skills = self.skills.load_many(
            selected_names, profile, self.tools.names()
        )
        composer = PromptComposer(self.prompt.layers)
        for skill in loaded_skills:
            composer.add("skill_%s" % skill.metadata.name, 50, skill.instructions)
        system_prompt = composer.render(
            {
                "board_profile": profile,
                "board_model": board.get("model", "unknown"),
                "agent_name": self.config.get("agent", {}).get("name", "EdgeAgent"),
            }
        )

        model = self.config["model"]
        runtime = self.config["runtime"]
        model_settings = dict(model.get("settings", {}))
        max_steps = int(runtime.get("max_steps", 8))
        tool_schemas = self._tool_schemas(profile)
        provider_state: Dict[str, Any] = {}
        usage: Dict[str, Any] = {}

        for step in range(1, max_steps + 1):
            response = await self.provider.complete(
                ProviderRequest(
                    model=str(model["name"]),
                    system_prompt=system_prompt,
                    messages=list(history),
                    tools=tool_schemas,
                    settings=model_settings,
                    state=provider_state,
                )
            )
            provider_state = dict(response.state)
            _merge_usage(usage, response.usage)
            assistant = response.as_message()
            history.append(assistant)
            self.memory.append_message(session, assistant)
            self.memory.append_event(
                session,
                "model_response",
                {
                    "step": step,
                    "finish_reason": response.finish_reason,
                    "tool_calls": [call.to_dict() for call in response.tool_calls],
                    "usage": response.usage,
                },
            )
            if not response.tool_calls:
                return AgentResult(
                    session_id=session,
                    content=response.content,
                    messages=history,
                    steps=step,
                    usage=usage,
                )
            for call in response.tool_calls:
                execution = await self.tools.execute(
                    call,
                    self.policy,
                    board_profile=profile,
                    approval_handler=self.approval_handler,
                )
                tool_message = Message(
                    role="tool",
                    content=execution.to_json(),
                    name=call.name,
                    tool_call_id=call.id,
                )
                history.append(tool_message)
                self.memory.append_message(session, tool_message)
                self.memory.append_event(session, "tool_execution", execution.to_dict())
        raise AgentLimitError("agent reached max_steps=%s" % max_steps)
