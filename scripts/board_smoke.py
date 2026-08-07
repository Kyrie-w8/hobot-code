#!/usr/bin/env python3
import json
import sys

from edge_agent import __version__
from edge_agent.tools import system_snapshot


def main():
    if sys.version_info < (3, 9):
        raise RuntimeError("Python 3.9 or newer is required")
    print(
        json.dumps(
            {"runtime_version": __version__, "snapshot": system_snapshot()},
            ensure_ascii=False,
        )
    )


if __name__ == "__main__":
    main()
