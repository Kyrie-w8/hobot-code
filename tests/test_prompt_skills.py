import pytest

from edge_agent.prompt import PromptComposer, PromptError
from edge_agent.skills import SkillCatalog, SkillError


def test_prompt_layers_are_ordered_and_rendered():
    prompt = PromptComposer()
    prompt.add("role", 30, "Agent {{agent_name}}")
    prompt.add("policy", 10, "Board {{board_profile}}")
    value = prompt.render({"agent_name": "A", "board_profile": "x5"})
    assert value.index("<policy>") < value.index("<role>")
    assert "Agent A" in value


def test_prompt_missing_variable_fails():
    prompt = PromptComposer()
    prompt.add("x", 1, "{{missing}}")
    with pytest.raises(PromptError):
        prompt.render({})


def test_skill_is_lazy_loaded_and_checks_tools():
    catalog = SkillCatalog.discover(["skills"])
    metadata = catalog.metadata()[0]
    assert metadata.name == "system-info"
    skill = catalog.load("system-info", "x5", ["system.snapshot"])
    assert "system.snapshot" in skill.instructions
    with pytest.raises(SkillError, match="requires unavailable"):
        catalog.load("system-info", "x5", [])
