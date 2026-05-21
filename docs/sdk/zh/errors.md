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

`AgentError` 携带 category、operation、agent ID、request ID、parent request ID、
tool name、subagent ID、round 和被包装的 cause。需要审计上下文时使用
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
