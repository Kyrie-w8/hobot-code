# Changelog

## 0.9.0

- Add ordered allow/ask/deny permissions for built-in, RDK, plugin, and MCP tools; denied tools are removed from the active model context.
- Add redacted, target-specific approval dialogs and fail-closed behavior for non-interactive sessions.
- Add `/init` to create a board-aware `AGENTS.md` and `.hobot/quality-gates.json` without overwriting existing project files.
- Add persistent session quality gates with configurable commands and timeouts, bounded output, workspace fingerprints, stale detection, and completion enforcement.
- Add `/permissions`, `/gate`, and a model-callable `quality_gate` tool with unit coverage for policy, initialization, redaction, and fingerprints.

## 0.8.0

- Remove the obsolete predecessor implementation, service mode, packaging chain, command aliases, paths, and environment namespaces.
- Standardize the product on the `hobot` command and `/etc/hobot-code`, `/var/lib/hobot-code`, and `/usr/local/lib/hobot-code` paths.
- Add a strict repository validator that rejects removed brand identifiers and filenames.
- Document prioritized design lessons from Prime Agent and Crush for long-running work, permissions, hooks, LSP, Skills, and remote notifications.

## 0.7.0

- Add a complete D-Robotics RDK expert role covering evidence, platform routing, model deployment, multimedia, TROS, hardware interfaces, performance, safety, and delivery standards.
- Render the expert role with the live board model, RDK OS version, documentation track, hostname, and architecture on every agent turn.
- Add `/system-prompt` for inspecting the effective Pi and Hobot Code expert prompt.
- Package and validate the standalone expert prompt while retaining a conservative missing-file fallback.
- Update bundled Skills to use Pi's current `read`, `bash`, `edit`, and `write` tool contract.

## 0.6.0

- Detect X5, S100, and S600 board IDs plus the complete `/etc/version` RDK OS string.
- Add a versioned local RDK knowledge pack with official D-Robotics source indexes.
- Add `rdk_docs_search` and `/knowledge` with board, RDK OS, topic, and version-match routing.
- Keep knowledge out of the base context while prompting the agent to distinguish documentation from live evidence.
- Validate and package the knowledge manifest and documents in the ARM64 installer.
