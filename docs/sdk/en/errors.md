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
- `ErrSessionNotFound`
- `ErrSessionVersionMismatch`
- `ErrSessionInvalidRecord`
- `ErrSessionEventConflict`
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
provider diagnostics when available, model error subcategory when applicable,
and the wrapped cause. Use `errors.As` when audit context is needed.

```go
var agentErr *agent.AgentError
if errors.As(err, &agentErr) {
	log.Printf("category=%s subcategory=%s operation=%s request=%s",
		agentErr.Category,
		agentErr.ModelErrorSubcategory,
		agentErr.Operation,
		agentErr.RequestID,
	)
}
```

## Provider Diagnostics

Built-in model adapters attach safe provider diagnostics to HTTP, transport,
and decode failures. Diagnostics may include provider name, HTTP status,
endpoint host, provider request ID, `RetryAfter`, `RateLimitLimit`,
`RateLimitRemaining`, and `RateLimitReset`. They do not include full URLs with
query strings, request or response bodies, prompts, tool arguments, API keys,
authorization headers, cookies, `Set-Cookie`, or raw provider error text.

```go
var agentErr *agent.AgentError
if errors.As(err, &agentErr) {
	diag := agentErr.ProviderDiagnostics
	log.Printf("provider=%s status=%d host=%s provider_request=%s retry_after=%s rate_remaining=%s",
		diag.Provider,
		diag.HTTPStatus,
		diag.EndpointHost,
		diag.RequestID,
		diag.RetryAfter,
		diag.RateLimitRemaining,
	)
}
```

When handling errors returned directly from a model adapter, use
`ProviderDiagnosticsFromError`.

## Model Error Subcategories

Model failures keep `ErrorCategoryModel` as their high-level category and may
also carry `ModelErrorSubcategory` for logs and alert grouping. Built-in
providers classify HTTP 401/403 as `auth`, HTTP 429 as `rate_limited`, other
HTTP 400-499 as `bad_request`, HTTP 500-599 as `server_error`, timeout-like
transport failures as `timeout`, other transport failures as `transport_error`,
JSON decode failures as `decode_error`, and unclassified model/provider
failures as `unknown`.

Use `ModelErrorSubcategoryFromError` when handling errors returned directly
from a model adapter.

## Error Categories

- `ErrorCategoryModel`
- `ErrorCategoryTool`
- `ErrorCategoryApproval`
- `ErrorCategorySchema`
- `ErrorCategoryMCP`
- `ErrorCategoryCompact`
- `ErrorCategorySubagent`
- `ErrorCategoryStreaming`
- `ErrorCategorySession`
- `ErrorCategoryHook`
- `ErrorCategoryConfig`

Categories are intended for branching, logging, and telemetry without relying on
provider-specific error text.
