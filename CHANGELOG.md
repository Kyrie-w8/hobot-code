# Changelog

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
