# 子 Agent

父 agent 可以创建子 agent，并选择继承哪些能力。当应用需要独立上下文、system
prompt、模型选择或能力边界时，subagents 很有用。

## 创建子 Agent

```go
worker, err := master.SpawnSubagent(ctx, agent.SubagentOptions{
	ID:                "worker-1",
	SystemPrompt:      "You are a focused implementation worker.",
	Model:             workerModel,
	InheritToolNames:  []string{"read_file", "run_tests"},
	InheritSkillNames: []string{"review"},
	InheritMCP:        true,
})
if err != nil {
	return err
}
```

如果 `Model` 为 nil，子 agent 使用父 agent 的模型。如果 `SystemPrompt` 为空，子
agent 使用父 agent 的 system prompt。

## 发送消息

```go
reply, err := master.SendMessageToSubagent(ctx, worker.ID(), "Implement the next task.")
if err != nil {
	return err
}
_ = reply
```

子 agent 可以向父 agent inbox 发送消息：

```go
if err := worker.SendToParent(ctx, "Finished the assigned task."); err != nil {
	return err
}
messages := master.DrainSubagentMessages(worker.ID())
```

## 继承控制

`SubagentOptions` 可以继承全部或指定的 tools、MCP servers、skills、hooks 和
instruction files。也可以给子 agent 额外挂载 tools、skills 和 MCP servers。

缺失 subagent 时返回的错误可通过 `ErrSubagentNotFound` 识别。
