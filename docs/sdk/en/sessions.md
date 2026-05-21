# Sessions

Session APIs manage the conversation context owned by an agent. They do not
change the configured model, tools, approval policy, hooks, observer, skills,
MCP servers, or compactor.

## Reset

```go
bot.Reset()
```

`Reset` clears the managed conversation context.

## Snapshot and Restore

```go
snapshot := bot.Snapshot()
payload, err := json.Marshal(snapshot)
if err != nil {
	return err
}

var restored agent.SessionSnapshot
if err := json.Unmarshal(payload, &restored); err != nil {
	return err
}

next, err := agent.New(cfg, model)
if err != nil {
	return err
}
if err := next.Restore(restored); err != nil {
	return err
}
```

`Snapshot` returns an isolated copy of the current conversation. Its JSON shape
includes messages for persistence while keeping the in-memory slice immutable to
callers.

## Fork

```go
branch, err := bot.Fork("what-if")
if err != nil {
	return err
}
```

`Fork` creates a separate agent with copied context and copied capability
registries. Later messages are isolated between the original and forked agents.

## Application Responsibilities

Applications own durable storage, access control, encryption, retention policy,
and schema migration for persisted snapshots.
