# 流式输出

流式模型除了实现 `Model`，还需要实现 `StreamModel`。内置的
OpenAI-compatible chat completions、OpenAI Responses 和 Anthropic Messages
适配器都支持基于 provider 原生流式响应的 `RunStream`。当调用方需要增量
assistant 文本时，使用 `RunStream`。

```go
events, err := bot.RunStream(ctx, "Write a short summary.")
if err != nil {
	if errors.Is(err, agent.ErrStreamingUnsupported) {
		// Fall back to Run or use a streaming-capable adapter.
	}
	return err
}

for event := range events {
	switch event.Type {
	case agent.StreamEventDelta:
		fmt.Print(event.Delta)
	case agent.StreamEventDone:
		fmt.Println(event.Message.Content)
		_ = event.Usage // Provider 在 stream 中报告时的最终 token usage。
	case agent.StreamEventError:
		return event.Error
	}
}
```

调用方必须一直读取返回的 channel，直到它关闭；如果需要提前停止读取，必须取消传给
`RunStream` 的 context。取消 context 会释放 provider stream 并关闭返回的 channel；
在不取消 context 的情况下丢弃 channel，可能会让转发 goroutine 阻塞。

## 事件类型

- `StreamEventDelta`：增量 assistant 文本。
- `StreamEventDone`：最终 assistant 消息；如果 provider 报告 usage，也会携带 token usage。
- `StreamEventError`：流式失败。

SDK 只会在 done event 转发给调用方后提交最终 assistant 消息。中断的 delta
stream 以及已取消、被丢弃的 stream 不会持久化部分或未送达的 assistant 文本。

最终的 streaming `EventAfterModel` event 和 observation 会通过 `Duration` 携带整个
stream 的持续时间。如果模型在 done event 上报告 usage，同一组 token counts 也会写入
`TokenUsage`。只要至少收到一个 delta，它们还会包含脱敏的 `StreamTelemetry`：
time to first token、delta 数量、streamed delta 字节数和吞吐量。Stream telemetry
不会包含 streamed text。

如果需要 start、first delta、done 和 error 的 observer-only stream lifecycle
telemetry，可以在 `RunStream` 调用上使用 `WithStreamObservations()`。该选项不会为
第一个 delta 之后的每个 delta 逐条发出 observations。

## 当前限制

流式 tool calls 还不会被执行。如果流式模型发出 tool calls，SDK 会报告
`ErrStreamingToolCallsUnsupported`。

如果 provider 在初始 streaming HTTP 请求阶段拒绝请求，`RunStream` 会立即返回结构化
provider error。如果 stream 已经开始，然后 provider 发出错误或无效事件，调用方会收到
携带安全 provider diagnostics 的 `StreamEventError`（当 diagnostics 可用时）。
