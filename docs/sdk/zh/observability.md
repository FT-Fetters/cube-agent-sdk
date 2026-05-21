# 可观测性

SDK 暴露两个生命周期扩展点：

- Hooks 可以观察事件，并通过返回错误拒绝操作。
- Observers 接收脱敏遥测，不能改变执行结果。

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

Hooks 接收模型调用、审批、工具、压缩、skill 激活和 subagent 消息对应的 `Event`。

每次 `Run` 和 `RunStream` 都有一个 run ID，同一次调用发出的所有生命周期事件
共享它。可以传入 `agent.WithRunID("trace-123")` 使用应用自己的 trace ID；
否则 SDK 会基于 agent ID 和本地序列生成非空 ID。

当应用同时需要 run ID 和外部 trace ID 时，应把它们作为不同字段使用。可以把
trace 元数据附加到 context：

```go
ctx = agent.WithTraceContext(ctx, agent.TraceContext{
	TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
	SpanID:     "00f067aa0ba902b7",
	TraceState: "vendor=state",
})
```

SDK 会把 `TraceID`、`SpanID` 和 `TraceState` 传播到 events、observations 和
`AgentError`。如果没有传入 `WithRunID`，SDK 仍会生成 run ID，而不会用
`TraceID` 替代它。

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

Observer panic 会被 recover 并忽略。遥测是 best-effort，不能改变 agent 行为。

## 脱敏元数据

事件和 observations 携带 event type、agent ID、run ID、trace ID、span ID、
trace state、subagent ID、request ID、parent request ID、round、duration、
estimated tokens、真实 token usage、tool name、tool risk、approval result、skill
name、error category、model error subcategory，以及模型失败时的安全 provider
diagnostics 等审计字段。`ParentRequestID` 会把工具和审批事件关联到触发它们的模型请求，也会关联同一 run 内的后续模型请求。

`EstimatedTokens` 是 SDK 在请求侧估算的 token 数，即使 provider 没有返回 usage
也会继续填充。`TokenUsage` 则携带非 streaming `EventAfterModel` 及其 observation
中来自 `ModelResponse.Usage` 的真实 input、output 和 total tokens。如果没有可用的
usage，`TokenUsage` 字段保持零值。

Observations 有意省略消息内容、工具参数、工具结果、原始错误、API keys、带
query string 的完整 provider URL 和 MCP 环境变量。
