# Configuration

Configuration is YAML or JSON. `extends` accepts one relative path or a list of
paths. Parent documents merge left to right, then the current file wins. A board
overlay is applied last.

Environment syntax:

```text
${REQUIRED_VARIABLE}
${OPTIONAL_VARIABLE:-default}
```

Important sections:

| Section | Purpose |
|---|---|
| `runtime` | Agent step and tool timeout limits |
| `board` | Hardware profile, observed resources, and board prompt |
| `agent` | Name, role prompt, and enabled Skills |
| `model` | Provider, model ID, endpoint, key, timeout, provider settings |
| `memory` | SQLite path |
| `prompts` | Ordered prompt layers from paths or inline text |
| `skills` | Skill search roots |
| `security` | Tool allow/deny/approval patterns and filesystem roots |

Prompt order uses ascending priority. The supplied convention is policy `10`,
board `20`, Agent role `30`, and selected Skills `50`. Skill instructions therefore
cannot remove enforcement performed by `PolicyEngine`.

The machine-readable structural contract is in
[`config/schema/edge-agent.schema.json`](../config/schema/edge-agent.schema.json).
The runtime also performs dependency-free validation for required fields and
positive limits.
