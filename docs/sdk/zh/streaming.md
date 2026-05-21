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

## 当前限制

流式 tool calls 还不会被执行。如果流式模型发出 tool calls，SDK 会报告
`ErrStreamingToolCallsUnsupported`。
