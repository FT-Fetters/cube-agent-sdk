# Cube Agent SDK

A small Go SDK for building agents with managed conversation state, tools,
approval checks, streaming, MCP integration, session snapshots, hooks, observers,
skills, compaction, subagents, and eval/replay test helpers.

The SDK keeps provider credentials, external process deployment, approval UI,
durable storage, and telemetry exporters outside the core runtime. Applications
plug those pieces in through Go interfaces and options.

## Install

```bash
go get github.com/cubence/cube-agent-sdk
```

The module has no third-party Go dependencies.

## Quick Start

Run the tests and compile every example:

```bash
go test ./...
```

Run one local example without a real model provider:

```bash
go run ./examples/tool_schema
```

Minimal agent setup:

```go
package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/cubence/cube-agent-sdk"
)

type model struct{}

func (model) Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error) {
	return agent.ModelResponse{
		Message: agent.Message{Role: agent.RoleAssistant, Content: "ok"},
	}, nil
}

func main() {
	bot, err := agent.New(agent.Config{
		SystemPrompt: "You are a focused coding agent.",
	}, model{})
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(context.Background(), "Say hello.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)
}
```

The `Agent` owns prompt assembly, message history, active skills, tool
descriptors, approval checks, lifecycle hooks, observer notifications,
compaction, MCP server metadata, and subagent registries.

## Full Documentation

Complete SDK documentation is maintained under `docs/sdk`:

- [English](docs/sdk/en/README.md)
- [中文](docs/sdk/zh/README.md)

Production observability:

