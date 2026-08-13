# Compatibility matrix

Hobot Studio evaluates compatibility during the board connection handshake. A connection can be `supported`, `limited`, or `upgrade-required`.

## Hard requirements

Studio refuses the connection when any of these conditions is true:

- Protocol 1 is outside the board service's advertised protocol range.
- The board event schema is older than schema 2.
- The board service is missing task lifecycle, task paging, or event paging.

The failure includes the detected Studio, agentd, protocol, and event versions plus an upgrade action. The board client is closed instead of remaining in a partially usable state.

## Feature degradation

The board remains usable, but Studio reports `limited`, when a board service is missing model capability negotiation, model route health checks, hardware telemetry, support bundles, deployment workflows, side agents, or workspace browsing. Event schema 2 is also accepted with a warning because it cannot expose every normalized activity detail.

Studio and agentd should use the same major and minor Hobot Code release line. A patch-version difference is accepted; a major or minor difference is reported as limited until the versions are aligned.

## Validated board targets

| Board | Expected RDK OS line | Validation baseline | Validated releases |
| --- | --- | --- | --- |
| RDK X5 | 3.x | 3.5.0 | 3.5.0 |
| RDK S100 | 4.x | 4.0.5 | 4.0.5, 4.0.5-Beta |
| RDK S600 | 5.x | 5.1.0 | 5.1.0 |

Validation uses the reported release identifier, including pre-release suffixes. A known image such as S100 `4.0.5-Beta` is accepted only because it is listed explicitly; an unlisted beta, release candidate, patch, or minor release on the expected major line remains usable but is marked as not fully validated. The warning directs users to save a support bundle and verify board-specific workflows before production use. A different major line or an unknown board also remains connectable for general development, while hardware-specific deployment workflows carry an explicit warning.

This matrix records Hobot Code validation scope. It does not replace D-Robotics release notes or prove that every peripheral, sensor, model, or third-party package is compatible.
