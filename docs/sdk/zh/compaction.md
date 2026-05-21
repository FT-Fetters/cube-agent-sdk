# 压缩

压缩会在模型调用前缩短过长的托管上下文。通过 `Config` 配置。

```go
cfg := agent.Config{
	SystemPrompt: "You are a focused coding agent.",
	Compact: agent.CompactConfig{
		MaxTokens: 200000,
		Threshold: 0.8,
		KeepLast:  8,
	},
}
```

当设置了 `MaxTokens`，且估算上下文超过阈值时，SDK 会调用配置的 `Compactor`。

## Token 计数

默认 `ApproxTokenCounter` 无依赖，适合阈值检查。使用 `WithTokenCounter` 安装应用
自己的计数器。

```go
counter := agent.TokenCounterFunc(func(message agent.Message) int {
	return len(strings.Fields(message.Content))
})

bot, err := agent.New(cfg, model, agent.WithTokenCounter(counter))
```

## 内置 Compactors

`SummaryCompactor` 生成确定性的本地摘要占位，并保留最近消息。

`ModelCompactor` 请求模型摘要较早上下文，并保留最近消息。

```go
bot, err := agent.New(cfg, chatModel,
	agent.WithCompactor(agent.ModelCompactor{
		Model:        summaryModel,
		SystemPrompt: "Summarize context for the next agent turn.",
		KeepLast:     8,
	}),
)
```

应用应根据产品和合规要求选择摘要 prompt 与保留规则。
