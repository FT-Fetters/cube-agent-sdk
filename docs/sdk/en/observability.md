# Observability

The SDK exposes two lifecycle extension points:

- Hooks can observe events and reject operations by returning an error.
- Observers receive sanitized telemetry and must not alter execution.

## Hooks

```go
hook := func(ctx context.Context, event agent.Event) error {
	if event.Type == agent.EventBeforeTool && event.ToolRisk == agent.ToolRiskDestructive {
		return fmt.Errorf("destructive tools require a separate workflow")
	}
	return nil
}

bot, err := agent.New(cfg, model, agent.WithHook(hook))
```

Hooks receive `Event` values for model calls, approvals, tools, compaction,
skill activation, and subagent messages.

Every `Run` and `RunStream` has a run ID shared by all lifecycle events emitted
for that call. Pass `agent.WithRunID("trace-123")` to use an application trace
ID; otherwise the SDK generates a non-empty ID from the agent ID and a local
sequence.

Keep run IDs and external trace IDs distinct when you need both. Trace metadata
can be attached to the context:

```go
ctx = agent.WithTraceContext(ctx, agent.TraceContext{
	TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
	SpanID:     "00f067aa0ba902b7",
	TraceState: "vendor=state",
})
```

The SDK propagates `TraceID`, `SpanID`, and `TraceState` to events,
observations, and `AgentError` values. If `WithRunID` is not supplied, the SDK
still generates a run ID instead of replacing it with `TraceID`.

## Request IDs

By default, request IDs keep the existing `<agent-id>-request-<sequence>`
format. Install `WithRequestIDGenerator` when an application needs request IDs
to match an upstream logging or tracing convention:

```go
bot, err := agent.New(cfg, model,
	agent.WithRequestIDGenerator(func(ctx agent.RequestIDContext) string {
		return fmt.Sprintf("%s.%s.%d", ctx.RunID, ctx.Operation, ctx.Sequence)
	}),
)
```

`RequestIDContext` contains safe correlation fields only: agent ID, run ID,
trace metadata, event type, operation, local sequence, round, parent request
ID, tool name, and subagent ID. It never includes prompts, message content,
tool arguments, tool results, raw errors, credentials, provider URLs, or MCP
settings.

The generator should return a non-empty ID. If it returns an empty or
whitespace-only string, or if it panics, the SDK falls back to the default ID for
that request. Passing a nil generator to `WithRequestIDGenerator` returns a
configuration error. The SDK does not de-duplicate custom IDs; if a generator
returns the same value more than once, observations and `ParentRequestID` keep
that value exactly as returned.

## Observers

```go
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

bot, err := agent.New(cfg, model, agent.WithObserver(observer))
```

For standard-library structured logs, configure `SlogObserver` explicitly:

```go
slogObserver := agent.NewSlogObserver(agent.SlogObserverOptions{
	Logger:  slog.Default(),
	Level:   slog.LevelInfo,
	Message: "agent observation",
})

bot, err := agent.New(cfg, model, agent.WithObserver(slogObserver))
```

For metrics, implement `MetricSink` in your application and install
`MetricsObserver`:

```go
type appMetricSink struct{}

func (appMetricSink) AddCounter(ctx context.Context, name string, delta int64, labels []agent.MetricLabel) {
	// Forward the counter update to your metrics backend.
}

func (appMetricSink) RecordDuration(ctx context.Context, name string, duration time.Duration, labels []agent.MetricLabel) {
	// Forward the duration to your metrics backend or histogram.
}

metricsObserver := agent.NewMetricsObserver(agent.MetricsObserverOptions{
	Sink: appMetricSink{},
})

bot, err := agent.New(cfg, model, agent.WithObserver(metricsObserver))
```

Use `Observers` or `MultiObserver` to fan out sanitized observations to more
than one observer:

```go
combined := agent.Observers(slogObserver, metricsObserver)

bot, err := agent.New(cfg, model, agent.WithObserver(combined))
```

Wrap any observer with `NewSamplingObserver` to reduce observation volume while
keeping the telemetry surface sanitized:

```go
sampled := agent.NewSamplingObserver(agent.SamplingObserverOptions{
	Child:                combined,
	EventTypes:           []agent.EventType{agent.EventAfterModel, agent.EventAfterTool},
	FailureStatus:        agent.SampleAllObservations,
	Ratio:                0.1,
	AlwaysSampleFailures: true,
})

bot, err := agent.New(cfg, model, agent.WithObserver(sampled))
```

`EventTypes` filters by event type when non-empty, `FailureStatus` can limit
sampling to failed or successful observations, and `Ratio` applies to eligible
observations. `AlwaysSampleFailures` keeps eligible failed observations even
when the ratio is low. A nil `Child` makes the sampling observer a no-op. The
default ratio sampler is deterministic and hashes only sanitized `Observation`
fields; use `ObservationSampler` or `ObservationSamplerFunc` when tests or
deployments need caller-controlled decisions.

