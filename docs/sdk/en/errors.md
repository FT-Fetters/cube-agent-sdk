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
trace state, request ID, parent request ID, tool name, subagent ID, round,
provider diagnostics when available, and the wrapped cause. Use `errors.As`
when audit context is needed.

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

## Provider Diagnostics

Built-in model adapters attach safe provider diagnostics to HTTP, transport,
and decode failures. Diagnostics may include provider name, HTTP status,
endpoint host, and provider request ID. They do not include full URLs with query
strings, request or response bodies, prompts, tool arguments, API keys,
authorization headers, or raw provider error text.

```go
var agentErr *agent.AgentError
if errors.As(err, &agentErr) {
	diag := agentErr.ProviderDiagnostics
	log.Printf("provider=%s status=%d host=%s provider_request=%s",
		diag.Provider,
		diag.HTTPStatus,
		diag.EndpointHost,
		diag.RequestID,
	)
}
```

When handling errors returned directly from a model adapter, use
`ProviderDiagnosticsFromError`.

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
