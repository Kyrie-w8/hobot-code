# EdgeAgent Runtime

EdgeAgent is a small, policy-aware Agent runtime for D-Robotics X5, S100, and
S600 boards. The same core runs on every board; hardware differences live in
configuration overlays and tool/provider adapters.

## P1 status

Implemented:

- Python 3.9+ core, tested against the boards' Python 3.10 and 3.12 runtimes;
  compatible with their existing PyYAML 5.4.1/6.0.1 installations.
- OpenAI Responses, OpenAI-compatible, Anthropic Messages, and current Gemini
  Interactions provider mappings.
- Provider-independent messages, function calls, results, usage, and state.
- Bounded Agent tool loop with step limits, timeouts, argument validation,
  board capability checks, allowlists, and approval gates.
- Layered system prompts, lazy Skills, SQLite sessions/events, and JSONL-ready
  SFT trajectory export.
- Read-only `system.snapshot` tool and opt-in root-bounded text-file tool.
- Offline mock provider, diagnostics, CLI, board profiles, and tests.

Not yet implemented:

- Streaming/SSE output, long-running daemon/API, MCP transport, remote node RPC,
  out-of-process plugin isolation, and OTA management.
- A generic BPU LLM provider. `hrt_model_exec` exists on all boards, but each
  model still requires a supported conversion and measured runtime validation.
- Live paid-provider tests. Protocol builders and parsers use deterministic tests;
  real API tests require user-supplied keys and chosen model IDs.

## Quick start

The default configuration is offline and does not require an API key:

```bash
python3 -m edge_agent diagnose \
  --config config/base.yaml \
  --board config/boards/x5.yaml

python3 -m edge_agent run \
  --config config/base.yaml \
  --board config/boards/x5.yaml \
  --message "介绍当前节点" \
  --json
```

Install the CLI in a virtual environment when packaging it for a board:

```bash
python3 -m venv .venv
.venv/bin/pip install -e .
.venv/bin/edge-agent diagnose --board config/boards/s600.yaml
```

## Model providers

Use an Agent config and optionally a board overlay:

```bash
OPENAI_API_KEY=... edge-agent run \
  --config config/agents/openai-responses.yaml \
  --board config/boards/s600.yaml \
  --message "读取板卡状态"
```

Available provider configs:

| Config | API path | Typical use |
|---|---|---|
| `openai-responses.yaml` | `/v1/responses` | Native OpenAI tool workflows |
| `openai-compatible.yaml` | `/v1/chat/completions` | DeepSeek, Qwen, GLM, vLLM, llama.cpp servers |
| `anthropic.yaml` | `/v1/messages` | Anthropic-native tools |
| `gemini.yaml` | `/v1beta/interactions` | Current Gemini steps/tool workflow |

Set model IDs through `OPENAI_MODEL`, `MODEL_NAME`, `ANTHROPIC_MODEL`, or
`GEMINI_MODEL`. Secrets are expanded from environment variables and are never
written into the SQLite trajectory.

## Security model

- There is no generic shell tool in P1.
- A tool must be registered, exposed for the active board, allowed by policy,
  schema-valid, and approved when required.
- Write and dangerous tools require approval unless deployment policy explicitly
  enables them.
- File reads are disabled until `security.filesystem_roots` is configured, and
  resolved paths must remain under those roots.
- Agent reasoning is control-plane logic. Safety-critical or hard real-time loops
  must stay in deterministic device services.

## Skills and trajectories

Skills use `skills/<name>/SKILL.md` with YAML frontmatter. Only selected Skills
load their full instructions. Required tools and board profiles are checked before
prompt composition.

Export one complete session as one training trajectory:

```bash
edge-agent export --config config/base.yaml --session SESSION_ID > trajectory.jsonl
```

The export preserves assistant tool calls, tool results, and the exposed tool
schemas so training serialization can match deployment.

## Verification

```bash
PYTHONPYCACHEPREFIX=/tmp/edge-agent-pycache python3 -m pytest
```

See [architecture](docs/architecture.md) and [configuration](docs/configuration.md)
for extension contracts and the next implementation phase.