Nil children are ignored. Observer panics are recovered and ignored, including
inside fan-out groups, so one child observer cannot prevent later children from
receiving an observation. Telemetry is best-effort and must not change agent
behavior. `NoopObserver` remains the default; slog output is only emitted when
the application installs `SlogObserver` with `WithObserver`, and metrics are
only emitted when the application installs `MetricsObserver` with a sink.

## OpenTelemetry Integration

The core SDK does not import or require OpenTelemetry. Applications that use
OpenTelemetry can bridge the sanitized `Observation` surface in their own
module.

The credential-free example under `examples/opentelemetry` is a separate Go
module with its own OpenTelemetry dependencies:

```bash
go -C examples/opentelemetry test ./...
go -C examples/opentelemetry run .
```

The example maps observations to OpenTelemetry spans, span events, and
attributes for run/request/parent correlation, event type, agent ID, tool name
and risk, duration, error category, token usage, streaming telemetry, tool
lifecycle timing, tool schema hash, safe tool result metadata, and safe provider
diagnostics. It does not map prompts, message content, tool arguments, tool
result content, tool result metadata values, raw errors, credentials, full
provider URLs, or MCP environment values.

## Telemetry Attribute Names

The stable naming surface lives in code as `TelemetryAttr*` constants and
`StableTelemetryAttributeNames()`. Logs, traces, custom observers, and the
OpenTelemetry example should prefer the dotted `agent.*` attributes. Existing
`SlogObserver` snake_case and grouped fields remain as compatibility aliases,
but new integrations should read the `agent.*` fields. `MetricsObserver` label
names are also stable, but they intentionally keep the existing snake_case names
because metric names already carry the agent namespace.

These names are part of the SDK observability contract. Compatible releases may
add new attributes or labels, but should not remove, rename, or change the
meaning of an existing name outside a major-version compatibility break.

| Attribute | Signals | Cardinality | Notes |
| --- | --- | --- | --- |
| `agent.event` | logs, traces, metrics as `event` | low | SDK event type. |
| `agent.failed` | logs, traces, metrics as `failed` | low | Boolean failure status. |
| `agent.id` | logs, traces | high | Agent identifier. |
| `agent.run_id` | logs, traces | high | Run correlation ID. |
| `agent.subagent_id` | logs, traces | high | Subagent identifier. |
| `agent.trace_id` | logs, traces | high | Caller-provided trace ID. |
| `agent.span_id` | logs, traces | high | Caller-provided span ID. |
| `agent.trace_state` | logs, traces | high | Caller-provided trace state. |
| `agent.request_id` | logs, traces | high | Request correlation ID. |
| `agent.parent_request_id` | logs, traces | high | Parent request correlation ID. |
| `agent.round` | logs, traces | numeric | Model/tool round number. |
| `agent.duration_ms` | logs, traces | numeric | Observation duration in milliseconds. |
| `agent.estimated_tokens` | logs, traces | numeric | SDK request-side estimate. |
| `agent.tokens.input` | logs, traces | numeric | Provider-reported input tokens. |
| `agent.tokens.output` | logs, traces | numeric | Provider-reported output tokens. |
| `agent.tokens.total` | logs, traces | numeric | Provider-reported total tokens. |
| `agent.stream.time_to_first_token_ms` | logs, traces | numeric | Streaming time to first delta. |
| `agent.stream.delta_count` | logs, traces | numeric | Streamed delta count. |
| `agent.stream.byte_count` | logs, traces | numeric | Streamed delta byte count. |
| `agent.stream.throughput_bytes_per_second` | logs, traces | numeric | Stream throughput. |
| `agent.tool.name` | logs, traces, metrics as `tool_name` | bounded/high | Registered tool name; keep metric use bounded. |
| `agent.tool.risk` | logs, traces, metrics as `tool_risk` | low | Tool risk label. |
| `agent.tool.schema_hash` | logs, traces | high | Safe schema drift identifier. |
| `agent.tool.timing.validation_ms` | logs, traces | numeric | Tool validation duration. |
| `agent.tool.timing.approval_ms` | logs, traces | numeric | Approval wait duration. |
| `agent.tool.timing.execution_ms` | logs, traces | numeric | Tool execution duration. |
| `agent.tool.result.content_bytes` | logs, traces | numeric | Tool result content byte length. |
| `agent.tool.result.metadata_keys` | logs, traces | high | Metadata key names only, never values. |
| `agent.tool.result.mcp_is_error` | logs, traces | low | MCP result error flag. |
| `agent.skill.name` | logs, traces | high | Activated skill name. |
| `agent.approval.approved` | logs, traces | low | Approval decision. |
| `agent.approval.reason` | logs, traces | high | Approval reason text. |
| `agent.error.category` | logs, traces, metrics as `error_category` | low | Safe error category. |
| `agent.error.model_subcategory` | logs, traces, metrics as `model_error_subcategory` | low | Safe model error subcategory. |
| `agent.provider.name` | logs, traces, metrics as `provider` | low | Provider adapter name. |
| `agent.provider.http_status` | logs, traces, metrics as `http_status` | low | HTTP status code. |
| `agent.provider.endpoint_host` | logs, traces | high | Host only, never full URL. |
| `agent.provider.request_id` | logs, traces | high | Provider request ID. |
| `agent.provider.retry_after` | logs, traces | high | Retry header value. |
| `agent.provider.rate_limit.limit` | logs, traces | high | Provider rate limit header value. |
| `agent.provider.rate_limit.remaining` | logs, traces | high | Provider remaining quota header value. |
| `agent.provider.rate_limit.reset` | logs, traces | high | Provider reset header value. |

