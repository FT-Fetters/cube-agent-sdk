# Agent Runtime

`Agent` is the SDK runtime. It owns the managed conversation context, prompt
assembly, model calls, tool execution, approvals, hooks, observers, compaction,
skills, MCP metadata, and subagent registry.

## Construction

```go
bot, err := agent.New(agent.Config{
	ID:            "support-agent",
	SystemPrompt:  "You are a careful support agent.",
	MaxToolRounds: 4,
}, model,
	agent.WithTools(lookupTool),
	agent.WithApprovalPolicy(agent.AllowToolsApproval("lookup_account")),
)
```

`New` requires a non-nil `Model`. If `Config.ID` is empty, the SDK assigns an
agent ID. If `MaxToolRounds` is zero, the SDK defaults it to four rounds.

## Managed Context

Every `Run` appends the user input to the managed message history. The final
assistant message is appended when the model returns without tool calls. Tool
loops append assistant tool-call messages and tool result messages before the
next model round.

Useful context APIs:

- `AppendMessage` imports an existing message.
- `Messages` returns a deep copy of managed context.
- `Reset` clears context without changing model or capabilities.
- `Snapshot`, `Restore`, and `Fork` support persistence and branching.

## Run Lifecycle

1. Append user input.
2. Resolve active skills from persistent activation, run options, inline markers,
   and trigger phrases.
3. Compact context if configured thresholds are exceeded.
4. Build a `ModelRequest` with system prompt, messages, tools, MCP servers, and
   active skills.
5. Emit before/after model events.
6. If the model returns tool calls, validate arguments, run approval, execute
   tools, append tool results, and call the model again.
7. Return the final assistant message.

## Instruction Files

Use `WithInstructionFiles` to load additional local instruction files into the
system prompt at construction time.

```go
bot, err := agent.New(cfg, model,
	agent.WithInstructionFiles("AGENTS.md"),
)
```

The files are read once during construction. Updating the file later does not
change an already-created agent.
