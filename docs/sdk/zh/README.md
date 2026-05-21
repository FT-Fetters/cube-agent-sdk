# Cube Agent SDK

Cube Agent SDK 是一个小型 Go SDK，用于构建带托管会话状态、工具、审批、
流式输出、MCP 集成、会话快照、hooks、observers、skills、压缩和子 agent
能力的 agent。

SDK 有意把模型凭证、外部进程部署、人类审批 UI、持久化存储和遥测导出器
留在核心运行时之外。应用通过 Go 接口和选项把这些部分接入。

## 文档导航

- [快速开始](./getting-started.md)：安装、测试和运行最小 agent。
- [Agent 运行时](./agent-runtime.md)：`Agent` 如何管理上下文和模型调用。
- [模型](./models.md)：内置适配器和自定义模型实现。
- [工具](./tools.md)：本地函数、schema、校验和工具风险。
- [流式输出](./streaming.md)：通过 `RunStream` 消费增量 assistant 输出。
- [MCP](./mcp.md)：MCP server 元数据和 stdio 工具桥接。
- [会话](./sessions.md)：reset、snapshot、restore 和 fork。
- [审批](./approvals.md)：内置策略和生产审批流程。
- [可观测性](./observability.md)：hooks、observers 和脱敏遥测。
- [错误处理](./errors.md)：哨兵错误和结构化 `AgentError`。
- [Skills](./skills.md)：可复用指令包和激活路径。
- [压缩](./compaction.md)：上下文阈值检查和摘要。
- [子 Agent](./subagents.md)：由父 agent 控制的子 agent。
- [示例](./examples.md)：本地示例和可选 live API 示例。
- [生产集成](./production.md)：真实部署时的集成清单。
- [API 参考](./api-reference.md)：按领域分组的导出 SDK 表面。

## SDK 提供什么

- Prompt 组装、消息历史和 active skill 注入。
- 模型和流式模型接口。
- OpenAI-compatible chat completions、OpenAI Responses 和 Anthropic Messages
  适配器。
- 工具描述、工具参数 schema 和执行前校验。
- MCP stdio client 和 MCP 到工具的桥接。
- 会话 reset、snapshot、restore 和 fork。
- 审批策略、hooks、observers 和脱敏生命周期元数据。
- 压缩和子 agent 编排原语。

## 应用提供什么

- Provider 账号、模型 ID、base URL、API key、重试策略和网络控制。
- MCP server 二进制文件、部署、进程监管和运行权限。
- 人类审批 UI 或业务策略集成。
- 快照的持久化存储、加密、保留策略和迁移。
- 日志、指标和 tracing 导出器。
- 密钥管理、速率限制、发布策略和生产监控。