- [English](docs/sdk/en/production.md#production-observability-checklist)
- [中文](docs/sdk/zh/production.md#生产观测清单)

## Examples

Local examples are under `examples/` and avoid real credentials or external
services:

- `go run ./examples/openai_compatible`
- `go run ./examples/model_factory`
- `go run ./examples/tool_schema`
- `go run ./examples/streaming`
- `go run ./examples/mcp_stdio`
- `go run ./examples/session_state`
- `go run ./examples/approval_observer`

The OpenTelemetry example is a separate Go module so tracing dependencies stay
out of the core SDK module. It maps observations to the SDK's stable `agent.*`
telemetry attributes without adding OpenTelemetry to the core dependency graph:

- `go -C examples/opentelemetry test ./...`
- `go -C examples/opentelemetry run .`

`examples/live_api` is the only example intended for a real provider endpoint.
It reads credentials from environment variables and is not required by CI:

```bash
MODEL_API_TYPE=anthropic-messages \
MODEL_BASE_URL=https://api.anthropic.com \
MODEL_API_KEY="<your-api-key>" \
MODEL_NAME=claude-sonnet-4-6 \
go run ./examples/live_api
```

Use `MODEL_API_TYPE=openai-compatible` with an OpenAI-compatible
`MODEL_BASE_URL` to run the same example against a chat completions endpoint,
or `MODEL_API_TYPE=openai-responses` with `MODEL_BASE_URL=https://api.openai.com`
to use OpenAI's Responses API.

## Optional Live API Tests

The default test suite uses local fakes and `httptest` servers. To exercise a
real provider, provide a complete model configuration in the process environment
or a root `.env` file:

```bash
MODEL_API_TYPE=anthropic-messages
MODEL_BASE_URL=https://api.anthropic.com
MODEL_API_KEY=<your-api-key>
MODEL_NAME=claude-sonnet-4-6
```

When these variables are present in the process environment or root .env as a
complete configuration, the live API test runs automatically. When any required
variable is missing, it is skipped. Do not commit real credentials; keep local
`.env` files out of version control.

Run the live test with verbose output:

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

Run any single test with verbose output by replacing the test name:

```bash
go test -v -run '^TestName$' ./...
```

## SDK Responsibilities

The SDK provides:

- Runtime abstractions: `Agent`, `Model`, `StreamModel`, `Tool`,
  `ApprovalPolicy`, `Hook`, `Observer`, `Compactor`, and session APIs.
- OpenAI-compatible chat completions adapter with streaming support.
- OpenAI Responses API adapter with streaming support.
- Tool descriptor and JSON Schema subset for model-facing function calling.
- Tool argument validation before local tool execution.
- MCP stdio, HTTP, and SSE clients with MCP-to-`Tool` bridging.
- Session snapshot, restore, reset, and fork APIs.
- Approval policy helpers with tool-name and risk allowlists.
- Sanitized observations and structured lifecycle events.
- Composable model reliability wrappers for retry, timeout, rate, circuit, and
  budget controls.
- Stable telemetry attribute and metric label names for logs, metrics, and
  traces.
- Public scripted model, transcript, replay, and assertion helpers for
  deterministic agent evals in Go tests.
- Custom request ID generation from safe lifecycle metadata for application
  tracing and logging conventions.
- Structured `AgentError` values and sentinel errors.

Applications provide:

- Real model provider credentials, model IDs, and base URLs.
- External MCP server binaries or URLs, runtime configuration, and deployment.
- Human approval UI or business policy integration.
- Durable storage, encryption, retention policy, and migration strategy for
  session snapshots.
- Telemetry exporters, log sinks, metrics labels, and tracing correlation.
- Secret management, network controls, rate limiting, and production rollout.

## Built-In Model API Types

Use `NewModel` when you want application code to choose a provider wire
protocol through configuration. The SDK currently includes:

- `ModelAPIOpenAICompatible` for `/chat/completions` endpoints.
- `ModelAPIOpenAIResponses` for OpenAI Responses-style `/v1/responses`
  endpoints.
- `ModelAPIAnthropicMessages` for Anthropic Messages-style `/v1/messages`
  endpoints.

```go
model, err := agent.NewModel(agent.ModelConfig{
	APIType: agent.ModelAPIAnthropicMessages,
	BaseURL: os.Getenv("MODEL_BASE_URL"),
	APIKey:  os.Getenv("MODEL_API_KEY"),
	Model:   os.Getenv("MODEL_NAME"),
})
if err != nil {
	return err
}
```

Switch protocols by changing `APIType` and the provider settings:

```go
model, err := agent.NewModel(agent.ModelConfig{
	APIType: agent.ModelAPIOpenAICompatible,
	BaseURL: "https://api.openai.com/v1",
	APIKey:  os.Getenv("OPENAI_API_KEY"),
	Model:   "gpt-4.1",
})
```

`NewModel` is a convenience factory. Provider-specific constructors remain
available when an application wants protocol-specific fields or clearer wiring.


## Provider capability matrix

Applications can inspect provider protocol support with `CapabilitiesOf(model)` before selecting a model:

```go
required := agent.ModelCapabilityRequirement{Tools: true, Streaming: true}
model, caps, ok := agent.SelectModelByCapabilities(candidates, required)
if !ok {
	return fmt.Errorf("no configured model supports tools and streaming")
}
_ = caps
_ = model
```

Built-in declarations are protocol-level adapter support, not a guarantee for
every remote model variant behind the same endpoint.

| Provider API | Tools | Streaming | JSON mode | Structured output | Reasoning metadata | Parallel tool calls | MCP metadata | Model-handled MCP | Token usage |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `ModelAPIOpenAICompatible` | Yes | Yes | No | No | No | Yes | No | No | Yes |
| `ModelAPIOpenAIResponses` | Yes | Yes | No | No | Yes | Yes | No | No | Yes |
| `ModelAPIAnthropicMessages` | Yes | Yes | No | No | Yes | Yes | No | No | Yes |

When a model declares capabilities, the agent rejects obvious incompatible
configuration before provider calls. The error is compatible with
`errors.Is(err, agent.ErrCapabilityMismatch)` and `errors.As` into
`*agent.CapabilityMismatchError`. Custom models without capability declarations
keep the previous permissive behavior.

## OpenAI Responses Models

Use `NewOpenAIResponsesModel` or `NewModel` with
`ModelAPIOpenAIResponses` for OpenAI's Responses API. `BaseURL` can be an API
root, a `/v1` URL, or a full URL ending in `/v1/responses`.

```go
model, err := agent.NewOpenAIResponsesModel(agent.OpenAIResponsesConfig{
	BaseURL: os.Getenv("OPENAI_BASE_URL"),
	APIKey:  os.Getenv("OPENAI_API_KEY"),
	Model:   os.Getenv("OPENAI_RESPONSES_MODEL"),
})
if err != nil {
	return err
}
```

The adapter maps the SDK system prompt to Responses `instructions`, maps tools
to Responses function tools, maps SDK tool calls to `function_call` items, and
maps SDK tool results to `function_call_output` items. It preserves raw
Responses output metadata on assistant messages so multi-round tool loops can
replay reasoning and function-call context without using server-side response
state. It also supports `RunStream` using Responses semantic streaming events.
Set `MaxTokens` to send `max_output_tokens`, and set `Store` when your
application needs explicit control over OpenAI's response storage setting.

## OpenAI-Compatible Models

Use `NewOpenAICompatibleModel` for providers that expose the standard
`/chat/completions` request and response shape. `BaseURL` can be a provider root
or a full URL ending in `/chat/completions`.

```go
model, err := agent.NewOpenAICompatibleModel(agent.OpenAICompatibleConfig{
	BaseURL: os.Getenv("OPENAI_COMPATIBLE_BASE_URL"),
	APIKey:  os.Getenv("OPENAI_API_KEY"),
	Model:   os.Getenv("OPENAI_COMPATIBLE_MODEL"),
})
if err != nil {
	return err
}

bot, err := agent.New(agent.Config{
	SystemPrompt: "You are a concise assistant.",
}, model)
```

The adapter maps SDK messages, tool descriptors, and tool calls to the
OpenAI-compatible wire format. It also supports `RunStream` using chat
completion SSE chunks and requests final stream usage when the provider supports
`stream_options.include_usage`. It does not manage provider accounts, API keys,
retries, rate limits, or network policy. Provide a custom `HTTPClient` when your
application needs timeouts, proxies, tracing, or transport controls.

## Anthropic Messages

Use `NewAnthropicMessagesModel` or `NewModel` with
`ModelAPIAnthropicMessages` for providers that expose the Anthropic Messages
request and response shape. `BaseURL` can be a provider root, a `/v1` URL, or a
full `/v1/messages` URL.

```go
model, err := agent.NewModel(agent.ModelConfig{
	APIType: agent.ModelAPIAnthropicMessages,
	BaseURL: os.Getenv("ANTHROPIC_BASE_URL"),
	APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
	Model:   os.Getenv("ANTHROPIC_MODEL"),
	Thinking: &agent.AnthropicThinkingConfig{
		Type:    "adaptive",
		Display: "summarized",
	},
})
if err != nil {
	return err
}
```

The adapter maps the SDK system prompt to the top-level `system` field, maps
tools to Anthropic `tools` with `input_schema`, maps SDK tool calls to
`tool_use` content blocks, and maps SDK tool results to `tool_result` user
content blocks. It preserves raw Anthropic content blocks on assistant message
metadata, including `thinking` and `redacted_thinking`, so tool-use loops can
replay signed thinking context back to the provider. It also supports
`RunStream` using Anthropic Messages SSE events. The default Anthropic API
version is `2023-06-01`; set `AnthropicVersion` in `ModelConfig` or
`AnthropicMessagesConfig` if your provider requires another version. `Thinking`
is optional; omit it when the target model or provider should run without
extended/adaptive thinking.

## Tool Schema

Tools are local Go functions that models can request. Attach a schema to make
the model-facing descriptor explicit and to let the SDK validate arguments
before the function runs.

```go
lookup := agent.ToolFunc{
	ToolName:        "lookup_account",
	ToolDescription: "Read account status",
	ToolRisk:        agent.ToolRiskRead,
	Parameters: &agent.ToolParametersSchema{
		Type:     agent.SchemaTypeObject,
		Required: []string{"account_id"},
		Properties: map[string]agent.ToolParametersSchema{
			"account_id": {
				Type:        agent.SchemaTypeString,
				Description: "Application account identifier",
			},
		},
	},
	Fn: func(ctx context.Context, call agent.ToolCall) (agent.ToolResult, error) {
		accountID, _ := call.Arguments["account_id"].(string)
		return agent.ToolResult{
			CallID:  call.ID,
			Name:    call.Name,
			Content: "account " + accountID + " is active",
		}, nil
	},
}
```

Schema validation rejects missing, wrong-typed, or constraint-violating
arguments with `ErrToolValidation`. Validation errors include the parameter path
but not the rejected value. The supported subset intentionally stays small:
`enum`, `default`, numeric `minimum`/`maximum`, string `minLength`/`maxLength`,
array `minItems`/`maxItems`, `pattern`, and boolean `additionalProperties`.
Defaults are emitted to providers but are not injected into tool arguments.

You can also generate the schema from exported struct fields:

```go
type LookupArgs struct {
	AccountID string   `json:"account_id" description:"Application account identifier" required:"true" pattern:"^acct_[a-z0-9]+$"`
	Tier      string   `json:"tier,omitempty" enum:"free,pro,enterprise" default:"pro"`
	Limit     int      `json:"limit,omitempty" min:"1" max:"50" default:"10"`
	Tags      []string `json:"tags,omitempty" minItems:"1" maxItems:"5"`
}

parameters, err := agent.ToolParametersSchemaFromStruct(LookupArgs{})
if err != nil {
	return err
}
```

The generator supports `json`, `description`, `required`, `enum`, `default`,
`min`, `max`, `minLength`, `maxLength`, `minItems`, `maxItems`, `pattern`, and
`additionalProperties` tags on exported fields. It supports nested structs,
pointers, slices, arrays, primitive scalar types, and `json:"-"`; maps,
interfaces, functions, and channels are outside this lightweight subset. The
SDK validates the call and executes the function, but the application owns the
business logic, data access, side effects, and result content.

## Streaming

Models that support text deltas implement `StreamModel` in addition to `Model`.
The built-in OpenAI-compatible, OpenAI Responses, and Anthropic Messages
adapters implement `StreamModel`, so they can be passed directly to
`RunStream`. `RunStream` returns a channel of `StreamEvent` values. Anthropic
thinking and OpenAI Responses reasoning deltas are emitted as
`StreamEventThinkingDelta` when providers stream them.

```go
events, err := bot.RunStream(ctx, "Write a short summary.")
if err != nil {
	if errors.Is(err, agent.ErrStreamingUnsupported) {
		// Fall back to Run or choose a streaming-capable adapter.
	}
	return err
}

for event := range events {
	switch event.Type {
	case agent.StreamEventDelta:
		fmt.Print(event.Delta)
	case agent.StreamEventThinkingDelta:
		fmt.Printf("thinking: %s", event.Delta)
	case agent.StreamEventDone:
		fmt.Println(event.Message.Content)
	case agent.StreamEventError:
		return event.Error
	}
}
```

The SDK commits the final assistant message only after a done event. Interrupted
delta streams do not persist partial assistant text, and thinking deltas are not
appended to final assistant content. Final done events carry provider token usage
when the stream format reports it, and streaming observability copies that usage
into `EventAfterModel`/`Observation.TokenUsage`.
Built-in providers normalize supported streamed tool-call shapes into final done
messages so `RunStream` can execute them and continue the tool loop.

## Reliable Model Wrappers

Wrap any `Model` with `NewReliableModel` to add local reliability controls
without changing the `Agent` API. If the wrapped model implements `StreamModel`,
the returned wrapper also supports streaming. Streaming retries only cover stream
startup; once deltas begin, the wrapper does not replay the stream.

```go
model := agent.NewReliableModel(baseModel,
	agent.WithReliableMaxAttempts(3),
	agent.WithReliablePerAttemptTimeout(15*time.Second),
	agent.WithReliableTotalTimeout(45*time.Second),
	agent.WithReliableBackoff(func(attempt int) time.Duration {
		return time.Duration(attempt) * time.Second
	}),
	agent.WithReliableRateLimit(60, time.Minute),
	agent.WithReliableCircuitBreaker(5, time.Minute),
	agent.WithReliableTokenBudget(200_000),
	agent.WithReliableCostBudget(25, 0.01, 0.03),
	agent.WithReliabilityObserver(func(ctx context.Context, event agent.ReliabilityEvent) {
		// Export only event fields; they do not contain prompts or raw errors.
	}),
)
```

By default, retries are limited to safe retryable model failures such as
timeouts, HTTP 408/429, and 5xx provider diagnostics or subcategories. Bad
requests, auth failures, validation errors, approval errors, tool errors, and
other non-retryable categories are not retried by default.

## MCP Stdio, HTTP, and SSE

There are two MCP integration paths.

First, attach MCP server configuration to model requests when your model adapter
or provider handles MCP:

```go
bot, err := agent.New(cfg, model, agent.WithMCPServers(agent.MCPServerConfig{
	Name:      "filesystem",
	Command:   "mcp-filesystem",
	Args:      []string{"--root", "."},
	Env:       map[string]string{"MODE": "readonly"},
	Transport: agent.MCPTransportStdio,
}))
```

Second, run an MCP server as SDK tools. `StartMCPClient` selects stdio, HTTP,
or SSE from `MCPServerConfig.Transport`:

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "filesystem",
	Command:   os.Getenv("MCP_FILESYSTEM_COMMAND"),
	Args:      []string{"--root", os.Getenv("MCP_FILESYSTEM_ROOT")},
	Transport: agent.MCPTransportStdio,
})
if err != nil {
	return err
}
defer client.Close()

