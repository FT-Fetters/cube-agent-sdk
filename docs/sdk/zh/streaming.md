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
	case agent.StreamEventToolCallStart:
		fmt.Printf("tool starting: %s\n", event.ToolCall.Name)
	case agent.StreamEventToolCallDone:
		fmt.Printf("tool ready: %s\n", event.ToolCall.Name)
	case agent.StreamEventDone:
		fmt.Println(event.Message.Content)
		_ = event.Usage  // Provider 在 stream 中报告时的最终 token usage。
		_ = event.Finish // Provider 在 stream 中报告时的安全 finish metadata。
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
- `StreamEventToolCallStart`：安全的 tool-call 边界 metadata，例如 tool call ID、名称和 provider stream index；不包含 tool arguments。
- `StreamEventToolCallDone`：当 provider adapter 已经能重建 streamed tool call 时发出的安全边界 metadata；不包含 tool arguments。
- `StreamEventDone`：最终 assistant 消息；如果 provider 报告 usage 和安全 finish metadata，也会一起携带。
- `StreamEventError`：流式失败。

SDK 只会在 done event 转发给调用方后提交最终 assistant 消息。中断的 delta
stream 以及已取消、被丢弃的 stream 不会持久化部分或未送达的 assistant 文本。

最终的 streaming `EventAfterModel` event 和 observation 会通过 `Duration` 携带整个
stream 的持续时间。如果模型在 done event 上报告 usage，同一组 token counts 也会写入
`TokenUsage`。最终 done event 还可能携带 `Finish.Reason`，例如 provider 的 stop
reason 或 tool-call finish reason。只要至少收到一个 delta，lifecycle telemetry 还会包含脱敏的
`StreamTelemetry`：time to first token、delta 数量、streamed delta 字节数和吞吐量。
Stream telemetry 不会包含 streamed text 或 tool arguments。

如果需要 start、first delta、done 和 error 的 observer-only stream lifecycle
telemetry，可以在 `RunStream` 调用上使用 `WithStreamObservations()`。该选项不会为
第一个 delta 之后的每个 delta 逐条发出 observations。

## Tool Calls

`RunStream` 会执行最终 done message 上的 tool calls。内置的 OpenAI-compatible、
OpenAI Responses 和 Anthropic Messages adapters 会把已支持的 streamed tool-call
形态归一化到这些 done messages，并且可能额外发出安全的
`StreamEventToolCallStart` 和 `StreamEventToolCallDone` 边界事件，方便 UI 展示状态。
Tool-call arguments 仍然只放在最终 done event 的 `event.Message.ToolCalls` 中，供
agent 执行工具；边界事件只携带安全 metadata。

只处理 delta、done 和 error 的现有调用方可以保持原有逻辑。建议忽略未知 stream event
类型，以便兼容后续新增的 metadata events。

如果 provider 在初始 streaming HTTP 请求阶段拒绝请求，`RunStream` 会立即返回结构化
provider error。如果 stream 已经开始，然后 provider 发出错误或无效事件，调用方会收到
携带安全 provider diagnostics 的 `StreamEventError`（当 diagnostics 可用时）。
