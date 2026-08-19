# Changelog

## 0.29.1

### Changed

- Increase the bounded approval-model output budget for reasoning models so they can emit the required final JSON decision after thinking.
- Document the complete `Approve for me` flow, exact runtime system prompt, provider request envelopes, response schema, redaction rules, limits, and fail-closed behavior in the user guide.

### Fixed

- Preserve complete destructive and network facts for remote-build commands, while allowing the approval model to decide scoped cleanup and lifecycle actions instead of forcing confirmation from the command name alone.

## 0.29.0

### Added

- Let each task choose an independent approval model or continue following its Agent model through Studio, RPC, SDK, and CLI.
- Persist lightweight approval-model decisions in the conversation timeline so automatic approvals remain visible without blocking work.

### Changed

- Auto-approve scoped low- and medium-risk development actions, including reversible Hobot Code schedule control, while requiring explicit human confirmation for deletion, process or service interruption, destructive task-state changes, and other external impact.
- Downgrade every model-approved high or critical risk action to manual confirmation at the board control plane.

### Fixed

- Treat bounded Shell brace lists as data and distinguish tokenizer metadata filenames from actual credential references, so inbound model-file transfers no longer fall back to unnecessary human approval.

## 0.28.10

### Fixed

- Persist approval-model audit records in agentd instead of the sandboxed worker. Workspace and stricter sandboxes keep the session policy directory read-only while approvals remain auditable and no longer fall back to a human after a successful model decision.

## 0.28.9

### Fixed

- Keep the task-control socket read side open while waiting for an approval-model decision. This fixes Pi/Bun closing the Unix socket before the board-side reviewer response could be read.

## 0.28.8

### Fixed

- Reserve enough bounded output for reasoning-capable approval models to finish their strict JSON decision after a thinking block, instead of falling back to human approval before the decision is emitted.

## 0.28.7

### Added

- Upgrade **Approve for me** to an isolated, tool-free approval Agent that uses the task model, recent user intent, and exact tool action to review SSH, network, package, service, process, system-path, hardware, MCP, remote-build, and persistent operations.
- Add `tasks.permissions.llm-review.v1` and a task-scoped `permission.review` control method. Model credentials remain in agentd, so approval works across Board access modes; an Offline task still needs a local model before its Agent can propose tools.

### Changed

- Make approval choice independent from Board access and Network. The reviewer may approve an action but cannot bypass the selected filesystem, device, capability, or network boundary.
- Retain manual or denied handling only for critical invariants such as credential exfiltration, broad irreversible destruction, hidden persistent access, disabled security controls, and reviewer infrastructure tampering. Model failures and invalid responses fall back to a human decision.

## 0.28.6

### Added

- Add **Approve for me**, a capability-negotiated board-side reviewer for exact low-risk workspace actions. It replaces repetitive human review without expanding the task sandbox, network, writable roots, root access, device access, or persistent policy.
- Add private bounded reviewer audit records, repeated-denial circuit breaking, and one exact-action re-review while preserving human review for destructive, remote, persistent, dynamic, hardware, MCP, and quality-gate operations.

### Fixed

- Classify Shell risk from executable positions so arguments such as `grep -v chmod` and stored schedule prompts do not create false permission requests.
- Reject auto-review at the daemon boundary when an effective `Review` or `Workspace` OS sandbox is unavailable, including direct CLI or RPC changes that bypass Studio controls.

## 0.28.5

### Fixed

- Parse literal Python heredocs structurally in Developer mode so safe OpenExplorer configuration updates and cache-directory creation no longer prompt merely because the embedded script contains Python syntax. Keep protected paths, startup and credential files, deletion, dynamic file modes, process launch, dynamic code, and unknown Python calls approval-gated.
- Keep a navigated conversation as one continuous event range and page forward as the reader scrolls down, instead of joining an old page directly to the latest scheduled output and silently skipping the messages between them.
- Show a persistent **Latest** action while reading historical pages, while continuing to use **New output** for live messages that arrive when the reader is away from the current tail.
- Let Developer mode run common read-only board and OpenExplorer diagnostics without approval, including process, GPU, storage, compiler, package, environment, container, and cluster inspection commands.
- Keep confirmations for protected or persistent path writes, process and service changes, package and environment mutation, GPU or network reconfiguration, destructive container operations, cluster writes, and unbounded dynamic targets outside the managed OS sandbox.

## 0.28.4

### Added

