# Model adaptation levels

Hobot Code separates model availability from product qualification. A model
must not receive a stronger label because it passed a weaker test.

| Level | User-visible claim | Required evidence | Current command |
| --- | --- | --- | --- |
| Connectivity | The configured route responds now | Bounded text request, sanitized failure category, first-byte and total latency | `hobot model check` |
| Gateway protocol | The route supports the probed wire behavior | Terminal streaming, one structured tool call, matching tool-result continuation, and declared image input | `hobot model probe` |
| Agent runtime, synthetic | The model completes the isolated Hobot Code runtime suite | Pinned Pi RPC runtime, exact model selection, single and parallel tools, semantic argument recovery, structured thinking, correlated approval, declared image input, context compaction, forced mid-tool termination, exact-session recovery, exact continuations, settled barriers | `hobot model runtime-probe` |
| RDK read-only profile | The model passed one named, bounded read-only board workflow | Exact board/OS, build identity, Pi contract, prompt/extension/knowledge digests, constrained tools, live evidence, official sources, strict synthesis, and explicit non-coverage | `hobot model rdk-probe [--profile ID]` |
| RDK workflow suite | The model is broadly qualified for RDK development | Multiple dated profiles across X5/S100/S600, including coding, deployment, multimedia, hardware, success, latency, token and recovery metrics | Not implemented yet |

`models.conformance.v1` currently produces only the gateway-protocol level. Its
result therefore includes:

```json
{
  "schemaVersion": 1,
  "scope": "gateway-protocol",
  "status": "verified",
  "agentRuntimeStatus": "not-tested",
  "rdkTaskStatus": "not-tested"
}
```

The existing `verified`, `compatible`, and `failed` status values are retained
for protocol compatibility. In Studio they are presented as **Protocol OK**,
**Fallback**, and **Protocol failed**. They are never presented as overall
model qualification.

`hobot model runtime-probe` first starts an ephemeral Pi RPC process with
persistence, built-in tools, discovered extensions, Skills, prompt templates,
and project context disabled. It loads the packaged D-Robotics provider plus
one product-owned deterministic probe tool. Six stages test one tool call, two
calls emitted before either result, repair after an intentional semantic
argument error, structured thinking kept separate from exact final text, an
exact correlated confirmation, and a fixed metadata-free four-quadrant PNG.

A second Pi process uses a private temporary session. The probe places an
opaque token in older context, compacts the session with a small bounded
retention window, requires the model to recover the token from compacted
context, then begins a deliberately non-completing tool call. Agentd verifies
that the user turn and assistant tool call are durably present before killing
the entire Pi process group. It restarts Pi with the exact validated session
file and requires the model to recover a second token without replaying the
interrupted tool. Session paths must remain under the private temporary root,
must not be symbolic links, and become private bounded regular files before
resume.

The event validator requires correlated IDs, causal ordering, successful
token reduction, identical session ID/path/model after restart, and settled
barriers. It records only event shape and exact synthetic sentinels, never the
model's thinking text or user content. Models that do not declare reasoning or
image input receive a strict `not-applicable` check for that stage; a declared
capability must pass. Passing still returns `status: "partial"`, because this is
synthetic Agent-runtime evidence rather than RDK task qualification. The only
remaining `pending` check is `rdk-task-suite`. Only one runtime probe may run
per board at a time. Temporary config and state are removed after success,
failure, or timeout, and the model token is passed through the same anonymous
descriptor used by background workers. A failure exposes only a bounded stage
category (`preparation`, `configuration`, `process`, `protocol`, or `timeout`),
never raw Provider output or credentials.

Both built-in D-Robotics models and explicit Hobot-managed providers can run the
runtime and RDK layers. Direct route and gateway-protocol probes remain specific
to the built-in D-Robotics gateway until each other transport has a truthful
provider-specific conformance adapter.

