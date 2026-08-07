# Architecture

## Runtime boundary

`AgentRuntime` owns the bounded reasoning/tool loop but has no vendor or board
logic. It receives six replaceable services:

1. `ModelProvider` normalizes a vendor request and response.
2. `ToolRegistry` exposes typed functions and executes them with a timeout.
3. `PolicyEngine` decides allow, deny, and approval requirements.
4. `PromptComposer` merges policy, board, role, and selected Skill layers.
5. `SkillCatalog` validates and lazily loads instruction bundles.
6. `SessionStore` persists canonical messages and audit events.

The canonical flow is:

```text
user -> canonical history -> provider -> assistant/tool calls
     -> schema + policy -> local tool -> canonical tool result
     -> provider -> final assistant response -> SQLite trajectory
```

## Provider contract

Providers consume `ProviderRequest` and return `ProviderResponse`. Tool definitions
are canonical JSON Schema objects. Provider-specific state is opaque to the Agent
and carried between tool-loop steps:

- OpenAI Responses retains response output items for stateless reasoning/tool
  continuation or uses `previous_response_id` in stored mode.
- Gemini Interactions retains all returned steps in stateless mode or uses
  `previous_interaction_id` in stored mode.
- Chat Completions and Anthropic reconstruct requests from canonical messages.

The P1 HTTP transport is intentionally small and non-streaming. P2 adds an event
stream interface without changing the canonical completed-response contract.

## Board profiles

Profiles are configuration overlays, not forks. A profile defines observed
hardware, Python/runtime constraints, node role, and a board prompt. A tool can
also list supported profiles. BPU presence is capability metadata only; a model
becomes routable after conversion, accuracy, memory, latency, and thermal checks.

## Extension plan

P2 extension points keep the current core stable:

- Provider: llama.cpp streaming and validated D-Robotics HBRT model packages.
- Tool: GPIO, I2C, SPI, UART, CAN, V4L2, ALSA, ROS2/TROS, and MCP clients.
- Transport: local HTTP/WebSocket daemon and authenticated node-to-node RPC.
- Plugin: signed manifest plus process-isolated JSON-RPC workers.
- Operations: systemd service, health endpoint, metrics, offline wheelhouse, and
  staged updates with rollback.