- Add a low-profile Studio conversation navigator for user messages, with timestamped hover previews, exact click-to-jump highlighting, dense-history grouping, and keyboard navigation.
- Add bounded reverse event pagination so Studio can open earlier retained history without downloading or rendering an unbounded log at startup.

### Fixed

- Preserve the reader's scroll anchor while older pages are inserted, and keep live Agent output from pulling a user away from earlier messages.
- Disclose legacy-board, rotated-retention, expired-cursor, and conflicting-page boundaries instead of silently presenting an incomplete conversation.

## 0.28.3

### Fixed

- Let Studio verify the official stable board release from the Mac when a connected board explicitly times out reaching GitHub. Current and development-ahead boards now receive a truthful version result, while a newer release that the board cannot download remains blocked instead of exposing a broken update action.

## 0.28.2

### Fixed

- Distinguish a locally installed Studio build that is newer than GitHub's latest public stable release from a genuinely current public release, so development builds no longer misleadingly report that they are up to date.
- Fail macOS release jobs early with one actionable list of missing Developer ID signing and Apple notarization secrets instead of an opaque certificate-import failure.

## 0.28.1

### Fixed

- Replace whole-string shell keyword matching with executable-position analysis for Developer approvals. Schedule prompts, grep patterns, and echoed text no longer trigger false `chmod`/`rm`/network risk prompts, while command substitutions, interpreter scripts, SSH payloads, wrappers, dynamic execution, destructive operations, and protected state remain guarded.
- Preserve the actual `shared`, `model-only`, or `offline` runtime boundary in permission diagnostics and enforce tool egress consistently with the OS sandbox.

## 0.28.0

### Added

- Add board-owned one-shot and recurring schedules for existing main tasks, with CLI, Go SDK, Studio management, task-scoped Agent self-management, pause, resume, run-now, deletion, and private persistent state.
- Reuse the task's current session, model, permissions, sandbox, network, and workspace while coalescing missed or busy intervals into one run and refusing automatic replay after an uncertain crash window.

### Changed

- Distinguish scheduled prompts from user-authored messages in Studio, keep schedule prompts out of default lists and support bundles, and block task archive or deletion while schedules still reference it.
- Add strict schedule protocol validation, private-directory diagnostics, scoped Unix-socket authorization, Side Agent isolation, durable dispatch claims, bounded limits, and race-tested restart behavior.

### Fixed

- Route the public `hobot schedule` command through the board control service instead of passing it to the Pi conversation runtime.
- Route Agent-owned schedule commands through their task-scoped control socket before the sandboxed launcher's read-only configuration checks.
- Let a managed main Agent create its own schedule without supplying a task ID or name, accept `--prompt` as an explicit prompt form, and replace opaque cron/argument failures with actionable fixed-interval guidance.

## 0.27.7

### Changed

- Introduce a consistent Studio typography scale, raise conversation and composer text to 16px, and keep navigation, controls, code, diagnostics, and capability metadata readable without sacrificing information hierarchy.
- Preserve the larger typography at narrow window widths and add regression coverage that prevents sub-11px fixed text or responsive font-size reductions from returning.

## 0.27.6

### Fixed

- Give Studio and TUI Side Agents an enforced Side identity, refresh bounded Main-Agent activity without sharing hidden reasoning, keep Side Agents flat while retaining their true source task, and fail closed on shared-workspace writes whenever the Main Agent is active or collaboration state cannot be verified.
- Limit live Side Agents per main task, advertise the limit through the board protocol, and expose safe collaboration activity in Studio while retaining existing workspace, worker, model-egress, and RDK hardware coordination boundaries.

## 0.27.5

### Fixed

- Classify process-control commands by their effective operation instead of their executable name: literal signal-zero probes and signal-list/help queries are observation-only in Developer mode, while terminating, dynamic, mixed, and ambiguous forms remain approval-gated.

## 0.27.4

### Fixed

- Present Pi automatic model retries as explicit `n/5` progress, remove recoverable intermediate failure cards after a successful retry, and show an actionable error only after retry exhaustion.
- Classify fallback throttling as temporary model load even when the primary gateway route also reports an unsupported model, avoiding a misleading permanent model-unavailable diagnosis.
- Preserve retry progress through normalized board events without exposing gateway error payloads, while allowing Studio to repair the presentation of retained events created by older board versions.
- Raise the default Agent retry limit from three to five and migrate installations that still use the previous default.

## 0.27.3

### Fixed

