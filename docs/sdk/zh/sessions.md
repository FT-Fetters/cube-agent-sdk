# 会话

会话 API 管理由 agent 拥有的对话上下文。它们不会改变已配置的模型、工具、
审批策略、hooks、observer、skills、MCP servers 或 compactor。

## Reset

```go
bot.Reset()
```

`Reset` 清空托管会话上下文。

## Snapshot 和 Restore

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

`Snapshot` 返回当前会话的隔离副本。它的 JSON 形状包含 `schema_version` 和
messages，便于持久化；同时内存中的 slice 不会直接暴露给调用方修改。不包含
`schema_version` 的旧 JSON payload 仍会按当前 schema 反序列化。

`Restore` 会先校验 snapshot schema 和 message role，再替换托管上下文。不支持的
schema version 会返回包裹在 `SessionPersistenceError` 中的
`ErrSessionVersionMismatch`，并携带 `MigrationHint`。

## Session Stores

`SessionStore` 是公开、无额外依赖的持久化契约。SDK 使用者可以基于 Redis、
Postgres、S3、文件或其他存储实现它。`NewMemorySessionStore` 提供并发安全的
内存实现，适合测试、示例和参考行为。

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

`SessionRecord` 包含：

- `schema_version`，用于持久化 record 兼容性。
- `version`，用于乐观保存和更新流程。
- `snapshot`，只包含对话上下文。
- 安全的字符串 `metadata`、时间戳和可选迁移提示。

使用 `errors.Is` 判断 `ErrSessionNotFound`、`ErrSessionVersionMismatch` 和
`ErrSessionInvalidRecord`。需要安全细节时，用 `errors.As` 取出
`SessionPersistenceError`，读取 session ID、期望 version、schema version 和
migration hint。

## Event Logs

`SessionEventLog` 是应用层会话生命周期记录的 append-only companion interface。
内存 store 同时实现了它和 `SessionStore`。

```go
_, err = store.AppendSessionEvent(ctx, agent.SessionEvent{
	SessionID: saved.ID,
	Type:      agent.SessionEventRunStarted,
	RunID:     "run-1",
	Metadata:  map[string]string{"agent_id": next.ID()},
})
```

Event 拥有稳定 ID、单调递增 sequence、schema metadata、可选 run ID，以及调用方
明确允许的字符串 metadata。它们有意不携带原始 prompt、tool arguments、tool
results、provider credentials、runtime config 或 raw telemetry payload。sequence
冲突会返回 `ErrSessionEventConflict`。

## Fork

```go
branch, err := bot.Fork("what-if")
if err != nil {
	return err
}
```

`Fork` 创建一个独立 agent，复制上下文和能力注册表。之后原 agent 和 fork 出来的
agent 的消息互相隔离。

## 应用职责

应用负责为持久化 snapshots、session records 和 event logs 提供存储适配器、
加密、访问控制、保留策略、备份恢复行为和 schema 迁移。请把 snapshots 当作
用户内容处理，因为正常 session messages 可能包含敏感的用户或产品数据。
