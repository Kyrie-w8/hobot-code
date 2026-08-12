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
4. The first available model.

The runtime table reports configured compatibility, not current gateway availability. Users can explicitly run `hobot model check drobotics/kimi-k3`, or use **Check** beside Studio's model picker, to verify the real streaming route without creating a task or writing a session. Health results are cached for five minutes and expose only status, a sanitized failure category, transport, and latency. The probe verifies a minimal text request; it does not prove image quality, long-context stability, tool calling, or production quota.