- Editing remains available when a previously edited Pi session has lost its header or branch prefix: agentd now maps the selected user-message occurrence back through the edit ancestry and forks from the nearest healthy parent context.
- Recovery is limited to messages at or before the recorded edit anchor and validates the unchanged prompt prefix, so a damaged session cannot silently attach an edit to unrelated context.

## 0.27.2

### Fixed

- Editing a repeated user message such as `continue` now resolves the exact turn by retained event order and the active Pi session branch instead of rejecting duplicate text as ambiguous.
- Developer permissions now allow routine network, remote-build, quality-gate, memory, and goal operations while preserving confirmation for destructive shell commands, protected paths, system changes, and unclassified MCP tools.
- Remote OpenExplorer build commands now receive the same destructive-command inspection as local Bash commands.
- Commands that remove or replace Hobot Code's persistent state now explain that they can destroy task and conversation data.

## 0.27.1

- Replace the ambiguous **Allow this exact call for this task** approval with capability-scoped choices. Ordinary and high-risk calls now offer only one-time approval; recognized network access can be allowed for the current task, and a selected OpenExplorer build host can be trusted for that task only after a successful probe. Existing exact-call records remain readable solely for session compatibility.
- Add independently controlled Studio and board updates. Studio accepts only the versioned stable ARM64 DMG and matching SHA-256 asset from the fixed official release, while active board tasks block only the transactional board update.
- Render inline and display mathematics in conversations, while preserving single tildes used in model ranges and command output instead of interpreting them as strikethrough.
- Accept legacy underscore-based diagnostic utility identifiers after canonicalizing them without hiding duplicates.
- Route `hobot version`, `hobot --version`, and `hobot -v` directly to the version command before loading user configuration or starting a conversation.

## 0.27.0