tools, err := client.Tools(ctx)
if err != nil {
	return err
}

bot, err := agent.New(cfg, model,
	agent.WithTools(tools...),
	agent.WithApprovalPolicy(agent.AllowToolsApproval("read_file")),
)
```

HTTP MCP servers use JSON-RPC POSTs to `URL`:

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "remote-tools",
	URL:       "https://mcp.example.com/rpc",
	Transport: agent.MCPTransportHTTP,
})
```

SSE MCP servers connect to `URL`, read an `endpoint` event, and then send
JSON-RPC requests to that discovered HTTP endpoint:

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "remote-tools",
	URL:       "https://mcp.example.com/events",
	Transport: agent.MCPTransportSSE,
})
```

All SDK-managed MCP clients perform initialize, support `tools/list` pagination,
map MCP schemas to SDK tool schemas, call `tools/call`, expose `Tools(ctx)`,
refresh tool descriptors with `RefreshTools(ctx)`, probe health with
`Health(ctx)`, and clean up with `Close()`. HTTP and SSE startup, health, and
list calls use a short retry/backoff window for transient network failures,
HTTP 408/429, and 5xx responses; tool calls are not retried to avoid duplicating
side effects.

Diagnostics and errors avoid MCP environment values, URL query strings, raw HTTP
response bodies, tool arguments, and tool results. Applications must still
supply the real server binary or URL, credentials, environment, filesystem or
network permissions, process supervision policy, and approval UX.

## Session State

Use snapshots when you need to persist or branch an agent session.

```go
snapshot := bot.Snapshot()
payload, err := json.Marshal(snapshot)
if err != nil {
	return err
}

