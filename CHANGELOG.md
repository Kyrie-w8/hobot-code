# Changelog

## 0.20.0

- Make the Studio composer send and stop actions share one stable button position, and restrict the desktop model picker to D-Robotics models.
- Let every project create multiple conversations, derive readable Unicode titles from the first instruction, and support inline conversation renaming.
- Add per-task Review, Ask, and Developer approval modes whose private policies remain enforced on the RDK board with high-risk operations guarded.
- Document the current attachment boundary: image/document transport remains disabled until secure SSH staging, validation, and session replay are implemented end to end.

## 0.19.1

- Make Studio reply links open safely in the Mac default browser instead of disappearing inside the embedded WebView.
- Replace the hidden branch icon with an explicit Side Agent action that explains when a settled context is required.
- Allow idle and stopped Studio tasks to select a model, persisting terminal-task choices for the next resume while keeping in-flight turns immutable.
- Add D-Robotics Qwen 3.8 Max and GLM 5.2 alongside Kimi K3 in the built-in provider and default model scope.
- Suspend the oldest idle worker when the board-side concurrency pool is full, preserving its session while never interrupting active work or approvals.
- Rebuild the Studio sidebar as a collapsible project/conversation hierarchy, flatten Side Agents into sibling branches, and add confirmed conversation/project removal that never deletes workspace files.

## 0.19.0

- Rework Hobot Studio around project-grouped navigation, nested conversation branches, softer macOS-native visual hierarchy, and unlabeled user/Agent messages.
- Add board-side model discovery and idle-session model switching, exposed through a compact model selector in the composer.
- Show an optimistic persisted user turn plus staged, elapsed Agent progress immediately after submit, before the model emits its first token.
- Replace manually typed working directories with board-side folder browsing, safe folder creation, and an explicit no-project-folder workspace.
- Add persistent multi-turn side tasks that inherit the latest settled session context and continue independently under the existing per-user task limit.
- Make historical message editing a true session-tree fork from the selected user turn instead of appending a duplicate prompt to the current conversation.
- Add bounded protocol and regression coverage for model tables, workspace browsing, safe session leaves, and historical session snapshots.

## 0.18.0

- Rebuild Hobot Studio around a conversation-first two-column workspace with a wider reading surface, optional task details, clearer task state, responsive composition, and restrained Codex-style visual hierarchy.
- Persist every user prompt as a private schema-3 normalized task event so desktop conversations retain user turns across refresh, reconnect, resume, and restart.
- Group fragmented thinking, tool execution, notices, and assistant text into coherent Agent turns; keep thinking and tool details collapsible while rendering answers as safe GitHub-flavored Markdown.
- Add user-message copy and edit-and-send-again actions, auto-growing drafts that remain editable while the Agent works, explicit stop/send states, bottom-follow behavior, and a new-output jump control.
- Replace protocol-shaped error text and lifecycle noise with user-facing task states while retaining raw bounded events on the board for diagnostics.

## 0.17.1

- Make Enter send the desktop composer while Shift+Enter inserts a newline and IME composition remains uninterrupted.
- Distinguish resumable stopped tasks from tasks without a saved session; the latter now restart explicitly with a fresh Hobot Code session instead of failing with `task_resume_failed`.
- Add the `task.restart` board protocol and CLI operation while preserving the task ID, workspace, approval policy, event history, and separate restart accounting.
- Reload and resubscribe to task events after resume or restart, and derive the board's active-task count from the live task list instead of the initial connection snapshot.

## 0.17.0

- Add the Hobot Code macOS application for connecting to RDK boards, managing persistent Agent tasks, following normalized event streams, and handling approvals without moving credentials or permission decisions off the board.
- Add a reusable Go SSH Bridge SDK with typed task APIs, a reused control connection, dedicated event subscriptions, strict connection validation, bounded protocol decoding, and real S100 integration coverage.
- Add saved board profiles containing connection metadata only, secure local storage, automatic event-stream reconnection, task start/send/stop/resume controls, and a responsive task timeline with visible thinking and tool activity.
- Add a branded deterministic app icon, signed ARM64 application packaging, DMG generation, version and bundle metadata validation, and separate macOS CI release artifacts.

## 0.16.0

