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

## Sanitized Metadata

Events and observations carry audit fields such as event type, agent ID,
run ID, trace ID, span ID, trace state, subagent ID, request ID, parent request
ID, round, duration, estimated tokens, real token usage, tool name, tool risk,
approval result, skill name, error category, model error subcategory, and safe
provider diagnostics for model failures. `ParentRequestID` links tool and
approval events to the model request that caused them, and links follow-up model
requests within the same run.

`EstimatedTokens` is the SDK's request-side estimate and stays populated even
when the provider does not report usage. `TokenUsage` carries real input,
output, and total token counts from `ModelResponse.Usage` on non-streaming
`EventAfterModel` records and their observations. If usage is unavailable, the
`TokenUsage` fields remain zero.

Observations intentionally omit message content, tool arguments, tool results,
raw errors, API keys, full provider URLs with query strings, and MCP
environment values.

`SlogObserver` logs `event` and `failed` on every record. It omits other
zero-value fields, emits duration as `duration_ms`, and groups token usage, tool
metadata, approval metadata, and provider diagnostics as structured attributes.

`MetricsObserver` increments `agent_observations_total` for every observation,
increments `agent_observation_failures_total` for failed observations, and
records positive durations as `agent_observation_duration`. Metric labels are
limited to `event`, `failed`, `error_category`, `model_error_subcategory`,
`tool_name`, `tool_risk`, `provider`, and `http_status` when present. Run IDs,
request IDs, trace IDs, and provider request IDs are intentionally omitted from
metric labels by default.