var restored agent.SessionSnapshot
if err := json.Unmarshal(payload, &restored); err != nil {
	return err
}

next, err := agent.New(cfg, model)
if err != nil {
	return err
}
if err := next.Restore(restored); err != nil {
	return err
}

branch, err := next.Fork("what-if")
```

`Snapshot` copies the managed conversation context. `Restore` validates the
snapshot schema and replaces only that context, preserving the target agent's
configured model and capabilities. `Fork` creates an independent agent with
copied context and copied capability registries.

For durable persistence, implement `SessionStore` against Redis, Postgres, S3,
or a file store, or use `NewMemorySessionStore` for tests and local examples.
Session records include `schema_version`, an optimistic `version`, timestamps,
safe string metadata, and structured migration errors.

```go
store := agent.NewMemorySessionStore()

record := agent.NewSessionRecord("session-123", snapshot)
record.Metadata = map[string]string{"tenant": "acme"}
saved, err := store.SaveSession(ctx, record)
if err != nil {
	return err
}

loaded, err := store.LoadSession(ctx, saved.ID)
if err != nil {
	return err
}
if err := next.Restore(loaded.Snapshot); err != nil {
	return err
}
```

`SessionEventLog` is an append-only companion interface for safe lifecycle
metadata. Events have stable IDs, monotonic sequences, schema metadata, run IDs,
and caller-approved string metadata only.

```go
_, err = store.AppendSessionEvent(ctx, agent.SessionEvent{
	SessionID: saved.ID,
	Type:      agent.SessionEventRunStarted,
	RunID:     "run-1",
	Metadata:  map[string]string{"agent_id": next.ID()},
})
```

The SDK serializes snapshot and record shapes; applications own durable storage,
encryption, access control, retention, database migrations, and any adapters for
Redis, Postgres, S3, or files. Do not put provider credentials, runtime config,
prompts beyond normal session messages, tool payloads, or raw telemetry in event
metadata.

## Approval Policies

Every tool call passes through an `ApprovalPolicy`. The default is
`AllowAllApproval` for compatibility, but production agents should install an
explicit policy.

```go
policy := agent.RequireAllApprovals(
	agent.AllowToolsApproval("lookup_account"),
	agent.AllowRisksApproval(agent.ToolRiskRead),
)