- Import the official customer-catalog Skills from a user-supplied OpenExplorer LLM package through bounded, owner-controlled, symlink-safe discovery without redistributing or modifying vendor files. Capabilities preserve the 24-directory versus 23-entry catalog discrepancy and unknown vendor test state; only cataloged Skills enter private task settings. Host-side workflows use a task-scoped, user-selected direct SSH x86_64 builder with architecture/CUDA probes, bounded output, board-side network policy, and per-command approval instead of running conversion or quantization on ARM64 RDK boards.
- Keep long-running task history durable after its event log reaches the configured size. Agentd now atomically rolls to a continuous newest-event window, migrates logs produced by older stop-persisting versions on the next event, and reports retained boundaries through `events.retention.v1` so CLI and Studio can distinguish expired history from an actual durability gap.
- Add `readiness-diagnostics` to the mandatory X5/S100/S600 release matrix. The ordinary-user board harness now proves that `hobot doctor --json` and `diagnostics.inspect` are read-only, repairs require explicit confirmation, permission repair is limited to declared private runtime paths, and no support file, model request, credential, or temporary path escapes the isolated test.
- Add `hobot doctor` and `diagnostics.inspect.v1` as a zero-side-effect first-run readiness contract across CLI, Go SDK, and Studio. Direct `hobot` launch now stops before an unusable empty conversation when no model Provider is configured, while explicit TUI inspection remains available.
- Add explicitly confirmed, fail-closed diagnostic repairs. agentd may only restrict known current-user runtime paths through no-follow file descriptors; Studio and CLI may restart stale configuration only when no active or queued Agent task exists. Credentials, configuration content, ownership, links, system software, dependencies, and resource conditions are never auto-repaired.
- Upgrade one-click diagnostics to `support.bundle.v2`: classify build, board, private state, sandbox, model route, resources, stalled task lifecycle, and recent sanitized failures into healthy, attention, or action-required outcomes; show bounded recovery actions directly in the CLI and Studio; keep optional utilities and unused model egress informational; and strictly validate downloaded metadata and content while retaining v1 client compatibility.
- Keep the public `hobot.env.example` release template readable in a root-owned immutable candidate while continuing to install the user's live `hobot.env` as private `0600`; package validation now rejects regressions in the public template mode.
- Add an isolated root-level `install-lifecycle` release scenario for X5, S100, and S600. It exercises first install, an ordinary-user launcher, data-preserving upgrade, post-swap failure recovery, rollback, and non-purge uninstall while fingerprinting the board's existing installation before and after. Install, rollback, uninstall, update, and launcher paths now share a fail-closed test root that production configuration cannot enable.
- Make public releases a protected two-stage operation: tag builds now stop at a verified draft, while a separate production-reviewed promotion workflow requires a fresh, complete X5/S100/S600 acceptance matrix for the exact clean ARM64 tag build. Promotion revalidates the archive and package, binds all hashes in deterministic public release evidence, attests that evidence, and is the only workflow allowed to publish the draft.
- Add enforceable `shared`/`model-only`/`offline` network boundaries across TUI, persistent tasks, CLI/RPC/SDK, and Studio. `model-only` keeps the built-in D-Robotics Provider and credential-backed Hobot-managed Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses Providers working through an agentd-owned Unix Socket broker while the worker has no general network or model credential. The broker freezes an exact Provider/model allowlist at daemon startup and fixes the HTTPS origin, protocol route, method, authentication, redirects, concurrency, and payload limits. `offline` hides the broker and requires a local model; Google Generative AI, Pi login, self-managed models, missing credentials, and unknown routes fail closed instead of falling back to shared networking.
- Give every background Pi worker a private writable runtime snapshot of `settings.json` and `models.json`, preserving Pi's settings-lock lifecycle without making canonical user configuration writable in the sandbox. Pi login `auth.json` is copied only for `shared` networking and is removed from `model-only` and `offline` task snapshots.
- Ship a repeatable `model-egress-runtime` board acceptance harness with each ARM64 release. It verifies the complete package manifest, then uses fake credentials and an isolated localhost gateway to prove the packaged Pi Anthropic/OpenAI adapters, exact `model-only` broker routes, complete event lifecycle, and worker credential isolation before writing a private sanitized report for X5, S100, and S600 release evidence.
- Add a strict three-board acceptance matrix verifier that rejects missing boards or scenarios, duplicate or public reports, unknown fields, mixed binaries or package manifests, and Pi contract drift. A selected scenario can pass independently, while the public release gate remains `incomplete` until every declared scenario passes on X5, S100, and S600.
- Extend packaged board acceptance with a real Pi `rpc-background` scenario covering exact tool approval, single execution, a second turn, bounded image input without payload retention, fresh RPC reconnects, multi-turn Side Agents, flat root parentage, and continued main-task input.
- Add a real Pi `session-recovery` board scenario covering semantic context compaction with net token reduction, forced mid-tool termination, exact private-session resume without tool replay, fresh clients, and history-edit branching. Runtime-probe diagnostics now ignore unrelated extension UI events and identify the exact bounded compaction condition that failed.
- Add an `extension-safety` board scenario that verifies the packaged extension and Skill inventory, a real parallel extension tool batch, correlated board-side permission hooks, and fail-closed workspace write leases across competing Agents.
- Add an ordinary-user `tui-basics` board scenario using a real PTY to verify Chinese input, structured thinking, draft editing, persistent detach, reattach, and continued conversation. Persistent tmux servers now discard model credentials before they become long-lived process state and keep their socket in the private user state directory instead of assuming `/tmp` is accessible.
- Strip structured image payloads from both agentd-authored and upstream Pi worker events before they reach subscribers or `events.jsonl`; retain bounded attachment metadata while leaving the private Pi session responsible for model-context recovery.
- Expand the read-only capability catalog to Pi extensions, Skills, prompt templates, themes, package declarations, and task-bound project resources. Discovery is bounded, owner-checked, symlink-safe, path-redacted, and gated by the task's persisted project-trust decision; declared or discovered resources are never presented as loaded.
- Add task-context capability inventory across CLI, SDK, and Studio, including source, scope, trust, permissions, and truthful diagnostics. MCP remains attributed to its real extension/package host instead of being inferred from filenames.
- Add strict Hobot-managed API Providers for Anthropic Messages, OpenAI Chat Completions, OpenAI Responses, and Google Generative AI. Secrets remain outside JSON, travel through the existing anonymous-descriptor/sandbox one-shot credential boundary, inherit into Side Agents, and never enter tool environments.
- Add `hobot provider list|add|rotate|remove` with hidden or stdin credential entry, redacted status, strict schema validation, cross-process locking, private durable writes, shared-key confirmation, explicit deletion confirmation, and crash-safe publication ordering.
- Add Studio Provider management for safe creation, removal, and in-place key rotation over fixed short-lived SSH commands; keys remain outside React state, browser storage, command arguments, task events, and the long-lived bridge.
- Mark managed model provenance in agentd and expose only built-in plus explicit managed models in Studio; apply isolated Agent runtime and RDK profile probes to managed models without overstating provider-specific direct protocol coverage.
- Separate model connectivity, gateway protocol, Agent runtime, and RDK task qualification; add `hobot model probe` while retaining `verify` as a compatibility alias, and remove ambiguous overall `Verified` labels from Studio.
- Add `hobot model runtime-probe`, an isolated Pi RPC suite covering tools, semantic argument recovery, private structured thinking, correlated read-only approval, declared image input, semantic context compaction, and exact-session recovery after forced mid-tool termination while retaining an explicit partial-only result and capability-aware `not-applicable` states.
- Add `hobot model rdk-probe`, a non-cached, board-bound `read-only-rdk-diagnostic-v1` qualification profile that cross-checks live RDK identity, versioned official knowledge, causal read-only tools, and strict model synthesis while binding the complete release and RDK knowledge/runtime inputs.
- Replace Studio's separate model check buttons with one layered **Readiness** panel for route, gateway protocol, isolated Agent runtime, and a per-workflow RDK matrix; preserve model/board/build scope and keep development evidence visibly distinct from release qualification.
- Persist sanitized model readiness evidence in private board state, restore it in Studio without model calls, expose `hobot model status`, and invalidate only the layers affected by configuration, build, Pi, board, RDK OS, Prompt, extension, knowledge, or TTL drift.
- Add a versioned RDK workflow registry and private per-model/profile evidence matrix for X5, S100, and S600; expose `hobot model profiles`, profile-selectable probes, strict SDK validation, and a Studio matrix that separates planning, current, stale, and not-implemented evidence without implying execution.
- Bind the installed Pi compatibility contract to the verified agentd build identity and reject missing, writable, linked, or mutated contract files.
- Add a machine-readable Pi upstream compatibility contract and release gate across source tests, packaged runtime documentation, build identity, Studio, and three-board reliability evidence.
- Fall back to the private Hobot Code state directory for the per-user daemon socket when `XDG_RUNTIME_DIR` is unavailable, so unprivileged users work on RDK images whose `/tmp` is root-only.

