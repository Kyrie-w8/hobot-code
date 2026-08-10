# Changelog

## 0.13.0

- Expand the versioned RDK knowledge pack from 7 to 27 professional topics spanning X5, S100, S600, official resource routing, RDK Studio/XBurn, X5 SDK, S600 application cases, system lifecycle, AI toolchains, BPU runtime, Model Zoo, LLM/VLM/VLA, camera and multimedia, TROS, peripheral I/O, MCU/IPC/CAN, VDSP/GPU, drivers, storage, networking, bring-up, safety, and performance engineering.
- Cite official D-Robotics documentation or D-Robotics GitHub repositories inside every knowledge document, with explicit review dates and board/RDK OS applicability.
- Reject unlisted knowledge files, missing or non-official sources, uncited manifest links, stale review dates, and credential-like content during release validation; verify the complete manifest-driven knowledge set inside the ARM64 package.
- Show more source provenance in `/knowledge` results and document the knowledge coverage and governance model.

## 0.12.9

- Define the user-facing Agent identity as Hobot Code; underlying models and runtimes remain implementation details.
- Rename the `/system-prompt` composition label from `Pi base` to `Core agent` and validate release prompts against identity regressions.

## 0.12.8

- Treat Shell redirects to `/dev/null` as routine output suppression instead of protected-device writes.
- Continue requiring approval for real device nodes, protected system paths, and lookalike paths such as `/dev/null/child`.

## 0.12.7

- Repair interrupted tool-call history at request time so sessions stopped during approval or execution can resume without invalid gateway payloads.
- Reload the shared permission policy before every tool call and `/permissions` operation, keeping concurrent terminals consistent without restarts.
- Preserve mandatory confirmation for destructive Shell commands and writes outside the workspace even when root policy mode honors an explicit `allow` rule.

## 0.12.6

- Add explicit root permission modes: the safe `confirm` default and opt-in `policy` mode, which honors persistent `allow/ask/deny` rules for routine root operations.
- Keep destructive Shell commands, writes outside the workspace, and protected system paths guarded in every root mode.
- Show the effective root mode in `/permissions status` and explain the active mode at startup and in diagnostics.

## 0.12.5

- Replace Pi's bundled release history with the Hobot Code changelog inside release packages, preventing unrelated upstream entries and dates from flooding the startup screen.
- Collapse update notices by default on new installations while preserving `/changelog` for intentional review.
- Validate that the user-facing and runtime changelogs are identical so release packaging cannot silently reintroduce upstream history.

## 0.12.4

- Fork `/btw` from the parent's latest fully settled turn so an in-flight main task cannot become the side task.
- Use `agent_settled` as the multi-turn RPC barrier and correlate prompt and abort failures by request ID.
- Queue concurrent side-agent dialogs, support enhanced-terminal Y/N keys, focus the pane explicitly, and bound approvals to two minutes.
- Bound child shutdown, isolate render failures, and release side-agent capacity only after cleanup finishes.

## 0.12.3

- Restore RDK expert-prompt rendering after the Provider split by using the shared well-formed Unicode sanitizer.
- Retry a bounded buffered request when the D-Robotics gateway ends an empty SSE response early, while continuing to reject partial streams after content has started.

## 0.12.2

- Split the D-Robotics gateway into a focused provider module and preserve valid Unicode while rejecting malformed text.
- Bound total gateway, Hook, LSP, and workspace-fingerprint memory use for predictable operation on embedded boards.
- Bound LSP diagnostic retention and shutdown latency, reject relative runtime path overrides, and keep `/btw` within narrow terminals.
- Make memory deduplication and persistent-goal transitions transactional, keep side-agent memory reads non-mutating, and ignore untrusted project quality gates.
- Harden release provenance, package closure validation, environment parsing, installation, and complete command rollback.
- Reorganize the project documentation around installation and daily workflows, with contributor guidance, security reporting, and CI.

## 0.12.1

- Upgrade `/btw` from a one-shot response to a persistent, private multi-turn RPC Agent with an in-overlay input line.
- Present `/btw` as a full-height right-side 50% pane and cap same-user side-agent concurrency (default 2, configurable up to 8).
- Keep the side conversation, thinking stream, tool activity, and usage visible across follow-up turns until the user closes it.
- Forward confirmation, selection, and input requests from the side process into the overlay while preserving close-time deletion and parent-session isolation.
- Stream D-Robotics Anthropic responses as SSE with bounded buffered fallback, explicit stop-reason handling, visible thinking, and payload limits.
- Harden tool execution with realpath-aware workspace boundaries, destructive-command detection, credential-free Hook/LSP environments, project-hook trust, and mandatory root confirmation for `bash`, `write`, and `edit`.
- Make runtime and command swaps transactional with locking, process preflight checks, staged self-tests, command backups, and failure restoration.
- Improve resource lifecycle cleanup, relevant-only memory recall, Chinese knowledge search, accurate quality-gate mutation tracking, readable doctor/knowledge output, and version provenance checks.

## 0.12.0

- Add `/btw <task>` as an ephemeral independent coding agent that can run while the parent Agent continues.
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
