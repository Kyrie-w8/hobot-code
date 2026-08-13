# Compatibility matrix

Hobot Studio evaluates compatibility during the board connection handshake. A connection can be `supported`, `limited`, or `upgrade-required`. The default Studio view translates that internal state into a user outcome: whether daily Agent work is available, whether hardware-specific production evidence is trusted, and the single most useful next action. Exact versions, protocol numbers, event schema, target identity, and individual issues remain available under **Technical details**.

## Hard requirements

Studio refuses the connection when any of these conditions is true:

- Protocol 1 is outside the board service's advertised protocol range.
- The board event schema is older than schema 2.
- The board service is missing task lifecycle, task paging, or event paging.

The failure includes the detected Studio, agentd, protocol, and event versions plus an upgrade action. The board client is closed instead of remaining in a partially usable state.

## Feature degradation

The board remains usable, but Studio reports `limited`, when a board service is missing model capability negotiation, model route health checks, hardware telemetry, support bundles, deployment workflows, side agents, persistent task queuing, structured task failures, structured event items, workspace browsing, read-only workspace change review, isolated Git task workspaces, cross-Agent workspace write leases, or verifiable build identity. Studio labels this case **Update recommended** and states that core Agent work is still available. Event schema 2 is also accepted with a warning because it cannot expose every normalized activity detail; schema 3 remains readable, while schema 4 plus `events.items.v1` adds stable item lifecycle and bounded tool previews.

Studio and agentd should use the same major and minor Hobot Code release line. A patch-version difference is accepted; a major or minor difference is reported as limited until the versions are aligned.

From 0.26.0 onward, `build.identity.v1` binds the running `agentd` SHA-256 to its packaged source commit, clean/dirty flag, build time, target, and Pi runtime. A dirty, missing, or mismatched identity is reported as limited even when the semantic version matches. Product version equality alone is not release equivalence.

The repository replays recorded handshakes for the current release, the previous minor release, and the minimum readable event schema. These fixtures lock the SDK decoding and Studio user outcome for supported, degraded, and minimum-capability boards; they do not replace live installation and recovery tests.

## Validated board targets

| Board | Expected RDK OS line | Validation baseline | Validated releases |
| --- | --- | --- | --- |
| RDK X5 | 3.x | 3.5.0 | 3.5.0 |
| RDK S100 | 4.x | 4.0.5 | 4.0.5, 4.0.5-Beta |
| RDK S600 | 5.x | 5.1.0 | 5.1.0 |

Validation uses the reported release identifier, including pre-release suffixes. A known image such as S100 `4.0.5-Beta` is accepted because it is listed explicitly. An unlisted beta, release candidate, patch, or minor release on the expected major line remains usable and Studio explicitly says **Daily Agent work is available**, but labels its hardware confidence **Hardware unverified**. The warning directs users to save a support bundle and verify board-specific workflows before production use. A different major line or an unknown board also remains connectable for general development, while hardware-specific deployment workflows carry an explicit warning.

This matrix records Hobot Code validation scope. It does not replace D-Robotics release notes or prove that every peripheral, sensor, model, or third-party package is compatible.