`StableTelemetryMetricLabelNames()` returns the built-in metric label names:
`event`, `failed`, `error_category`, `model_error_subcategory`, `tool_name`,
`tool_risk`, `provider`, `http_status`, and `tool_phase`. Default metrics omit
run IDs, request IDs, trace IDs, span IDs, trace state, provider request IDs,
tool schema hashes, tool result metadata keys, tool result metadata values, and
MCP environment values. Treat `tool_name` as bounded: it is useful when the tool
catalog is controlled, but high-cardinality dynamic tool catalogs should avoid
using it for backend labels.

Do not map prompts, message content, tool arguments, tool result content, tool
result metadata values, raw errors, credentials, full provider URLs, or MCP
environment values into logs, metric labels, traces, span events, or baggage.
`ForbiddenTelemetryFieldNames()` returns this policy list for tests and docs.

## Sanitized Metadata

Events and observations carry audit fields such as event type, agent ID,
run ID, trace ID, span ID, trace state, subagent ID, request ID, parent request
ID, round, duration, estimated tokens, real token usage, streaming telemetry,
tool name, tool risk, tool schema hash, tool lifecycle timing, approval result,
skill name, error category, model error subcategory, safe tool result metadata,
and safe provider diagnostics for model failures. `ParentRequestID` links tool
and approval events to the model request that caused them, and links follow-up
model requests within the same run.

Tool and approval lifecycle records include `ToolSchemaHash` when the tool has
a parameter schema. The hash is deterministic over the parameter schema and
descriptor metadata, and does not include tool arguments, tool results, prompts,
or raw schema JSON. It stays empty for tools without parameter schemas.

After-tool observations include `ToolResultMetadata` with result content byte
size, sorted result metadata key names, and MCP `mcpIsError` status when
present. They do not include result content, metadata values, structured MCP
content values, tool arguments, raw errors, or secrets.

After-tool observations also include `ToolTiming` with validation, approval, and
execution duration segments. Segment durations that were not reached stay zero,
and `Duration` remains the total tool lifecycle duration. These fields contain
only durations and do not include tool arguments, results, metadata values, raw
errors, prompts, or credentials.

`EstimatedTokens` is the SDK's request-side estimate and stays populated even
when the provider does not report usage. `TokenUsage` carries real input,
output, and total token counts from `ModelResponse.Usage` on non-streaming
`EventAfterModel` records and from final `StreamEvent.Usage` on streaming
`EventAfterModel` records and their observations. If usage is unavailable, the
`TokenUsage` fields remain zero.

For streaming `EventAfterModel` records, `Duration` is the total stream
duration. `StreamTelemetry` carries time to first token, delta count, streamed
delta byte count, and bytes-per-second throughput when at least one delta was
received. If a stream fails before the first delta, time to first token and the
stream counters remain zero while `Duration` still records the failed stream
duration.

`RunStream` does not emit stream lifecycle observations by default. Pass
`WithStreamObservations()` on a single `RunStream` call to add observer-only
`EventStreamStart`, `EventStreamFirstDelta`, `EventStreamDone`, and
`EventStreamError` observations. Only the first delta is observed; subsequent
deltas are not emitted as observations.

Observations intentionally omit prompts, message content, tool arguments, tool
result content, tool result metadata values, raw errors, credentials, full
provider URLs with query strings, and MCP environment values.

`SlogObserver` logs `agent.event` and `agent.failed` on every record and keeps
legacy `event` and `failed` aliases. It omits other zero-value fields, emits
duration as `agent.duration_ms`, and also keeps legacy grouped token usage, tool
metadata, `tool.timing`, stream telemetry, approval metadata, and provider
diagnostics for compatibility.

`MetricsObserver` increments `agent_observations_total` for every observation,
increments `agent_observation_failures_total` for failed observations, and
records positive durations as `agent_observation_duration`. Positive tool
lifecycle segments are recorded as `agent_tool_lifecycle_duration` with
`tool_phase` values of `validation`, `approval`, or `execution`; these segment
metrics omit `tool_name` to avoid high-cardinality labels. Metric labels are
limited to `event`, `failed`, `error_category`, `model_error_subcategory`,
`tool_name`, `tool_risk`, `provider`, and `http_status` when present on general
observation metrics, and to low-cardinality tool timing labels on lifecycle
segment metrics. Run IDs, request IDs, trace IDs, provider request IDs,
`ToolSchemaHash`, tool result metadata keys, and MCP result status are
intentionally omitted from metric labels by default.
