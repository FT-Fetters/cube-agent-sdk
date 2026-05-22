# Production

The SDK intentionally keeps production infrastructure outside the core runtime.
Use the SDK primitives, but make deployment, security, telemetry, and storage
decisions in the application.

This guide focuses on production observability. The companion
[Observability](./observability.md) page documents the exact SDK APIs and stable
telemetry names; use this page as the rollout checklist for real deployments.

## Production Observability Checklist

Before enabling an agent in production, verify each item below:

1. Pick a correlation model: pass an application run ID with `WithRunID`, attach
   upstream trace metadata with `WithTraceContext`, and install
   `WithRequestIDGenerator` only when your platform requires a custom request ID
   format.
2. Install an explicit `Observer`. The default is `NoopObserver`, so production
   logs, metrics, and traces only exist after the application wires them.
3. Export structured logs through `SlogObserver` or a custom observer that reads
   only the safe `Observation` surface.
4. Export counters and duration histograms through `MetricsObserver` with an
   application-owned `MetricSink`.
5. Bridge traces through the application or adapt the credential-free
   `examples/opentelemetry` module; the core SDK does not import OpenTelemetry.
6. Decide whether metrics must be exact. If so, put `MetricsObserver` outside
   `NewSamplingObserver` and sample only detailed logs or traces.
7. Keep high-cardinality fields in logs and traces, not default metric labels.
8. Alert on model failure rate, provider throttling, tool failures, stream
   latency, and missing telemetry.
9. Document the privacy red lines and test that forbidden fields never enter
   logs, metric labels, traces, span events, or baggage.
10. Run local fake-provider tests, the OpenTelemetry example tests, and the
    optional live-provider test before rollout.

## Signal Wiring

Observers are best-effort telemetry sinks. They receive sanitized `Observation`
values and cannot reject or alter execution. Use hooks for enforcement and
observers for telemetry.

```go
slogObserver := agent.NewSlogObserver(agent.SlogObserverOptions{
	Logger:  slog.Default(),
	Level:   slog.LevelInfo,
	Message: "agent observation",
})

metricsObserver := agent.NewMetricsObserver(agent.MetricsObserverOptions{
	Sink: appMetricSink{},
})

// Keep aggregate metrics exact, then sample high-volume detailed signals.
sampledDetails := agent.NewSamplingObserver(agent.SamplingObserverOptions{
	Child:                agent.Observers(slogObserver, appOpenTelemetryObserver),
	EventTypes:           []agent.EventType{agent.EventAfterModel, agent.EventAfterTool},
	Ratio:                0.1,
	AlwaysSampleFailures: true,
})

observer := agent.Observers(metricsObserver, sampledDetails)

bot, err := agent.New(cfg, model,
	agent.WithObserver(observer),
	agent.WithRequestIDGenerator(func(ctx agent.RequestIDContext) string {
		return fmt.Sprintf("%s.%s.%d", ctx.RunID, ctx.Operation, ctx.Sequence)
	}),
)
```

Attach upstream trace metadata to the request context and keep it separate from
the SDK run ID:

```go
ctx = agent.WithTraceContext(ctx, agent.TraceContext{
	TraceID:    upstreamTraceID,
	SpanID:     upstreamSpanID,
	TraceState: upstreamTraceState,
})

reply, err := bot.Run(ctx, input, agent.WithRunID(applicationRunID))
```

`RunID`, `RequestID`, `ParentRequestID`, and `TraceContext` are correlation
fields. They are safe for logs and traces, but they are high-cardinality and
should not become default metric labels.

## Logs, Metrics, and Traces

Map each signal to a clear operational purpose:

| Signal | Recommended SDK source | Production use |
| --- | --- | --- |
| Structured logs | `SlogObserver` or a custom observer over `Observation` | Request reconstruction, incident timelines, provider diagnostics, and per-run debugging. |
| Metrics | `MetricsObserver` with an application `MetricSink` | Counters, failure ratios, latency histograms, tool phase histograms, and alert inputs. |
| Traces | Application OpenTelemetry bridge or `examples/opentelemetry` | Run/request/parent span structure, cross-service correlation, stream timing, and tool timing. |

Use the stable `agent.*` attributes for logs and traces. Use the stable
`MetricsObserver` label names for metrics. Metrics are intentionally narrower:
they omit run IDs, request IDs, trace IDs, provider request IDs, tool schema
hashes, tool result metadata keys, and MCP environment values by default.

Recommended mappings:

- `agent.event`, `agent.failed`, `agent.error.category`, and
  `agent.error.model_subcategory` drive error dashboards and alerts.
- `agent.duration_ms` and `agent.tool.timing.*` feed latency histograms.
- `agent.stream.time_to_first_token_ms`, `agent.stream.delta_count`,
  `agent.stream.byte_count`, and
  `agent.stream.throughput_bytes_per_second` explain streaming quality.
