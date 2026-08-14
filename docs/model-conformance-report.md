# D-Robotics model protocol validation

This report records explicit Hobot Code protocol probes. It is evidence from a
specific gateway, date, board, and client build, not a permanent model
allowlist or a model-quality leaderboard.

## 2026-08-13 S100 validation

Environment:

- Board: RDK S100
- Model gateway: `https://ai-api.d-robotics.cc`
- Hobot Code source line: `0.25.0` development tree
- Probe: bounded streaming, structured tool call, matching tool-result
  continuation, and valid 32x32 PNG input
- Privacy: prompts, model output, raw gateway bodies, and credentials were not
  retained in the report

| Model | Result | Native tool stream | Tool call | Tool-result continuation | Image input | Observed duration |
| --- | --- | --- | --- | --- | --- | ---: |
| `qwen3.8-max` | Protocol OK | Passed | Passed | Passed | Passed | 5.0 s |
| `glm-5.2` | Protocol fallback | Buffered fallback | Passed | Passed | Passed | 10.8 s |
| `kimi-k3` | Not verified in this run | Buffered fallback | Passed | Passed | Gateway image request was unstable | 40.7 s |

`Protocol OK` means every probed gateway step completed with an explicit native
stream terminal event. `Protocol fallback` means the bounded gateway sequence
worked, but one or more successful streamed requests required Hobot Code's JSON
fallback. Neither label proves the complete Pi Agent runtime or RDK task
quality. A failed run must remain visible even when an earlier run passed;
declared image support does not prove current route stability.

The Kimi K3 tool stream ended before the terminal event but its buffered
response contained the expected structured call. The matching tool result was
accepted and produced a valid next assistant turn. Image probing was
non-deterministic across repeated runs, so this snapshot does not certify Kimi
K3 image input for production use.

Re-run the current route before relying on it:

```bash
hobot model probe --force drobotics/qwen3.8-max
hobot model probe --force drobotics/glm-5.2
hobot model probe --force drobotics/kimi-k3
```

The conformance probe does not execute the complete Pi Agent runtime and does
not measure reasoning quality, long-context stability, prompt-cache efficiency,
RDK task completion rate, quota, or cost. Those require the separate pinned
levels defined in [Model adaptation levels](model-adaptation-levels.md).
