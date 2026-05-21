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

## Observers

```go
observer := agent.ObserverFunc(func(ctx context.Context, observation agent.Observation) {
	log.Printf("type=%s request=%s round=%d failed=%v",
		observation.Type,
		observation.RequestID,
		observation.Round,
		observation.Failed,
	)
})

bot, err := agent.New(cfg, model, agent.WithObserver(observer))
```

Observer panic 会被 recover 并忽略。遥测是 best-effort，不能改变 agent 行为。

## 脱敏元数据

事件和 observations 携带 event type、agent ID、subagent ID、request ID、round、
duration、estimated tokens、tool name、tool risk、approval result、skill name 和
error category 等审计字段。

Observations 有意省略消息内容、工具参数、工具结果、原始错误、API keys 和 MCP
环境变量。
