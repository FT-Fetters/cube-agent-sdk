# 流式输出

流式模型除了实现 `Model`，还需要实现 `StreamModel`。当调用方需要增量
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
	case agent.StreamEventError:
		return event.Error
	}
}
```

## 事件类型

- `StreamEventDelta`：增量 assistant 文本。
- `StreamEventDone`：最终 assistant 消息。
- `StreamEventError`：流式失败。

SDK 只会在 done event 到达后提交最终 assistant 消息。中断的 delta stream 不会
持久化部分 assistant 文本。

最终的 streaming `EventAfterModel` event 和 observation 会通过 `Duration` 携带整个
stream 的持续时间。只要至少收到一个 delta，它们还会包含脱敏的 `StreamTelemetry`：
time to first token、delta 数量、streamed delta 字节数和吞吐量。Stream telemetry
不会包含 streamed text。

如果需要 start、first delta、done 和 error 的 observer-only stream lifecycle
telemetry，可以在 `RunStream` 调用上使用 `WithStreamObservations()`。该选项不会为
第一个 delta 之后的每个 delta 逐条发出 observations。

## 当前限制

流式 tool calls 还不会被执行。如果流式模型发出 tool calls，SDK 会报告
`ErrStreamingToolCallsUnsupported`。
