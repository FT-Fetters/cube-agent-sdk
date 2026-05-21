# 生产集成

SDK 有意把生产基础设施留在核心运行时之外。使用 SDK 原语，但在应用侧决定部署、
安全、遥测和存储策略。

## 集成清单

1. 创建模型适配器，并明确 timeout、retry、request logging 和 provider 特定错误
   映射。
2. 从部署环境加载 credentials、base URLs、MCP command paths 和 secrets。
3. 只注册 agent 所需工具，并为工具配置 schema 和风险标签。
4. 安装默认拒绝的审批策略，并把 `ApprovalFunc` 连接到人类或业务审批流程。
5. 挂载 `Observer`，把脱敏元数据导出到日志、指标或 tracing 系统。
6. 在应用拥有的存储中持久化 `SessionSnapshot`，并配置访问控制和保留策略。
7. 用应用进程监管和最小权限运行外部 MCP servers。
8. 在 agent 入口周围增加速率限制、provider quota 和发布控制。
9. 除非产品明确需要且已做好保护，不要把原始工具参数、工具结果、模型内容和
   provider 错误写入通用遥测。

## 安全说明

- 生产工具访问优先使用 allowlist，而不是 denylist。
- 把 destructive tools 当成需要显式审批的独立流程。
- 尽量使用短期凭证。
- 用 context 和 timeout 约束模型与工具执行。
- 审查 session snapshot 保留策略，因为 snapshot 可能包含用户内容。
