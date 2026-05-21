# Errors

The SDK exposes sentinel errors for common control flow and wraps operational
failures in `AgentError` when stable context is useful.

## Sentinel Errors

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
- `ErrModelAPIUnsupported`

Use `errors.Is` for sentinel checks.

```go
reply, err := bot.Run(ctx, input)
if err != nil {
	if errors.Is(err, agent.ErrApprovalDenied) {
		return err
	}
	return err
}
_ = reply
```

## AgentError

`AgentError` carries category, operation, agent ID, run ID, trace ID, span ID,
trace state, request ID, parent request ID, tool name, subagent ID, round, and
the wrapped cause. Use `errors.As` when audit context is needed.

```go
var agentErr *agent.AgentError
if errors.As(err, &agentErr) {
	log.Printf("category=%s operation=%s request=%s",
		agentErr.Category,
		agentErr.Operation,
		agentErr.RequestID,
	)
}
```

## Error Categories

- `ErrorCategoryModel`
- `ErrorCategoryTool`
- `ErrorCategoryApproval`
- `ErrorCategorySchema`
- `ErrorCategoryMCP`
- `ErrorCategoryCompact`
- `ErrorCategorySubagent`
- `ErrorCategoryStreaming`
- `ErrorCategoryHook`
- `ErrorCategoryConfig`

Categories are intended for branching, logging, and telemetry without relying on
provider-specific error text.
