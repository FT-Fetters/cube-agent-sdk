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
includes `schema_version` and messages for persistence while keeping the
in-memory slice immutable to callers. Old JSON payloads that do not include
`schema_version` still unmarshal as the current schema.

`Restore` validates the snapshot schema and message roles before replacing the
managed context. Unsupported schema versions return `ErrSessionVersionMismatch`
wrapped in `SessionPersistenceError` with a `MigrationHint`.

## Session Stores

`SessionStore` is a public, dependency-free contract for durable session
records. SDK users can implement it for Redis, Postgres, S3, files, or any
other storage. `NewMemorySessionStore` provides a concurrency-safe in-memory
implementation for tests, examples, and reference behavior.

```go
store := agent.NewMemorySessionStore()

record := agent.NewSessionRecord("session-123", bot.Snapshot())
record.Metadata = map[string]string{"tenant": "acme"}
saved, err := store.SaveSession(ctx, record)
if err != nil {
	return err
}

loaded, err := store.LoadSession(ctx, saved.ID)
if err != nil {
	return err
}
if err := next.Restore(loaded.Snapshot); err != nil {
	return err
}
```

`SessionRecord` carries:

- `schema_version` for persisted record compatibility.
- `version` for optimistic save/update flows.
- `snapshot` with the conversation context only.
- safe string `metadata`, timestamps, and optional migration guidance.

Use `errors.Is` with `ErrSessionNotFound`, `ErrSessionVersionMismatch`, and
`ErrSessionInvalidRecord`. Use `errors.As` with `SessionPersistenceError` for
safe details such as session ID, expected version, schema version, and migration
hint.

## Event Logs

`SessionEventLog` is an append-only companion interface for application-level
session lifecycle records. The in-memory store implements it alongside
`SessionStore`.

```go
_, err = store.AppendSessionEvent(ctx, agent.SessionEvent{
	SessionID: saved.ID,
	Type:      agent.SessionEventRunStarted,
	RunID:     "run-1",
	Metadata:  map[string]string{"agent_id": next.ID()},
})
```

Events have stable IDs, monotonic sequences, schema metadata, optional run IDs,
and caller-approved string metadata. They intentionally do not carry raw
prompts, tool arguments, tool results, provider credentials, runtime config, or
raw telemetry payloads. Sequence conflicts return `ErrSessionEventConflict`.

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

Applications own durable storage adapters, encryption, access control,
retention policy, backup/restore behavior, and schema migrations for persisted
snapshots, session records, and event logs. Treat snapshots as user content
because normal session messages may contain sensitive user or product data.