## 0.26.0

- Add one-click stable board updates in Hobot Studio with active-task preflight, a fixed non-downgrading SSH command boundary, automatic bridge shutdown, reconnect, and post-install version verification.
- Add the versioned `hobot.extensions/v1` catalog across agentd, CLI, Go SDK, and Studio so built-in extensions and Skills expose their origin, capabilities, declared permissions, and supported RDK targets without loading code or bypassing board-side policy.
- Add a searchable Studio capability center and live, secret-free discovery of private user Provider, Hook, and LSP configuration. Missing or unsafe sources fail closed, refresh without restarting agentd, and never expose endpoint credentials or command arguments to clients.
- Add explicit model protocol verification across agentd, CLI, SDK, and Studio. `hobot model verify` distinguishes a reachable endpoint from an Agent-ready model by testing terminal streaming, structured tools, matching tool results, and declared image input without retaining raw provider content.
- Add board-side bubblewrap profiles for foreground and background Agents: read-only review, writable workspace, RDK hardware access, and explicit opt-out. The default TUI and persistent launcher now share the same versioned execution boundary, re-protect common credentials inside broad home workspaces, and report the effective file, device, capability, and shared-network boundary through CLI, SDK, Studio, diagnostics, and compatibility checks.
- Add board-enforced per-task Git worktrees for clean committed repositories, Studio preflight and workspace selection, inherited Side Agent/edit branches, and fail-closed explicit cleanup that preserves dirty or newly committed code.
- Serialize workspace-changing turns across main, side, foreground, and background Agents with private crash-recoverable write leases; detect external source changes before each Agent write, expose live ownership in Studio diagnostics and the board CLI, and keep support bundles identity-redacted.
- Add reviewed delivery for isolated task workspaces: Studio and CLI preflight an exact binary-capable snapshot, stop idle sibling Agents only after confirmation, and apply it to an unchanged original project as staged Git changes without committing or pushing.
- Queue new, forked, resumed, and restarted Agent work in a private FIFO when all board worker slots are busy; preserve not-yet-executed prompts across daemon restarts, support cancellation, and never replay an already-started tool turn.
- Add normalized event schema 4 with stable item lifecycle semantics and bounded command/tool previews while retaining schema 3 fields and capability compatibility for older clients.
- Classify terminal task failures into stable, sanitized reasons with one safe recovery action; persist terminal lifecycle events so Studio does not mistake a completed task stream for an SSH disconnect.
- Persist a bounded, privacy-preserving turn ledger with tool completion counts and Git workspace state summaries; show uncertain side effects in Studio recovery guidance, CLI summaries, and support bundles without automatically replaying work.
- Replace Studio's protocol-first compatibility block with a user-facing readiness outcome, one primary action, and collapsible technical evidence; distinguish daily Agent availability from hardware-production validation.
- Add a bounded, read-only Studio change review for each task workspace; bind inspection to the server-side task directory, disable executable Git integrations, omit untracked file contents, and label shared-workspace attribution honestly.
- Bind every packaged `agentd` executable to strict release metadata and expose its source commit, clean/dirty state, build time, Pi runtime, and binary SHA-256 to diagnostics and Studio compatibility checks.
- Add a private, resumable X5/S100/S600 reliability verifier with bounded SSH/RPC sampling, sanitized evidence, release-capability checks, and an opt-in idle-daemon recovery test that refuses to interrupt live tasks.
- Add recorded current, previous-minor, and minimum-schema handshake fixtures so SDK and Studio compatibility behavior is replayed in CI instead of inferred from version numbers alone.
- Resume `hobot task attach` from a private per-task display cursor after disconnects, add explicit full replay, and default task and approval inspection to redacted summaries with opt-in local details.

