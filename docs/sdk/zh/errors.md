# 错误处理

SDK 为常见控制流暴露哨兵错误；当稳定上下文有价值时，会把运行失败包装为
`AgentError`。

## 哨兵错误

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

使用 `errors.Is` 做哨兵判断。

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

`AgentError` 携带 category、operation、agent ID、run ID、trace ID、span ID、
trace state、request ID、parent request ID、tool name、subagent ID、round、可用时的 provider diagnostics 和被包装的 cause。需要审计上下文时使用
`errors.As`。

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

内置模型适配器会在 HTTP、transport 和 decode 失败时附加安全的 provider
diagnostics。这些字段可能包含 provider name、HTTP status、endpoint host 和
provider request ID、`RetryAfter`、`RateLimitLimit`、`RateLimitRemaining` 和
`RateLimitReset`；不会包含带 query string 的完整 URL、request/response body、
prompt、tool arguments、API key、authorization header、cookies、`Set-Cookie`
或原始 provider 错误文本。

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

直接处理模型适配器返回的错误时，可以使用 `ProviderDiagnosticsFromError`。

## 错误类别

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

这些类别用于分支、日志和遥测，避免依赖 provider 特定错误文本。
