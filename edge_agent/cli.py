from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path
from typing import Any, Dict, Iterable, Optional, Tuple

from edge_agent.agent import AgentRuntime
from edge_agent.config import LoadedConfig, load_config
from edge_agent.diagnostics import diagnose
from edge_agent.memory import SessionStore
from edge_agent.policy import PolicyEngine
from edge_agent.prompt import PromptComposer, load_prompt
from edge_agent.providers import create_provider
from edge_agent.skills import SkillCatalog
from edge_agent.tools import ToolRegistry, create_builtin_registry


def _resolve(value: str, config: LoadedConfig) -> str:
    path = Path(value).expanduser()
    if not path.is_absolute():
        path = config.source_path.parent / path
    return str(path.resolve())


def _load_skills(config: LoadedConfig) -> SkillCatalog:
    roots = [
        _resolve(str(path), config)
        for path in config.data.get("skills", {}).get("roots", [])
    ]
    return SkillCatalog.discover(roots)


def _load_prompt(config: LoadedConfig) -> PromptComposer:
    composer = PromptComposer()
    for layer in config.data.get("prompts", {}).get("layers", []):
        if "path" in layer:
            content = load_prompt(_resolve(str(layer["path"]), config))
        else:
            content = str(layer.get("content", ""))
        composer.add(
            str(layer.get("name", "prompt")), int(layer.get("priority", 50)), content
        )
    board_prompt = config.data.get("board", {}).get("prompt", "")
    if board_prompt:
        composer.add("board", 20, str(board_prompt))
    role_prompt = config.data.get("agent", {}).get("prompt", "")
    if role_prompt:
        composer.add("agent_role", 30, str(role_prompt))
    return composer


def build_runtime(
    config: LoadedConfig,
) -> Tuple[AgentRuntime, SessionStore, ToolRegistry, SkillCatalog]:
    data = dict(config.data)
    memory_cfg = dict(data.get("memory", {}))
    memory_cfg["path"] = _resolve(memory_cfg.get("path", "../var/sessions.db"), config)
    data["memory"] = memory_cfg
    tools = create_builtin_registry(data)
    skills = _load_skills(config)
    policy = PolicyEngine.from_config(data.get("security", {}))
    store = SessionStore(memory_cfg["path"])
    runtime = AgentRuntime(
        config=data,
        provider=create_provider(data["model"]),
        tools=tools,
        policy=policy,
        memory=store,
        prompt=_load_prompt(config),
        skills=skills,
    )
    return runtime, store, tools, skills


def _common(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--config", default="config/base.yaml")
    parser.add_argument("--board", help="optional board-profile overlay")


async def _run_once(args: argparse.Namespace) -> int:
    loaded = load_config(args.config, args.board)
    runtime, store, _, _ = build_runtime(loaded)
    try:
        result = await runtime.run(
            args.message, session_id=args.session, skill_names=args.skill
        )
        if args.json:
            print(
                json.dumps(
                    {
                        "session_id": result.session_id,
                        "content": result.content,
                        "steps": result.steps,
                        "usage": result.usage,
                    },
                    ensure_ascii=False,
                )
            )
        else:
            print(result.content)
            print("session: %s" % result.session_id, file=sys.stderr)
        return 0
    finally:
        store.close()


def _diagnose(args: argparse.Namespace) -> int:
    loaded = load_config(args.config, args.board)
    runtime, store, tools, skills = build_runtime(loaded)
    try:
        report = diagnose(runtime.config, tools, skills)
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 0 if report["ok"] else 1
    finally:
        store.close()


def _list_tools(args: argparse.Namespace) -> int:
    loaded = load_config(args.config, args.board)
    _, store, tools, _ = build_runtime(loaded)
    try:
        for spec in tools.specs():
            print("%s\t%s\t%s" % (spec.name, spec.risk, spec.description))
        return 0
    finally:
        store.close()


def _list_skills(args: argparse.Namespace) -> int:
    loaded = load_config(args.config, args.board)
    _, store, _, skills = build_runtime(loaded)
    try:
        for item in skills.metadata():
            print("%s\tv%s\t%s" % (item.name, item.version, item.description))
        return 0
    finally:
        store.close()


def _export(args: argparse.Namespace) -> int:
    loaded = load_config(args.config, args.board)
    runtime, store, _, _ = build_runtime(loaded)
    try:
        tools = runtime._tool_schemas(runtime.config.get("board", {}).get("profile", "generic"))
        value = store.export_trajectory(
            args.session,
            tools=tools,
            metadata={
                "model": runtime.config["model"]["name"],
                "provider": runtime.config["model"]["provider"],
            },
        )
        print(json.dumps(value, ensure_ascii=False))
        return 0
    finally:
        store.close()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="edge-agent")
    subparsers = parser.add_subparsers(dest="command", required=True)

    run = subparsers.add_parser("run", help="run one agent turn")
    _common(run)
    run.add_argument("--message", required=True)
    run.add_argument("--session")
    run.add_argument("--skill", action="append")
    run.add_argument("--json", action="store_true")
    run.set_defaults(handler=lambda args: asyncio.run(_run_once(args)))

    check = subparsers.add_parser("diagnose", help="inspect runtime compatibility")
    _common(check)
    check.set_defaults(handler=_diagnose)

    tools = subparsers.add_parser("tools", help="list registered tools")
    _common(tools)
    tools.set_defaults(handler=_list_tools)

    skills = subparsers.add_parser("skills", help="list discovered skills")
    _common(skills)
    skills.set_defaults(handler=_list_skills)

    export = subparsers.add_parser("export", help="export one SFT-compatible trajectory")
    _common(export)
    export.add_argument("--session", required=True)
    export.set_defaults(handler=_export)
    return parser


def main(argv: Optional[Iterable[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(list(argv) if argv is not None else None)
    try:
        return int(args.handler(args))
    except Exception as exc:
        print("edge-agent: %s: %s" % (type(exc).__name__, exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