- `agent.provider.*` fields diagnose provider throttling and outages without
  exposing request bodies or credentials.
- `agent.run_id`, `agent.request_id`, `agent.parent_request_id`, and
  `agent.trace_id` are log and trace correlation fields.

## Sampling Strategy

Sample detailed telemetry after the SDK has already produced sanitized
observations. Do not use sampling as a privacy boundary.

Recommended production pattern:

1. Keep low-volume counters and SLO histograms exact when backend cost allows.
2. Sample verbose logs and trace spans with `NewSamplingObserver`.
3. Use `AlwaysSampleFailures` so failed observations remain visible.
4. Sample by event type when needed. `EventAfterModel`, `EventAfterTool`, and
   stream lifecycle observations usually create the most useful detailed data.
5. Use separate ratios per environment. Development and canaries can run at
   100%; high-volume production paths often need lower ratios.
6. Revisit ratios after traffic or tool catalogs change.

If you place `MetricsObserver` behind a sampling observer, counters and
histograms will describe only sampled observations. That can be useful for cost
control, but it should be an explicit decision.

## High-Cardinality Fields

High-cardinality fields are safe only when the destination can handle them.
Keep these in logs and traces unless your metrics backend has a deliberate
cardinality budget:

- run IDs, request IDs, parent request IDs, trace IDs, span IDs, and trace
  state;
- agent IDs, subagent IDs, provider request IDs, endpoint hosts, retry-after
  values, and rate-limit header values;
- tool names when the tool catalog is dynamic;
- tool schema hashes, tool result metadata keys, skill names, and approval
  reason text.

Low-cardinality fields are usually safe as metric labels: event type, failed
status, error category, model error subcategory, tool risk, provider name, HTTP
status, and tool timing phase. Treat `tool_name` as bounded. It is useful for a
small controlled tool catalog, but it should be removed or bucketed for dynamic
tools.

## Alerts, SLOs, and Dashboards

Start with user-impacting SLOs, then add diagnostic dashboards.

Suggested SLOs:

- Agent availability: successful runs divided by attempted runs, excluding
  expected approval denials if they are product behavior.
- End-to-end latency: p50, p95, and p99 `agent.duration_ms` for model calls and
  total runs at the application boundary.
- Tool latency: p95 and p99 `agent.tool.timing.execution_ms` for tools that
  call downstream systems.
- Streaming quality: p95 time to first token and stream error rate for
  `RunStream` entry points.

Suggested alerts:

- Elevated `agent_observation_failures_total` ratio by `error_category` or
  `model_error_subcategory`.
- Provider 429, 408, and 5xx spikes by provider and HTTP status.
- Provider rate-limit remaining near zero, or repeated retry-after headers.
- Tool validation failures after a deploy, which can indicate schema drift or a
  model/tool contract mismatch.
- Approval denials above the expected baseline for write or destructive tools.
- Stream time to first token or total stream duration above SLO.
- No observations for active traffic, which usually means observer wiring,
  sampling, exporter, or backend ingestion broke.

Suggested dashboards:

- Overview: runs, failures, latency, token usage, active providers, and active
  tool count.
- Provider health: status codes, retry-after, rate-limit headers, request IDs,
  and model error subcategories.
- Tool health: validation, approval, execution durations, error category, tool
  risk, and controlled tool name breakdown.
- Streaming: time to first token, delta count, byte count, throughput, stream
  errors, and stream lifecycle observations when enabled.
- Privacy audit: absence checks for forbidden field names and unexpected
  high-cardinality metric labels.

## Provider Diagnostics

Built-in provider adapters attach safe diagnostics to provider failures. Use
`AgentError.ProviderDiagnostics` when handling SDK errors, or call
`ProviderDiagnosticsFromError` when you are inspecting an error returned by a
model adapter directly.

Safe provider diagnostics can include:

- provider adapter name;
- HTTP status;
- endpoint host only, never the full URL;
- provider request ID;
- retry-after and rate-limit header values.

They must not include request bodies, response bodies, prompt text, tool
arguments, cookies, credentials, or full provider URLs. For incident response,
log the provider request ID and HTTP status, then use the provider console or
support workflow to inspect server-side details under the provider's access
controls.

## Model Reliability

Use `NewReliableModel` at the model boundary when production traffic needs local
timeout, retry, backoff, rate-limit, circuit-breaker, or budget controls. The
wrapper does not expose prompts, messages, tool arguments, tool results, raw
provider errors, credentials, or full provider URLs in `ReliabilityEvent`.
Export only the safe fields on that event plus provider diagnostics.

Recommended starting policy:

- Set `WithReliablePerAttemptTimeout` below the provider or transport timeout.
- Set `WithReliableTotalTimeout` to cap retries and backoff for one model call.
- Keep `WithReliableMaxAttempts` small; the default retry classifier only
  retries timeouts, HTTP 408/429, and 5xx provider diagnostics/subcategories.
