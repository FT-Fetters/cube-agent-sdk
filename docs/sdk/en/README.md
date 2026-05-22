# Cube Agent SDK

Cube Agent SDK is a small Go SDK for building agents with managed conversation
state, tools, approval checks, streaming, MCP integration, session snapshots,
hooks, observers, skills, compaction, and subagents.

The SDK keeps provider credentials, external process deployment, human approval
UI, durable storage, and telemetry exporters outside the core runtime.
Applications connect those pieces through Go interfaces and options.

## Documentation Map

- [Getting Started](./getting-started.md): install, test, and run a minimal agent.
- [Agent Runtime](./agent-runtime.md): how `Agent` manages context and model runs.
- [Models](./models.md): built-in adapters and custom model implementations.
- [Tools](./tools.md): local functions, schemas, validation, and tool risks.
- [Streaming](./streaming.md): incremental assistant output with `RunStream`.
- [MCP](./mcp.md): MCP server metadata and stdio, HTTP, and SSE tool bridging.
- [Sessions](./sessions.md): reset, snapshot, restore, and fork.
- [Approvals](./approvals.md): built-in policies and production approval flows.
- [Observability](./observability.md): hooks, observers, stable telemetry names,
  and sanitized telemetry.
- [Errors](./errors.md): sentinel errors and structured `AgentError` handling.
- [Skills](./skills.md): reusable instruction bundles and activation paths.
- [Compaction](./compaction.md): context threshold checks and summarization.
- [Subagents](./subagents.md): parent-controlled child agents.
- [Examples](./examples.md): local examples and the optional live API example.
- [Production](./production.md): integration and production observability
  checklist for real deployments.
- [API Reference](./api-reference.md): exported SDK surface grouped by area.

## What the SDK Provides

- Prompt assembly, message history, and active skill injection.
- Model and streaming model interfaces.
- OpenAI-compatible chat completions, OpenAI Responses, and Anthropic Messages
  adapters with provider-native streaming support.
- Tool descriptors, tool argument schemas, and preflight validation.
- MCP stdio, HTTP, and SSE clients with MCP-to-tool bridging.
- Session reset, snapshot, restore, and fork operations.
- Approval policies, hooks, observers, and sanitized lifecycle metadata.
- Compaction and subagent orchestration primitives.

## What Applications Provide

- Provider accounts, model IDs, base URLs, API keys, retry policy, and network
  controls.
- MCP server binaries or URLs, deployment, supervision, and runtime permissions.
- Human approval UI or business policy integration.
- Durable storage adapters, encryption, access control, retention, and migration
  for snapshots, session records, and event logs.
- Log, metrics, and trace exporters.
- Secret management, rate limits, rollout strategy, and production monitoring.
