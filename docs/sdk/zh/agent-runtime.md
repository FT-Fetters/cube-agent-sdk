# Agent 运行时

`Agent` 是 SDK 运行时。它拥有托管会话上下文、prompt 组装、模型调用、工具
执行、审批、hooks、observers、压缩、skills、MCP 元数据和子 agent 注册表。

## 构造

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

`New` 要求传入非 nil 的 `Model`。如果 `Config.ID` 为空，SDK 会分配 agent ID。
如果 `MaxToolRounds` 为零，SDK 默认使用四轮工具调用上限。

## 托管上下文

每次 `Run` 都会把用户输入追加到托管消息历史。模型不再返回 tool call 时，最终
assistant 消息会被追加。工具循环会在下一轮模型调用前追加 assistant tool-call
消息和 tool result 消息。

常用上下文 API：

- `AppendMessage` 导入已有消息。
- `Messages` 返回托管上下文的深拷贝。
- `Reset` 清空上下文，不改变模型或能力。
- `Snapshot`、`Restore` 和 `Fork` 支持持久化和分支。

## Run 生命周期

1. 追加用户输入。
2. 从持久激活、run option、内联标记和触发短语解析 active skills。
3. 如果配置的阈值被超过，则压缩上下文。
4. 构造包含 system prompt、messages、tools、MCP servers 和 active skills 的
   `ModelRequest`。
5. 发出 before/after model 事件。
6. 如果模型返回 tool calls，则校验参数、执行审批、调用工具、追加结果并再次
   调用模型。
7. 返回最终 assistant 消息。

## 指令文件

使用 `WithInstructionFiles` 在构造时把额外本地指令文件加载进 system prompt。

```go
bot, err := agent.New(cfg, model,
	agent.WithInstructionFiles("AGENTS.md"),
)
```

文件只会在构造时读取一次。之后更新文件不会改变已创建的 agent。
