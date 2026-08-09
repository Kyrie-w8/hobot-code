---
name: workspace-coding
description: Inspect and modify code in the configured workspace with explicit approval for changes.
version: "1"
required_tools: [read, bash, edit, write]
board_profiles: []
---

Inspect existing files and conventions before editing. Use `read` for discovery, `edit` for precise
changes, `write` only for new files or complete bounded rewrites, and `bash` for builds and tests.
Working-directory writes follow the current Pi session policy; writes outside it and destructive
commands may require interactive approval. Report verification results and do not hide failures.
