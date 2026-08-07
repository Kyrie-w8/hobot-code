# Changelog

## 0.2.0

- Add deterministic `/q`, `/quit`, `/exit`, Ctrl-C cancellation, and idle exit behavior.
- Stream Anthropic-compatible text, reasoning summaries, and tool arguments over SSE.
- Add typed agent lifecycle events and visible tool execution status.
- Add queued prompts and cancellable terminal approvals without competing stdin readers.
- Add session listing, model/status display, thinking/details toggles, and session export commands.
- Add HTTP SSE events, active-turn cancellation, and per-session request serialization.
