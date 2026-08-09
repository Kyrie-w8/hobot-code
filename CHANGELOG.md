# Changelog

## 0.5.0

- Replace the custom terminal UI with the pinned official Pi 0.84.1 ARM64 runtime, preserving Pi interaction behavior exactly.
- Brand the unchanged Pi runtime as Aster through the supported `piConfig` package mechanism.
- Add a D-Robotics Kimi provider that maps the gateway's complete Anthropic response into native Pi thinking, text, tool-call, usage, and completion events.
- Add live RDK board status, `system_snapshot`, `/rdk`, `/doctor`, `/q`, and `/exit` through a Pi extension.
- Add board-safe write and destructive-command approval hooks without replacing Pi's built-in tools.
- Bundle pinned fd and ripgrep ARM64 binaries for network-independent first startup.
- Add install-time backup, isolated Pi sessions, legacy Go binary preservation, and `aster-rollback`.

## 0.4.0

- Replace per-session JSONL writes with a crash-safe SQLite WAL store while preserving and incrementally importing legacy files.
- Archive interrupted turns on startup and expose recovery status through `doctor`.
- Add non-destructive `/undo`, `/redo`, and model-assisted `/compact` session commands.
- Preserve full audit records and inject compacted context separately from conversation messages.
- Add native SSE streaming for OpenAI-compatible Chat Completions, OpenAI Responses, and Gemini, including reasoning and tool calls.
- Add protocol fallbacks and deterministic streamed tool-call reconstruction.

## 0.3.0

- Add persistent system or user launch profiles and a validated `aster configure` command.
- Make installed interactive and systemd modes share the same default model and board selection.
- Install provider and board profiles without overwriting the active launcher or secret file.
- Add a responsive terminal header, session status, model/tool metadata, and color fallback.
- Add an animated operation status with elapsed time for model and tool execution.
- Improve reasoning, answer, approval, tool result, cancellation, and queued-prompt presentation.

## 0.2.0

- Add deterministic `/q`, `/quit`, `/exit`, Ctrl-C cancellation, and idle exit behavior.
- Stream Anthropic-compatible text, reasoning summaries, and tool arguments over SSE.
- Add typed agent lifecycle events and visible tool execution status.
- Add queued prompts and cancellable terminal approvals without competing stdin readers.
- Add session listing, model/status display, thinking/details toggles, and session export commands.
- Add HTTP SSE events, active-turn cancellation, and per-session request serialization.
