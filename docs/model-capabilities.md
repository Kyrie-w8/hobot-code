# Model capability contract

Hobot Code treats the board runtime as the source of truth for model capabilities. `hobot --offline --list-models` publishes the configured models and the `thinking` and `images` columns. Agentd parses those columns and returns them through `models.list` as a typed capability object.

```json
{
  "provider": "drobotics",
  "id": "kimi-k3",
  "default": true,
  "capabilities": {
    "reasoning": true,
    "imageInput": true
  },
  "capabilitySource": "runtime-model-table"
}
```

Studio enables image attachments only when the effective model explicitly declares `imageInput: true`. Agentd repeats the same check before starting, resuming, restarting, forking, or sending a prompt, so a custom client cannot bypass it.

Older runtimes that do not publish capability columns remain compatible. Their models use `capabilitySource: "conservative-default"` and image input is disabled until the board runtime is upgraded. Unknown or removed model IDs also remain text-only instead of inheriting another model's capability.

The default model is resolved in this order:

1. The exact `ANTHROPIC_MODEL` selection.
2. `drobotics/kimi-k3` when installed.
3. The first configured D-Robotics model.

Studio additionally lists models from the strict Hobot-managed `providers.json`
catalog. It does not automatically expose every Pi login or self-managed
`models.json` entry, which keeps the product picker intentional while the TUI
retains Pi's complete `/model` surface. Managed models carry an explicit source
marker from agentd; Studio never infers trust from a provider name.
4. The first available model.

The runtime table reports configured compatibility, not current gateway availability. Users can explicitly run `hobot model check drobotics/kimi-k3`, or run the **Route** layer in Studio's **Readiness** panel, to verify the real streaming route without creating a task or writing a session. Health results are cached for five minutes and expose only status, a sanitized failure category, transport, and latency.

Use `hobot model probe drobotics/kimi-k3`, or the **Gateway protocol** layer in Studio's **Readiness** panel, to test the configured gateway protocol. The probe makes bounded synthetic requests and checks terminal streaming, a structured tool call, a matching tool-result continuation, and a real image request when the model advertises image input. `verified` means native streaming completed every probed gateway step; `compatible` means the probe completed through the bounded buffered fallback while exposing the incomplete stream as a degraded check. These API status values are not overall model qualification labels. Studio presents them as **Protocol OK**, **Fallback**, and **Protocol failed**. Results are sanitized, cached for one hour, scoped to the exact provider/model, and never contain the credential, prompts, raw gateway bodies, or model output. A skipped image check is valid only for a model that does not advertise image input. Probing consumes provider tokens and therefore runs only on explicit request. `hobot model verify` remains a compatibility alias.

This protocol probe is stronger than a health check, but it does not execute the model through the complete Pi runtime. Use `hobot model runtime-probe` for the isolated Pi suite covering single and parallel tools, semantic argument recovery, structured thinking, correlated read-only approval, a fixed image challenge for models that declare vision, context compaction, and exact-session recovery after forced mid-tool termination. Non-reasoning and text-only models receive explicit `not-applicable` results for those optional stages. The synthetic runtime suite still does not certify reasoning quality, maximum-context behavior, prompt-cache economics, production quota, or completion quality on RDK development tasks. The [model adaptation levels](model-adaptation-levels.md) keep those claims separate.

On a recognized ARM64 RDK board, `hobot model rdk-probe drobotics/kimi-k3` runs the default `read-only-rdk-diagnostic-v1` profile; `--profile` selects another registered read-only workflow. Each exposes only the product's live snapshot and versioned documentation search tools, then independently checks causal tool use, exact board identity, an allowlisted official source, and a strict evidence-only JSON synthesis. The result is bound to the installed product, agentd, Pi contract, complete RDK extension bundle, expert prompt, knowledge pack, board, and RDK OS. `hobot model profiles` shows the per-profile state without a model call. Deployment, multimedia, and hardware planning passes remain planning evidence and do not qualify conversion, inference, execution, physical control, reasoning quality, or every RDK workflow; a dirty development build is never public qualification evidence.

Completed layers are persisted in board-private agentd state and restored by
Studio. `hobot model status PROVIDER/MODEL` reads the same evidence without
calling a model or consuming tokens. Route evidence expires after five minutes
and gateway-protocol evidence after one hour. Deeper evidence remains current
until its exact binding changes. Board, RDK OS, Prompt, extension, or knowledge
changes invalidate the RDK layer; model configuration, product, agentd, or Pi
changes invalidate all affected layers. Stale records remain visible for
diagnosis but never contribute to a current readiness label. The index contains
only sanitized result structures, never credentials, endpoints, prompts, raw
model output, or gateway bodies.

See [D-Robotics model protocol validation](model-conformance-report.md) for dated, environment-scoped results. Treat the report as a reproducible snapshot rather than a permanent model allowlist.
