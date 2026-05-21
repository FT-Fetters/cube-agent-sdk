# Subagents

Parent agents can spawn child agents and choose which capabilities are
inherited. Subagents are useful when an application wants separate context,
system prompts, model choices, or capability boundaries.

## Spawn a Subagent

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

If `Model` is nil, the child uses the parent model. If `SystemPrompt` is empty,
the child uses the parent system prompt.

## Send Messages

```go
reply, err := master.SendMessageToSubagent(ctx, worker.ID(), "Implement the next task.")
if err != nil {
	return err
}
_ = reply
```

Child agents can send messages to the parent inbox:

```go
if err := worker.SendToParent(ctx, "Finished the assigned task."); err != nil {
	return err
}
messages := master.DrainSubagentMessages(worker.ID())
```

## Inheritance Controls

`SubagentOptions` can inherit all or selected tools, MCP servers, skills, hooks,
and instruction files. It can also attach extra tools, skills, and MCP servers
to the child.

Missing subagents return errors compatible with `ErrSubagentNotFound`.
