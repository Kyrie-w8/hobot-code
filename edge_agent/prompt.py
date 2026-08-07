from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterable, List, Mapping


_VARIABLE = re.compile(r"\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}")


class PromptError(ValueError):
    pass


@dataclass(frozen=True)
class PromptLayer:
    name: str
    priority: int
    content: str


class PromptComposer:
    def __init__(self, layers: Iterable[PromptLayer] = ()) -> None:
        self.layers = list(layers)

    def add(self, name: str, priority: int, content: str) -> None:
        if content.strip():
            self.layers.append(PromptLayer(name, priority, content.strip()))

    def render(self, variables: Mapping[str, object]) -> str:
        sections: List[str] = []
        for layer in sorted(self.layers, key=lambda item: item.priority):
            def replace(match: re.Match) -> str:
                name = match.group(1)
                if name not in variables:
                    raise PromptError("missing prompt variable: %s" % name)
                return str(variables[name])

            content = _VARIABLE.sub(replace, layer.content)
            sections.append("<%s>\n%s\n</%s>" % (layer.name, content, layer.name))
        return "\n\n".join(sections)


def load_prompt(path: str) -> str:
    target = Path(path).expanduser().resolve()
    try:
        return target.read_text(encoding="utf-8")
    except OSError as exc:
        raise PromptError("unable to read prompt %s: %s" % (target, exc)) from exc