- Use `WithReliableRateLimit` and `WithReliableCircuitBreaker` to fail fast
  during local overload or provider incidents.
- Use `WithReliableTokenBudget` and `WithReliableCostBudget` as guardrails, not
  accounting truth. Input tokens are estimated before each attempt, and exact
  usage is applied only when the model reports usage.

`ReliabilityEvent` types distinguish attempt starts, model attempt failures,
retry scheduling, final failures, successes, budget rejections, rate rejections,
and circuit rejections. Streaming wrappers apply these checks before stream
startup and do not retry after deltas begin.

## Streaming and Tool Timing

For streaming runs, final `EventAfterModel` observations carry total stream
duration in `Duration`, provider `TokenUsage` when the final done event reports
it, and sanitized `StreamTelemetry`:

- time to first token;
- delta count;
- streamed byte count;
- bytes-per-second throughput.

`RunStream` does not emit stream lifecycle observations by default. Add
`WithStreamObservations()` on a single call when you need observer-only
`EventStreamStart`, `EventStreamFirstDelta`, `EventStreamDone`, or
`EventStreamError` observations. The SDK emits only the first-delta lifecycle
observation, not one observation per delta.

After-tool observations carry `ToolTiming`:

- validation duration shows local schema and argument validation cost;
- approval duration shows human or business approval wait time;
- execution duration shows the actual tool call cost.

Use these phases to decide whether an incident belongs to model/tool contract
drift, approval workflow latency, or a downstream service called by the tool.
Tool result metadata is limited to content byte count, metadata key names, and
MCP `mcpIsError` status; it does not include tool result content or metadata
values.

## Privacy and Red Lines

`Observation` is designed as a safe telemetry surface, but production observers
can still become unsafe if the application enriches records with raw inputs.
Do not map these classes into logs, metric labels, traces, span events, or
baggage. `ForbiddenTelemetryFieldNames()` returns the same policy list for
tests:

- `prompts`
- `message_content`
- `tool_arguments`
- `tool_result_content`
- `tool_result_metadata_values`
- `raw_errors`
- `credentials`
- `full_provider_urls`
- `mcp_environment_values`

Operational rules:

- Redact before export, not only in the backend UI.
- Do not attach prompt or tool payloads to OpenTelemetry baggage.
- Do not log raw provider errors; use `AgentError` categories and provider
  diagnostics instead.
- Do not store raw tool result content in general telemetry. Store product data
  in product storage with product access controls.
- Treat session snapshots as user content and apply storage encryption, access
  controls, and retention.

## Live Test and Local Verification

Use local tests for the normal release gate:

```bash
go test ./... -skip '^TestLiveAPIModelRun$'
go test -race ./...
go vet ./...
go test -count=1 ./...
go -C examples/opentelemetry test ./...
```

Use fake HTTP servers for provider adapters and fake stdio processes for MCP
integrations. Before a production rollout, run the optional live-provider test
against a low-risk account and model:

```bash
MODEL_API_TYPE=anthropic-messages \
MODEL_BASE_URL=https://api.anthropic.com \
MODEL_API_KEY="<your-api-key>" \
MODEL_NAME=claude-sonnet-4-6 \
go test -v -run '^TestLiveAPIModelRun$' .
```

The live test skips when configuration is incomplete. Do not use production
customer prompts, real tool arguments, or long-lived credentials in live
diagnostics. Keep verbose logs sanitized and rotate any credential that appears
outside secret storage.

## Troubleshooting Runbook

When production observability is missing or confusing, work through this order:

1. No logs: confirm `WithObserver` is installed, `SlogObserver` has a logger and
   level that your process emits, and the observer is not hidden behind a zero
   sampling ratio.
2. No metrics: confirm the `MetricSink` is wired, the observer is outside
   sampling if exact metrics are expected, and the backend accepts the stable
   label names.
3. No traces: confirm the OpenTelemetry bridge receives observations, trace
   metadata is attached with `WithTraceContext`, and the exporter is flushing.
4. Correlation is broken: compare `RunID`, `RequestID`, `ParentRequestID`, and
   `TraceContext`. Custom request ID generators must return non-empty IDs.
5. Provider failures are opaque: inspect `AgentError` category, model
   subcategory, HTTP status, provider request ID, retry-after, and rate-limit
   diagnostics.
6. Tool calls are slow: split `ToolTiming` into validation, approval, and
   execution to find the owner.
7. Streams feel slow: compare total duration with time to first token, delta
   count, byte count, and throughput; enable `WithStreamObservations()` for
   one request path if lifecycle detail is needed.
8. Metric costs spike: look for high-cardinality labels, especially tool names,
   request IDs, provider request IDs, schema hashes, and metadata keys.
9. Sensitive data appears in telemetry: stop the exporter, preserve audit
   evidence, remove the unsafe enrichment, rotate exposed credentials, and add
   a regression test using `ForbiddenTelemetryFieldNames()`.