bot, err := agent.New(cfg, model,
	agent.WithTools(lookup),
	agent.WithApprovalPolicy(policy),
)
```

Useful helpers:

- `DenyAllApproval` rejects every tool call.
- `AllowToolsApproval` approves only named tools.
- `DenyToolsApproval` blocks selected tools and approves the rest.
- `AllowRisksApproval` approves only selected risk classes.
- `RequireAllApprovals` composes policies with AND semantics.
- `ApprovalFunc` adapts application logic, such as a human approval prompt.

Denied tools return an error compatible with `errors.Is(err,
agent.ErrApprovalDenied)`. Approval events and observations include approval
result, reason, tool name, and risk. They do not expose tool arguments.

## Observability

Use hooks when the application may reject an operation, and observers when
telemetry must never alter execution.

```go
hook := func(ctx context.Context, event agent.Event) error {
	if event.Type == agent.EventBeforeTool && event.ToolRisk == agent.ToolRiskDestructive {
		return fmt.Errorf("destructive tools require a separate workflow")
	}
	return nil
}

observer := agent.ObserverFunc(func(ctx context.Context, observation agent.Observation) {
	log.Printf("type=%s request=%s parent=%s round=%d failed=%v",
		observation.Type,
		observation.RequestID,
		observation.ParentRequestID,
		observation.Round,
		observation.Failed,
	)
	if observation.Type == agent.EventAfterTool {
		timing := observation.ToolTiming
		log.Printf("tool_timing validation=%s approval=%s execution=%s",
			timing.Validation,
			timing.Approval,
			timing.Execution,
		)
	}
})

