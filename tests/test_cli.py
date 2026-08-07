import json
from pathlib import Path

from edge_agent.cli import main


def write_config(tmp_path: Path) -> Path:
    prompt = tmp_path / "prompt.md"
    prompt.write_text("Agent {{agent_name}} on {{board_profile}}", encoding="utf-8")
    skill_root = tmp_path / "skills"
    skill = skill_root / "status"
    skill.mkdir(parents=True)
    skill.joinpath("SKILL.md").write_text(
        "---\nname: status\ndescription: status\nrequired_tools: []\n---\nUse status.",
        encoding="utf-8",
    )
    config = tmp_path / "config.yaml"
    config.write_text(
        "runtime:\n  max_steps: 2\n  tool_timeout_seconds: 5\n"
        "board:\n  profile: test\n  model: test\n"
        "agent:\n  name: Test\n  skills: []\n"
        "model:\n  provider: mock\n  name: mock\n"
        "memory:\n  path: memory.db\n"
        "prompts:\n  layers:\n    - name: base\n      priority: 1\n"
        "      path: prompt.md\n"
        "skills:\n  roots:\n    - skills\n"
        "security:\n  allowed_tools:\n    - system.*\n",
        encoding="utf-8",
    )
    return config


def test_cli_offline_run_and_export(tmp_path: Path, capsys):
    config = write_config(tmp_path)
    assert main(["run", "--config", str(config), "--message", "hello", "--json"]) == 0
    output = json.loads(capsys.readouterr().out)
    assert output["content"] == "[offline mock] hello"
    assert main(
        ["export", "--config", str(config), "--session", output["session_id"]]
    ) == 0
    trajectory = json.loads(capsys.readouterr().out)
    assert [item["role"] for item in trajectory["messages"]] == ["user", "assistant"]
