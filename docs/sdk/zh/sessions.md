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

`Snapshot` 返回当前会话的隔离副本。它的 JSON 形状包含 messages，便于持久化；
同时内存中的 slice 不会直接暴露给调用方修改。

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

应用负责持久化存储、访问控制、加密、保留策略和持久化快照的 schema 迁移。