bot, err := agent.New(cfg, model,
	agent.WithHook(hook),
	agent.WithObserver(observer),
)
```

For standard-library structured logs, opt in with `NewSlogObserver`:

```go
slogObserver := agent.NewSlogObserver(agent.SlogObserverOptions{
	Logger: slog.Default(),
})

bot, err := agent.New(cfg, model,
	agent.WithObserver(slogObserver),
)
```

For metrics, implement `MetricSink` in your application and opt in with
`NewMetricsObserver`:

```go
metricsObserver := agent.NewMetricsObserver(agent.MetricsObserverOptions{
	Sink: appMetricSink{},
})

bot, err := agent.New(cfg, model,
	agent.WithObserver(metricsObserver),
)
```

To send the same sanitized observation to multiple sinks, compose observers with
`agent.Observers`:

```go
combined := agent.Observers(slogObserver, metricsObserver)

bot, err := agent.New(cfg, model,
	agent.WithObserver(combined),
)
```

Wrap observers with `NewSamplingObserver` when high-volume telemetry should be
sampled before it reaches a sink:

```go
sampled := agent.NewSamplingObserver(agent.SamplingObserverOptions{
	Child:                combined,
	EventTypes:           []agent.EventType{agent.EventAfterModel, agent.EventAfterTool},
	Ratio:                0.1,
	AlwaysSampleFailures: true,
})

