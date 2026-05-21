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

## Observers

```go
observer := agent.ObserverFunc(func(ctx context.Context, observation agent.Observation) {
	log.Printf("type=%s request=%s round=%d failed=%v",
		observation.Type,
		observation.RequestID,
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
subagent ID, request ID, round, duration, estimated tokens, tool name, tool risk,
approval result, skill name, and error category.

Observations intentionally omit message content, tool arguments, tool results,
raw errors, API keys, and MCP environment values.
