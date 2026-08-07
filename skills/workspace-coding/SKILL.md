---
name: workspace-coding
description: Inspect and modify code in the configured workspace with explicit approval for changes.
version: "1"
required_tools: [fs_list, fs_read, fs_write, shell_exec]
board_profiles: []
---

Work only inside the configured workspace root. Inspect existing files and conventions before
editing. Use `fs_read` and `fs_list` for discovery. Use `fs_write` only for complete, bounded
file updates and use `shell_exec` for builds and tests. Both write and shell operations require
interactive approval unless the operator explicitly starts Aster with `--yes` or changes policy.
Report verification results and do not hide command failures.