bot, err := agent.New(cfg, model,
	agent.WithObserver(sampled),
)
```

Sampling can filter by event type and failure status, and can keep eligible
failures regardless of the ratio. A nil child makes the sampling observer a
no-op. The default sampler is deterministic and uses only sanitized
`Observation` fields; tests can provide `ObservationSamplerFunc` for explicit
decisions.

Events and observations carry audit fields such as event type, agent ID,
run ID, subagent ID, request ID, parent request ID, round, duration, estimated
tokens, stream telemetry, tool name, tool risk, tool schema hash, tool lifecycle
timing, approval result, skill name, safe tool result metadata, and error
category.
`ParentRequestID` links tool and
approval events to the model request that caused them, and links follow-up
model requests within the same run. Pass
`agent.WithRunID("trace-123")` to correlate SDK telemetry with an application
trace for one `Run` or `RunStream`; otherwise the SDK generates a run ID.
Tool and approval lifecycle records include `ToolSchemaHash` when the tool has
a parameter schema. The hash is deterministic over the parameter schema and
descriptor metadata, and does not include tool arguments, tool results, prompts,
or raw schema JSON.
After-tool observations include `ToolResultMetadata` with result content byte
size, sorted result metadata key names, and MCP `mcpIsError` status when
present. It does not include result content, metadata values, structured MCP
content values, tool arguments, raw errors, or secrets.
After-tool observations also include `ToolTiming` with validation, approval, and
execution duration segments. Segment durations that were not reached stay zero,
and `Duration` remains the total tool lifecycle duration.
For streaming model calls, final `EventAfterModel` records use `Duration` for
total stream duration and `StreamTelemetry` for time to first token, delta
count, streamed delta bytes, and throughput. Streams that fail before the first
delta keep first-token latency and stream counters at zero. Pass
`agent.WithStreamObservations()` to a single `RunStream` call to add
observer-only `EventStreamStart`, `EventStreamFirstDelta`, `EventStreamDone`,
and `EventStreamError` observations without emitting per-delta observations
after the first delta.
`SlogObserver` always logs `event` and `failed`, and omits other zero-value
attributes. Duration is emitted as `duration_ms`; token usage, stream telemetry,
tool metadata, `tool.timing`, approval metadata, and provider diagnostics are
emitted as structured groups.
`MetricsObserver` emits event and failure counters, plus duration recordings for
positive durations, using only low-cardinality labels derived from sanitized
observations. Positive tool lifecycle segments are recorded as
`agent_tool_lifecycle_duration` with a low-cardinality `tool_phase` label and
without `tool_name`. It does not add `ToolSchemaHash`, tool result metadata
keys, or MCP result status as labels by default.
For tracing, `examples/opentelemetry` shows an application-owned observer that
maps sanitized `Observation` values to OpenTelemetry spans, span events, and
attributes for run/request/parent correlation, event type, agent and tool
metadata, duration, error category, token usage, stream timing, and tool timing.
That example is a separate Go module; the core SDK module does not depend on
OpenTelemetry, and applications own their exporters and tracing policy.
Observations intentionally omit message content, tool arguments, tool results,
raw errors, API keys, and MCP environment values.
Nil child observers in a composed observer are ignored, and observer panics are
recovered so telemetry cannot alter agent execution.

## Error Handling

The SDK exposes sentinel errors for common control flow:

- `ErrApprovalDenied`
- `ErrToolNotFound`
- `ErrToolValidation`
- `ErrMaxToolRoundsExceeded`
- `ErrStreamingUnsupported`
- `ErrStreamingToolCallsUnsupported`
- `ErrMCPProcessExited`
- `ErrMCPRPC`
- `ErrMCPToolNotFound`
- `ErrSubagentNotFound`

Use `errors.Is` for sentinel checks and `errors.As` for structured context:

```go
reply, err := bot.Run(ctx, input)
if err != nil {
	if errors.Is(err, agent.ErrApprovalDenied) {
		return err
	}
	var agentErr *agent.AgentError
	if errors.As(err, &agentErr) {
		log.Printf("category=%s operation=%s request=%s",
			agentErr.Category,
			agentErr.Operation,
			agentErr.RequestID,
		)
	}
	return err
}
_ = reply
```

`AgentError` carries category, model error subcategory when applicable,
operation, agent ID, request ID, parent request ID, tool name, subagent ID,
round, safe provider diagnostics when available, and the wrapped cause. Provider
diagnostics can include retry-after and rate-limit hints, but never request or
response bodies, credentials, cookies, or full provider URLs.

## Skills

Skills are reusable instruction bundles. Build them directly:

```go
reviewSkill := agent.Skill{
	Name:           "review",
	Description:    "Review code changes",
	Instructions:   "Inspect changes for bugs, regressions, and missing tests.",
	TriggerPhrases: []string{"review", "code review"},
}

