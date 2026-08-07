from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, List, Mapping, Optional


class SkillError(ValueError):
    pass


@dataclass(frozen=True)
class SkillMetadata:
    name: str
    description: str
    path: Path
    required_tools: List[str]
    board_profiles: List[str]
    version: str = "1"


@dataclass(frozen=True)
class LoadedSkill:
    metadata: SkillMetadata
    instructions: str


def _frontmatter(path: Path, include_body: bool) -> tuple:
    text = path.read_text(encoding="utf-8")
    if not text.startswith("---\n"):
        raise SkillError("skill requires YAML frontmatter: %s" % path)
    marker = text.find("\n---\n", 4)
    if marker < 0:
        raise SkillError("skill frontmatter is not closed: %s" % path)
    try:
        import yaml
    except ImportError as exc:
        raise SkillError("PyYAML is required to load skills") from exc
    data = yaml.safe_load(text[4:marker]) or {}
    if not isinstance(data, dict):
        raise SkillError("skill frontmatter must be an object: %s" % path)
    body = text[marker + 5 :].strip() if include_body else ""
    return data, body


class SkillCatalog:
    def __init__(self, skills: Mapping[str, SkillMetadata]) -> None:
        self._skills = dict(skills)

    @classmethod
    def discover(cls, roots: Iterable[str]) -> "SkillCatalog":
        discovered: Dict[str, SkillMetadata] = {}
        for root_value in roots:
            root = Path(root_value).expanduser().resolve()
            if not root.exists():
                continue
            for path in sorted(root.glob("*/SKILL.md")):
                data, _ = _frontmatter(path, include_body=False)
                name = str(data.get("name") or path.parent.name)
                if name in discovered:
                    raise SkillError("duplicate skill: %s" % name)
                discovered[name] = SkillMetadata(
                    name=name,
                    description=str(data.get("description") or ""),
                    path=path,
                    required_tools=[str(item) for item in data.get("required_tools", [])],
                    board_profiles=[str(item) for item in data.get("board_profiles", [])],
                    version=str(data.get("version") or "1"),
                )
        return cls(discovered)

    def names(self) -> List[str]:
        return sorted(self._skills)

    def metadata(self) -> List[SkillMetadata]:
        return [self._skills[name] for name in self.names()]

    def load(
        self,
        name: str,
        board_profile: str,
        available_tools: Iterable[str],
    ) -> LoadedSkill:
        if name not in self._skills:
            raise SkillError("unknown skill: %s" % name)
        metadata = self._skills[name]
        if metadata.board_profiles and board_profile not in metadata.board_profiles:
            raise SkillError(
                "skill %s does not support board profile %s" % (name, board_profile)
            )
        missing = set(metadata.required_tools) - set(available_tools)
        if missing:
            raise SkillError(
                "skill %s requires unavailable tools: %s"
                % (name, ", ".join(sorted(missing)))
            )
        _, body = _frontmatter(metadata.path, include_body=True)
        return LoadedSkill(metadata=metadata, instructions=body)

    def load_many(
        self,
        names: Iterable[str],
        board_profile: str,
        available_tools: Iterable[str],
    ) -> List[LoadedSkill]:
        return [
            self.load(name, board_profile, available_tools) for name in names
        ]