## 0.25.0

- Bound update checks to ten seconds on disconnected boards, preserve the installed version, and replace raw curl failures with a concise recovery message while retaining opt-in debug details.
- Refuse implicit downgrades when cached or stale latest-release metadata is older than the installed version; intentional downgrades now require both an explicit version and explicit consent.
- Pin resolved updates to immutable versioned assets, restage packages in a root-owned private directory, block runtime starts during install/rollback, and scan again immediately before swapping the runtime.
- Make task command help side-effect free and protect active board tasks, foreground sessions, persistent clients, automations, and Studio bridges during transactional updates.
- Invalidate pending approvals when a task reaches a terminal state, expose task model and permission changes in the board CLI, and preserve slash-containing D-Robotics gateway groups as configured defaults.
- Verify Studio board connections before saving them; add saved-board editing/removal, board workspace browsing/creation, offline draft recovery, version guidance, background-task attention badges, and explicit image handling when editing history.
- Add a private `hobot setup` flow for first-run D-Robotics model configuration, with hidden token input, atomic `0600` writes, non-interactive stdin support, and explicit daemon restart guidance.
- Detect model configuration drift between the launcher and a running `agentd`, refusing model-dependent operations with a clear restart command instead of silently using stale credentials or routes.
- Preserve bounded model failures as structured conversation events so Studio can show actionable inline recovery rather than dropping an empty failed response.
- Support confirm, select, input, and editor approvals in Studio, and make `hobot task attach` handle approvals interactively without terminating the board-side task when the client disconnects.
- Resolve `hobot task respond ... yes|no` against the active approval shape, mapping select approvals to their first or last option instead of sending an incompatible confirm response.
- Add per-task model and permission options to `hobot task start`, clarify project trust as `--trust-project`, and extend model-health client deadlines beyond the server-side probe timeout.
- Scope Studio requests to the selected board and task, recover live event streams with bounded backoff, and keep stale asynchronous results from replacing the user's current conversation.
- Turn failed Studio responses into concise recovery cards with model checks and timeline-correct edit-and-retry actions, without exposing raw provider payloads.
- Stage GitHub releases as drafts and verify the complete asset list and published SHA-256 files before making a release visible.

## 0.24.0

