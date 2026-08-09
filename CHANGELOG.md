# Changelog

## 0.12.1

- Upgrade `/btw` from a one-shot response to a persistent, private multi-turn RPC Agent with an in-overlay input line.
- Present `/btw` as a full-height right-side 50% pane and cap board-wide side-agent concurrency (default 2, configurable up to 8).
- Keep the side conversation, thinking stream, tool activity, and usage visible across follow-up turns until the user closes it.
- Forward confirmation, selection, and input requests from the side process into the overlay while preserving close-time deletion and parent-session isolation.

## 0.12.0

- Add `/btw <task>` as an ephemeral full-capability side agent that can run while the parent Agent continues.
- Snapshot the parent session's in-memory branch, effective system prompt, model, thinking level, active tools, trust state, and scoped memory context without writing side messages back.
- Add a live, scrollable overlay with tool activity, streamed output, usage, cancellation, bounded event handling, and guaranteed temporary-session cleanup.
- Preserve workspace and device side effects while preventing side sessions from consuming persistent-goal budgets or writing parent memory and goal state.

## 0.11.1

- Move configuration and mutable state to isolated per-user XDG directories, with guarded migration of the legacy system layout.
- Replace the bilingual, repetitive RDK expert prompt with a compact English overlay that preserves only board-specific evidence, routing, deployment, and hardware-safety rules.
- Omit empty quality-gate, memory, and persistent-goal sections from normal turns; inject concise state only when it exists.
- Enforce a 1700-character budget and single-language contract for the maintained RDK prompt while keeping detailed platform knowledge available through tools and Skills.

## 0.11.0

- Add user-created persistent project goals with turn/token budgets, elapsed work, progress checkpoints, continuation counts, restart recovery, and verification fingerprints.
- Add structured PreToolUse and PostToolUse hooks with direct argv execution, bounded time/output, explicit block-or-warn policies, redacted audit records, and opt-in project hooks.
- Add configurable SSH terminal notifications using OSC 9, OSC 777, and bell for approval waits, long-turn completion/failure, and exhausted goal budgets.
- Add an on-demand LSP client for hover, definitions, references, symbols, and diagnostics with workspace confinement plus process, RSS, request, and idle limits.

## 0.10.0

- Add local SQLite/FTS5 persistent memory with user, project, board, and session scopes, deduplication, optional expiry, and bounded recall.
- Add `memory_search`, approval-gated `memory_save`, and `/memory` commands for status, list, search, direct add, deletion, bulk clear, pruning, and reload.
- Reject secret-like memory at the storage boundary, keep the database root-only, and audit mutations and searches without duplicating stored content.
- Inject only relevant memories as explicitly untrusted, potentially stale context while keeping current user instructions and live board evidence authoritative.

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