`hobot model rdk-probe` runs one registered, deliberately narrow read-only RDK
profile. It runs only on a recognized ARM64 X5, S100, or S600, disables
persistence, built-in tools, user extensions, Skills, prompt templates, and
project context, and exposes only `system_snapshot` and `rdk_docs_search` from
the product RDK extension. The model must first obtain a live snapshot, then
query the exact board and RDK OS documentation track for the selected workflow,
and finally return one strict JSON object derived from those results.

| Profile | Evidence class | What a pass means | Explicitly not proved |
| --- | --- | --- | --- |
| `read-only-rdk-diagnostic-v1` | Live read-only | Board identity and diagnostic guidance were grounded in matching live and official evidence | Coding, deployment, multimedia, hardware control |
| `read-only-model-deployment-planning-v1` | Knowledge-grounded planning | The model formed a board/version-aware conversion and validation plan | Conversion, board inference, accuracy, performance |
| `read-only-multimedia-planning-v1` | Knowledge-grounded planning | The model formed a board/version-aware camera, codec, display, or TROS plan | Capture, codec execution, throughput, device integration |
| `read-only-hardware-safety-planning-v1` | Knowledge-grounded planning | The model formed a board/version-aware risk and rollback plan | GPIO/CAN writes, firmware update, power cycle |
| `isolated-workspace-coding-v1` | Not implemented | Nothing; the profile is visible as `planned` | Repository edit, quality gate, change review |

`hobot model profiles PROVIDER/MODEL` and Studio's RDK workflow matrix read
these states without a model call. `available` describes whether a profile can
run on the current target; `untested`, `current`, and `stale` describe evidence.
`planned` is never an evidence state and cannot contain a result.

Agentd independently samples the board and rejects repeated, parallel,
unapproved, mismatched, out-of-order, or malformed tool evidence. It accepts
only D-Robotics official sources already present in the versioned knowledge
manifest and requires the model's conclusion to reproduce the exact board,
RDK OS, architecture, knowledge version, source, and four deterministic
signals. The public result contains checks and official source URLs, not the
prompt, thinking, raw tool data, raw model answer, or credential.

The result binds the Hobot Code version and build state, source commit, Linux
ARM64 agentd binary digest, Pi version/commit/compatibility digest, expert
prompt digest, complete RDK extension bundle digest, complete knowledge pack
digest/version/date, provider/model, board identity, RDK OS, architecture, and
profile revision. Resources are hashed before and after the run. A passing
dirty or unverifiable development build remains useful locally but has
`releaseEligible: false`. Passing any current read-only profile does not prove
the profile's explicit non-coverage. No current profile clears the runtime
probe's broader `rdk-task-suite` pending item.

Agentd stores the latest sanitized layers for at most 32 exact provider/model
pairs in private `agentd/model-qualification.json`; its RDK layer remains the
default diagnostic profile for backward compatibility. A separate private
`agentd/model-rdk-matrix.json` stores at most 64 exact provider/model/profile
records. Opening Studio's Readiness panel, running `hobot model status`, or
running `hobot model profiles` reads these files without calling a model. Route
evidence expires after five minutes and protocol evidence after one hour.
Configuration, product, build, Pi, board, Prompt, extension, or knowledge drift
marks only the affected evidence stale; stale or expired evidence remains
visible for diagnosis but no longer contributes to a current label. Neither
file contains credentials, endpoints, prompts, raw model responses, or raw
board/tool output.

## Qualification policy

1. Connectivity results expire after five minutes. Gateway protocol results
   expire after one hour. Neither is a permanent allowlist.
2. A runtime or RDK qualification must bind the Hobot Code version and build,
   agentd binary, Pi version/commit/compatibility digest, provider/model,
   prompt and product-extension revision, board model, RDK OS, test profile,
   date, and sanitized result.
3. Any required failure prevents the stronger label. Buffered fallback remains
   visible and cannot be rewritten as native streaming.
4. Credentials, raw gateway responses, model output, user files, and prompts
   are not included in the public result.
5. Model deployment accuracy and latency are properties of an on-device model
   artifact, not of the cloud model driving Hobot Code. They use the separate
   deployment evidence contract.

This separation allows third-party providers and self-hosted models to be
added without hard-coded brand exceptions while keeping product claims
auditable.