- Add an explicit `hobot model check` and Studio model-health control that validates the real D-Robotics streaming route without creating a task, caches results for five minutes, and returns only sanitized failure categories and bounded latency metadata.
- Negotiate model reasoning and image-input capabilities from the board runtime, disable unsupported attachments in Studio, and enforce the same contract again in agentd.
- Evaluate Studio/agentd protocol, event schema, feature capabilities, product versions, board model, and RDK OS during connection; reject unsafe partial compatibility and explain supported degradations.
- Serialize BPU, camera-device, and RDK media-pipeline tool calls through private, crash-recoverable hardware leases, with live occupancy in Studio and identity-redacted support bundles.
- Add `/cache [status|reset]` with gateway-reported aggregate/latest cache hit rates, input-token accounting, and privacy-preserving system/tool contract fingerprints.
- Freeze the rendered RDK expert prompt for each session so runtime template or board-state changes cannot silently invalidate an established model prefix.
- Add D-Robotics DeepSeek V4 Flash and Pro to the TUI and Studio model catalogs, route Flash through the verified `deepseek/deepseek-v4-flash` gateway model group, and map Pi thinking-off to the chat-template control while retaining text-only input validation.
- Publish reproducible S100 cache baselines for Kimi K3, GLM 5.2, and DeepSeek V4 Flash, including 99%+ stable-prefix results for all three model families.
- Add `hobot diagnose` and the `support.bundle.v1` control-plane capability for one-command, board-side diagnostics with bounded retention and private file permissions.
- Produce a self-describing support document with RDK identity and telemetry, daemon limits, fixed utility availability, structured health checks, and pseudonymous task summaries.
- Exclude conversations, prompts, tool inputs and outputs, environment variables, credentials, project files, raw logs, hostnames, local paths, and raw error messages from support documents by construction, with regression tests for representative secrets.
- Let Hobot Studio generate, integrity-check, and save a support document through the existing SSH bridge without weakening board-side authorization or exposing a new network service.
- Keep Studio reproducibly buildable from a clean checkout by using the Wails runtime injected into the webview instead of importing ignored generated bindings.

## 0.23.7

- Read Hbmem pool capacity and allocation from the kernel ION debugfs ledger, using the official board monitor only for DDR bandwidth or as an explicitly marked estimate when debugfs is unavailable.
- Attribute Hbmem to live matching processes with RSS context, separate application bytes from driver, firmware, and unowned system bytes, and reject stale records after PID reuse.
- Present the BPU/codec, VIO/system, and DMA shared-memory pools in development-focused order without describing shared DDR as dedicated accelerator VRAM.

## 0.23.6

- Sample the official RDK accelerator monitor through a bounded, cached collector to expose DDR bandwidth, named Hbmem pools, and any process attribution supplied by the board runtime.
- Present Hbmem pools as used-capacity bars, show DDR bandwidth only while BPU work is active, and list BPU processes only when the RDK runtime reports trustworthy records.
- Keep older board services compatible with a conservative allocation-only fallback instead of inventing a shared accelerator-memory total.

## 0.23.5

- Refocus the Studio inspector on board monitoring by removing duplicate task, workspace, and diagnostic metadata.
- Present CPU load, system memory, disk usage, and temperature as consistent capacity bars with compact values and threshold-aware color.
- Replace the dense ION/Hbmem table with only the allocation views exposed by the board, omit unavailable counters, and explain that BPU client, ION, CMA, and DMA-BUF measurements can overlap.
- Remove the duplicate inspector refresh action and fit the complete healthy-board overview within a common 720 px application height.

## 0.23.4

- Recover live task updates after an isolated SSH subscription reset by resuming from the last durable event sequence with bounded exponential backoff.
- Distinguish retryable transport interruptions from fatal event protocol errors, and keep transient reconnects out of the global error banner.
- Show a compact task-local reconnecting indicator that clears as soon as the subscription handshake succeeds.

## 0.23.3

- Make historical message editing replace the visible conversation timeline: retain context before the edited prompt, discard later turns from the new timeline, and keep the replacement in the main conversation instead of presenting it as a Side Agent.
- Stop the superseded idle worker before starting an edited timeline, preserve internal session ancestry for recovery, and bound copied event history on complete user-turn boundaries.
- Fold successive edits into one project conversation while keeping genuine Side Agents as independent sibling conversations.

## 0.23.2

- Recover each task's next event sequence from its durable log after an agentd restart, preventing stale metadata from appending duplicate sequence numbers and breaking Studio reconnects.
- Repair the continuous rollback suffix produced by affected older daemons without discarding conversation events, while continuing to reject malformed JSON, foreign task IDs, and genuine sequence gaps.

## 0.23.1

- Rename visible accelerator metrics to the RDK terms BPU load, BPU frequency, and ION/Hbmem; keep BPU client, ION, CMA, and DMA-BUF measurements separate instead of presenting an invented AI-memory total.
- Explain whether BPU telemetry is unavailable because the board service is old, no BPU device exists, the RDK OS exposes no metric node, or a node could not be read.
- Add system-level BPU devfreq fallback discovery and suppress impossible ION heap capacities larger than physical memory without discarding valid allocation data.
- Strengthen verified deployments with schema-v2 numerical accuracy thresholds, model and end-to-end latency distributions, resource samples and explicit thermal/memory limits; add a reproducible RT-IGEV acceptance profile for RDK X5.

