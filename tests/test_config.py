from pathlib import Path

import pytest

from edge_agent.config import ConfigError, deep_merge, expand_environment, load_config


def test_deep_merge_preserves_nested_values():
    assert deep_merge({"a": {"x": 1, "y": 2}}, {"a": {"y": 3}}) == {
        "a": {"x": 1, "y": 3}
    }


def test_environment_expansion_and_missing_value():
    assert expand_environment(
        {"key": "${TOKEN}", "fallback": "${MISSING:-local}"}, {"TOKEN": "secret"}
    ) == {"key": "secret", "fallback": "local"}
    with pytest.raises(ConfigError, match="MISSING"):
        expand_environment("${MISSING}", {})


def test_config_extends_and_board_overlay(tmp_path: Path):
    base = tmp_path / "base.yaml"
    child = tmp_path / "child.yaml"
    board = tmp_path / "board.yaml"
    base.write_text(
        "runtime:\n  max_steps: 4\n  tool_timeout_seconds: 5\n"
        "model:\n  provider: mock\n  name: base\nsecurity: {}\n",
        encoding="utf-8",
    )
    child.write_text("extends: base.yaml\nmodel:\n  name: child\n", encoding="utf-8")
    board.write_text("board:\n  profile: x5\n", encoding="utf-8")
    loaded = load_config(str(child), str(board))
    assert loaded.data["runtime"]["max_steps"] == 4
    assert loaded.data["model"]["name"] == "child"
    assert loaded.board_profile == "x5"


def test_repo_config_loads():
    loaded = load_config("config/base.yaml", "config/boards/s600.yaml")
    assert loaded.board_profile == "s600"
    assert loaded.data["board"]["bpu_cores"] == 4
