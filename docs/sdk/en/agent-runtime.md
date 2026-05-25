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

## Concurrent Runs

An `Agent` owns one managed conversation timeline. Prefer one active `Run` or
`RunStream` per agent. The SDK serializes overlapping calls on the same agent so
message history remains deterministic, but serialization is a safety guard, not a
parallelism model.

A `RunStream` stays active until its returned channel is drained or its context is
canceled. If another call starts on the same agent while a stream is active, it
waits for that stream lifecycle to finish before appending input or building a
model request. Waiting calls return a structured `AgentError` with operation
`run.acquire` if their context is canceled before the run slot is available.

Hooks, approval policies, and tools receive the active run context. Calling
`Run` or `RunStream` on the same agent with that context returns a structured
`AgentError` with operation `run.active` instead of blocking the outer callback.

For parallel conversations, isolate state by using `Fork` or by creating separate
agents from persisted session snapshots. Each forked or session-restored agent
owns its own context ordering.

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