bot, err := agent.New(cfg, model, agent.WithSkills(reviewSkill))
```

Or load standard `SKILL.md` directories:

```text
skills/
  review/
    SKILL.md
```

Activation options:

- `ActivateSkill` for persistent activation.
- `WithRunSkills` for one run.
- Inline markers such as `+review` or `+skill:review`.
- `TriggerPhrases` or a custom matcher for implicit activation.

## Compaction

Set `CompactConfig` to compact long sessions before model calls.

```go
cfg := agent.Config{
	SystemPrompt: "You are a focused coding agent.",
	Compact: agent.CompactConfig{
		MaxTokens: 200000,
		Threshold: 0.8,
		KeepLast:  8,
	},
}
```

By default, the SDK uses an approximate token counter and a deterministic local
summary placeholder. For production summaries, attach a model-backed compactor:

```go
bot, err := agent.New(cfg, chatModel,
	agent.WithCompactor(agent.ModelCompactor{
		Model:        summaryModel,
		SystemPrompt: "Summarize context for the next agent turn.",
		KeepLast:     8,
	}),
)
```

## Subagents

Parent agents can spawn subagents and choose which capabilities are inherited.

```go
worker, err := master.SpawnSubagent(ctx, agent.SubagentOptions{
	ID:                "worker-1",
	SystemPrompt:      "You are a focused implementation worker.",
	Model:             workerModel,
	InheritToolNames:  []string{"read_file", "run_tests"},
	InheritSkillNames: []string{"review"},
	InheritMCP:        true,
})
if err != nil {
	return err
}

reply, err := master.SendMessageToSubagent(ctx, worker.ID(), "implement the next task")
```

Inheritance supports all or selected tools, MCP servers, skills, hooks, and
instruction files.

## Production Integration

Recommended production path:

1. Create a model adapter with explicit timeouts, retries, request logging, and
   provider-specific error mapping.
2. Load credentials, base URLs, MCP command paths, and secrets from your
   deployment environment.
3. Register only the tools the agent needs, with schemas and risk labels.
4. Install a deny-by-default approval policy and connect `ApprovalFunc` to your
   human or business approval workflow.
5. Attach an `Observer` that exports sanitized metadata to your logging, metrics,
   or tracing system.
6. Persist `SessionSnapshot` payloads in application-owned storage with access
   controls and retention policy.
7. Run external MCP servers under application process supervision and least
   privilege.
8. Treat hooks as enforcement points and observers as best-effort telemetry.
9. Test model adapters with fake HTTP servers and MCP integrations with fake
   stdio servers before using real providers.

For the production observability rollout checklist, including logs, metrics,
traces, sampling, provider diagnostics, stream timing, tool timing, SLOs, and
privacy red lines, see the English and Chinese production guides linked above.

Local verification:

```bash
go test ./...
go test -race ./...
go vet ./...
go test -count=1 ./...
```

## Contributing

See `CONTRIBUTING.md` for local development, testing, and documentation
guidelines. The release quality gate is the same set of commands listed above.

## License

Cube Agent SDK is licensed under the MIT License. See `LICENSE` for details.
