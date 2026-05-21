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

Observer panics are recovered and ignored. Telemetry is best-effort and must not
change agent behavior.

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