## 0.23.0

- Add professional accelerator monitoring with per-core BPU utilization and frequency, BPU-specific temperature, bounded ION/CMA/dma-buf memory telemetry, orphaned-buffer warnings, and honest per-board fallback states.
- Add a board-bound model deployment workflow: bounded artifact discovery, conservative X5/Bayes, S100/Nash-E/Nash-M, and S600/Nash-P compatibility triage, persistent Agent execution, structured acceptance reports, server-verified artifact digests, and a Studio deployment wizard with live status.

## 0.22.4

- Add a bounded `system.snapshot` capability so Studio can show the exact RDK identity, OS, BPU devices, temperature, memory, storage, load, uptime, and board-side validation tools without exposing credentials.
- Turn board telemetry into actionable readiness guidance for thermal, memory, storage, BPU, and runtime-tool problems, while degrading cleanly against older board releases.
- Add editable RDK workflow starters for board diagnosis, model deployment, camera pipelines, TROS workspaces, and reproducible BPU validation.
- Validate telemetry on X5, S100, and S600, count BPU cores without unrelated `hobot-*` devices, and discover board utilities in the generation-specific `/usr/hobot/bin` and `/usr/sbin` locations.
- Make Developer permissions risk-based under root: routine inspection, builds, tests, and workspace edits run without repetitive prompts, while destructive Git/filesystem actions, protected-system writes, service/package/kernel/network changes, process termination, and board hardware writes still require approval.
- Replace command-line substring process detection in install, rollback, and uninstall flows with exact `/proc/<pid>/exe` checks, preventing SSH wrapper commands from falsely blocking upgrades.
- Strengthen Studio board refresh state, automatically restore task subscriptions after an SSH control reconnect, and add real-board SDK, snapshot, and frontend health regression coverage.

## 0.22.3

- Preserve single tildes in Studio Markdown so ranges and approximate CLI values do not accidentally strike through intervening options, while retaining standard double-tilde strikethrough.

## 0.22.2

- Bind the packaged agentd binary to the product version with an embedded release marker, reject stale binaries during packaging, and fail installation when the CLI, daemon, and archive versions differ.

## 0.22.1

- Make release metadata generation and package validation execute reliably through symbolic directory paths, preventing macOS `/tmp` path aliases from producing archives without `BUILD_INFO.json` or `MANIFEST.sha256`.

## 0.22.0

- Make Ask the default background-task permission mode, require exact-call approval for root shell and file mutations, bound remembered approvals, and prevent broad or legacy root policies from silently authorizing new targets.
- Make task stop synchronous with worker and output-stream teardown, eliminating the CI cleanup race and preventing failed workers from losing their process identity before collection.
- Require Developer ID signing, hardened runtime, Apple notarization, stapling, and Gatekeeper validation for public macOS releases while retaining credential-free local development builds.
- Show complete tool, risk, target, and reason details in the Studio approval panel, with an explicit explanation of exact-call scope and a tested minimum-window layout.
- Bound board storage with three recent upgrade backups within 768 MiB by default, a protected rollback point, 100 retained tasks, and configurable 16 MiB per-task event logs.

## 0.21.0

- Studio sends and stops from one stateful composer button, limits its model picker to the three D-Robotics gateway models, and offers S100, S600, and X5 board presets.
- Projects support multiple tasks with prompt-derived editable titles, and Studio refreshes full task state so pending approvals remain actionable.
- Tool approvals now offer allow once, allow for this task, and deny while retaining mandatory confirmation for destructive commands, protected paths, and out-of-workspace writes.
- Studio image prompts support bounded local compression and validated JPEG, PNG, WebP, and GIF content over the existing SSH/RPC channel. Document attachments remain unsupported.

## 0.20.0

- Make the Studio composer send and stop actions share one stable button position, and restrict the desktop model picker to D-Robotics models.
- Let every project create multiple conversations, derive readable Unicode titles from the first instruction, and support inline conversation renaming.
- Add per-task Review, Ask, and Developer approval modes whose private policies remain enforced on the RDK board with high-risk operations guarded.
- Isolate board switches in Studio by replacing the selected task and event stream atomically, closing the previous SSH client, and ignoring late watcher errors.
- Move release, module, CI, and security URLs to the repository's new `bryant-w/hobot-code` owner so installers do not depend on a legacy username redirect.
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