- Add capability negotiation and schema-2 normalized Hobot events while preserving protocol-1 envelopes and raw Pi RPC events for compatibility.
- Persist bounded approval requests, expose pending-approval recovery, and record normalized approval lifecycle events without moving permission decisions off the board.
- Bind background tasks to Pi session files and add explicit, side-effect-safe task resume after daemon or board interruption without replaying prompts, approvals, or tool calls.
- Add task rename, archive, unarchive, guarded deletion, task/event pagination, and configurable retained-task limits.
- Add `hobot bridge --stdio` for authenticated SSH transport to future desktop clients without opening a TCP listener or exporting model credentials.
- Fix a worker shutdown race that could misclassify an explicitly stopped task as failed when its RPC pipe closed.

## 0.15.0

- Add the per-user Go `agentd` control plane for background, multi-turn Pi RPC tasks that survive CLI and SSH client disconnects.
- Add versioned local JSONL task RPC with private Unix sockets, Linux peer-UID verification, persisted event sequences, bounded logs, reconnect replay, and fail-closed recovery.
- Add `hobot daemon` and `hobot task` lifecycle commands, including attach, follow, prompt, abort, approval response, stop, concurrency limits, and explicit interrupted-task semantics.
- Cross-compile and validate the static Linux ARM64 daemon in release packages, and include it in transactional install, rollback, uninstall, CI, documentation, and manifest checks.

## 0.14.3

- Use `curl` as the sole release downloader and restore the concise `curl -fsSL ... | sh` installation command.

## 0.14.2

- Support secure release installation and updates with either `curl` or GNU `wget`, covering stock S100 images that do not include `curl`.
- Make the documented one-command installer use `wget`, while retaining `curl` as an equivalent option.

## 0.14.1

- Fix clean-runner release builds by isolating POSIX Shell download and checksum state so first-time dependency downloads retain their final cache destinations.

## 0.14.0

- Add a release-hosted one-command installer with exact-version selection, RDK Linux ARM64 detection, HTTPS-only downloads, archive confinement, and strict SHA256 verification.
- Add `hobot update`, `hobot update --check`, and `hobot uninstall`, while preserving Pi's `hobot update --extensions` behavior.
- Preserve user configuration, sessions, memory, goals, and backups during normal uninstall; require explicit `--purge --yes` for unattended data removal.
- Publish tag-matched Linux ARM64 GitHub Releases with build provenance attestations, installer metadata, checksums, and immutable versioned archives.

## 0.13.5

- Add `/detach` to leave the current persistent TUI while keeping its Agent and tools running.
- Resolve and detach only the invoking terminal after validating the dedicated Hobot Code tmux socket, pane, session, and client TTY.
- Remove the `Ctrl+A` fallback because terminals and editors commonly reserve it for selection or line navigation.

## 0.13.4

- Forward OSC 52 clipboard writes through dedicated persistent tmux sessions so fullscreen drag selection and `/copy` reach the developer's local terminal.
- Document drag-to-copy, `/copy`, and the terminal-native Shift-drag fallback.

## 0.13.3

- Include Pi's read-only `ls`, `find`, and `grep` tools in the developer permission preset.

## 0.13.2

- Add `/permissions preset developer` to enable routine Shell and workspace editing while retaining approval for MCP, persistent-state changes, unknown tools, destructive commands, and writes outside the workspace.
- Show effective permissions for registered tools separately from ordered configured rules, making shadowed entries such as `bash: ask` unambiguous.
- Cover wildcard precedence and the bounded developer preset with regression tests.

## 0.13.1

- Mount `/btw` as a true equal-width fullscreen workspace so the main editor remains active while the side agent runs.
- Add explicit `Ctrl+Shift+Right` and `Ctrl+Shift+Left` focus navigation between the main and side agents.
- Switch input focus by clicking either half while preserving Pi's text selection, links, dragging, and wheel handling.
- Discover the fullscreen input-listener set by listener identity so click focus also works in minified standalone ARM64 builds.
- Route mouse and trackpad scrolling to the side transcript under the pointer, with a native scrollbar and history-friendly follow behavior.
- Keep a non-capturing overlay fallback for narrow terminals and regular TUI mode.
- Make fullscreen TUI the default for new installations so split-pane focus and pointer-routed scrolling work without extra setup.
- Add named `hobot persistent` sessions backed by tmux so Agent and tool processes survive SSH disconnects and can be listed, reattached, or stopped safely.
- Isolate persistent sessions on a dedicated tmux server with packaged mouse, extended-key, focus-event, and 256-color settings, leaving ordinary tmux sessions untouched.

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
