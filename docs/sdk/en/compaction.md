# Compaction

Compaction reduces long managed contexts before model calls. Configure it on
`Config`.

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

When `MaxTokens` is set and the estimated context exceeds the threshold, the SDK
calls the configured `Compactor`.

## Token Counting

The default `ApproxTokenCounter` is dependency-free and suitable for threshold
checks. Use `WithTokenCounter` to install an application-specific counter.

```go
counter := agent.TokenCounterFunc(func(message agent.Message) int {
	return len(strings.Fields(message.Content))
})

bot, err := agent.New(cfg, model, agent.WithTokenCounter(counter))
```

## Built-In Compactors

`SummaryCompactor` creates a deterministic local summary placeholder and keeps
recent messages.

`ModelCompactor` asks a model to summarize older context and keeps recent
messages intact.

```go
bot, err := agent.New(cfg, chatModel,
	agent.WithCompactor(agent.ModelCompactor{
		Model:        summaryModel,
		SystemPrompt: "Summarize context for the next agent turn.",
		KeepLast:     8,
	}),
)
```

Applications should choose summary prompts and retention rules that match their
product and compliance requirements.
