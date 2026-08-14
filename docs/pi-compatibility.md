# Pi upstream compatibility

Hobot Code embeds a pinned Pi runtime instead of maintaining a fork. A matching
version and archive hash prove provenance, but they do not prove that the
product still works after an upstream upgrade. `pi-runtime/compatibility.json`
therefore records the exact Pi capabilities on which Hobot Code depends.

The contract has three separate evidence layers:

1. **Source evidence** binds every required capability to a named Hobot Code
   regression test. This proves the adapter behavior that can run in CI.
2. **Runtime evidence** binds the capability to literal documentation shipped
   inside the pinned Pi archive. Packaging fails when an upstream release
   removes or changes one of those public contracts.
3. **Board evidence** defines end-to-end scenarios that must pass on X5, S100,
   and S600 before a public release. These scenarios exercise the installed
   ARM64 binary and cannot be replaced by source or package validation.

The contract covers the TUI, RPC lifecycle, sessions and branches, compaction,
extensions, resource discovery, providers and models, parallel tools, thinking,
and image prompts. It intentionally describes capabilities that Hobot Code
uses, rather than attempting to mirror every Pi feature.

## Release workflow

Run the source gate before building:

```bash
node scripts/validate-pi-compatibility.mjs --source .
```

The package validator repeats the runtime assertions against the files actually
extracted from the Pi archive. The contract SHA-256 is stored in
`BUILD_INFO.json`; `agentd` exposes it with the Pi version and commit so board
reports can distinguish builds that share a Pi version but were accepted under
different capability contracts.

Before publishing, execute every scenario declared in `boardAcceptance` on all
three required boards and retain a private, sanitized report using schema
`hobot.pi-board-compatibility/v1`. A missing board report is an incomplete
release gate, not a warning that source tests can waive.

`boardAcceptance` also contains the Hobot Code `readiness-diagnostics` and
`install-lifecycle` product scenarios with empty Pi capability lists. This is
deliberate: safe diagnosis and repair, transactional install, failure recovery,
rollback, and uninstall preservation are public product requirements, but they
are not claims about upstream Pi. The contract validator requires both
scenarios and rejects attempts to assign either one a Pi capability.

The `model-egress-runtime` scenario has an executable release gate. On each
board, run it against the extracted candidate package:

```bash
python3 ./verify-model-egress-runtime.py \
  --package-root . \
  --output ../model-egress-acceptance.json \
  --rpc-output ../rpc-background-acceptance.json \
  --session-output ../session-recovery-acceptance.json \
  --extension-output ../extension-safety-acceptance.json \
  --tui-output ../tui-basics-acceptance.json \
  --readiness-output ../readiness-diagnostics-acceptance.json
```

The harness first verifies every packaged file against `MANIFEST.sha256`, then
uses fake credentials and a localhost gateway inside an isolated temporary
home. It exercises the packaged Pi Anthropic Messages, OpenAI Chat
Completions, and OpenAI Responses adapters through `model-only`, verifies the
exact broker route and complete stream lifecycle, and inspects the worker
process tree for credential leakage. It does not read or replace the installed
configuration, runtime, or task history. The report contains only bounded
target/build identifiers, the manifest digest, and pass results, is written with mode `0600`, and
must be retained separately for X5, S100, and S600.

When `--rpc-output` is present, the same isolated package also runs the
`rpc-background` scenario through the real packaged Pi runtime. It requires an
exact correlated approval before one safe workspace write, proves that the
write executes once, sends a second turn and a bounded image over fresh RPC
connections, then creates two multi-turn Side Agents. Both Side Agents must
inherit the settled parent context and point directly to the root task while
the main task remains usable. Raw image bytes are forbidden from persisted
task events.

When `--session-output` is present, the harness also runs `session-recovery`.
The packaged Pi must compact a private persisted session with a measurable net
token reduction while preserving an opaque semantic token, survive forced
termination during a tool call, resume the exact session without replaying the
tool, and create a history-edit branch that excludes the replaced timeline.
The harness uses a new RPC connection for each control operation.

When `--extension-output` is present, the same isolated build must expose the
four packaged RDK extension and Skill declarations without overstating runtime
authority, complete a real parallel extension tool batch and correlated
approval, reject an overlapping Agent workspace write while another turn owns
the write lease, and release that lease after the turn settles.

When `--tui-output` is present, run the entire harness as the ordinary user who
will operate Hobot Code, never as root. The harness attaches a real PTY to the
packaged fullscreen TUI, submits exact Chinese text, requires a structured
thinking stream, edits a draft before sending, detaches the current client, and
reattaches to prove that the same Agent remains usable. It uses a unique,
isolated tmux session and removes it after the bounded test.

When `--readiness-output` is present, the harness also runs
`readiness-diagnostics`. It proves that RPC inspection and `hobot doctor
--json` do not call a model or create a support document, that repairs fail
without explicit confirmation, and that a confirmed repair can only restrict
the declared current-user private runtime paths. An outside-workspace sentinel
must remain byte-for-byte and mode-for-mode unchanged, and no credential,
sentinel content, or temporary path may enter the report.

On RDK images where `/tmp` is not traversable by ordinary users, set `TMPDIR`
to a private `0700` directory owned by that user before running the command.
Do not relax the system `/tmp` permissions for acceptance.

After collecting reports, validate one completed scenario with:

```bash
make board-acceptance-check \
  REPORT_DIR=artifacts/model-egress-candidate \
  SCENARIO=model-egress-runtime \
  REPORT=artifacts/model-egress-matrix.json
```

Omit `SCENARIO` for the public-release gate. The strict matrix verifier then
requires every declared scenario on every required board. It rejects duplicate
reports, unknown fields, public or linked files, mixed product binaries,
different package manifests, a mismatched Pi contract, and a report whose
overall status disagrees with its checks. Missing evidence returns
`incomplete` with exit status 1; it is never upgraded to a warning or pass.

The per-board reports remain private and must never be attached to a release.
The verifier's aggregate `hobot.pi-board-compatibility-matrix/v1` output is a
separate allowlisted document: it contains board model, RDK OS, timestamps,
build hashes, and pass state, but no address, hostname, prompt, command, model
output, path, or credential. Name that aggregate
`hobot-code-<version>-board-acceptance.json` and attach only it to the draft
release.

Tag builds stop at a draft. The protected `Promote Release` workflow parses the
aggregate again, validates the downloaded package and archive, and requires a
clean exact-tag build whose agentd, manifest, and compatibility hashes match
the matrix. Every board timestamp must follow the build, be no more than seven
days old, and stay within a five-minute clock-skew allowance. The workflow then
attests deterministic public release evidence and is the only automated path
that can make the draft visible.

When upgrading Pi:

1. update `pi.lock` and the compatibility contract together;
2. review the new upstream runtime documentation and extension lifecycle;
3. update adapters only where the upstream contract genuinely changed;
4. run source and package checks;
5. run the complete board acceptance matrix;
6. retain the old release and contract digest as the rollback baseline.

Assertions use bounded literal text rather than regular expressions or code.
The compatibility file is release data: it cannot execute commands, download
content, or grant permissions.
