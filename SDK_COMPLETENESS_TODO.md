# Cube Agent SDK 完备性完善 Todo

> 状态规则：`[ ]` 待实施，`[~]` 实施中，`[x]` 已完成并通过复审。

## 执行约束

- 每个任务先由实施 subagent 完成代码、测试和文档更新。
- 每个任务完成后，由独立 review subagent 验收完善内容和测试证据。
- 如 review 不通过，同一实施 subagent 继续修复，再次复审，直到通过。
- 任务通过复审后，才将状态标记为完成。
- 所有新增行为需要测试覆盖；生产代码变更应先补对应测试。

## Todo 清单

- [x] **任务 1：工具 schema 与参数校验**
  - 完善内容：为工具描述补充结构化参数 schema、必填参数、参数校验入口和校验错误；让模型请求能拿到可直接用于 function/tool calling 的工具定义。
  - 验收目标：
    - `ToolDescriptor` 能表达参数 schema。
    - 工具调用执行前会校验参数，校验失败返回可识别错误且不调用工具函数。
    - 现有无 schema 工具保持兼容。
    - 覆盖合法参数、缺少必填参数、类型不匹配、无 schema 兼容路径。

- [x] **任务 2：OpenAI-compatible 模型适配器**
  - 完善内容：提供基于标准库的 OpenAI-compatible chat completions 适配器，支持 base URL、API key、model、HTTP client、工具定义映射和 tool call 解析。
  - 验收目标：
    - SDK 用户可通过配置直接创建模型适配器，不需要自己实现 `Model`。
    - 请求包含 system prompt、历史消息、工具 schema。
    - 能解析普通 assistant 回复和 tool calls。
    - 使用本地 `httptest` 覆盖请求体、鉴权 header、响应解析、非 2xx 错误。

- [x] **任务 3：流式输出接口**
  - 完善内容：增加流式模型接口和 agent 流式运行 API，支持 assistant 增量文本事件、完成事件、错误事件，并在模型不支持流式时给出明确错误。
  - 验收目标：
    - 用户能通过流式 API 逐段消费输出。
    - 流式事件包含 agent ID、内容增量和最终消息。
    - 流式运行会写回最终 assistant 消息到上下文。
    - 覆盖正常流式、模型不支持流式、流式中断错误。

- [x] **任务 4：MCP stdio 客户端与工具桥接**
  - 完善内容：提供最小可用的 MCP stdio 客户端，支持启动进程、initialize、tools/list、tools/call，并将 MCP tools 暴露为 SDK `Tool`。
  - 验收目标：
    - 能从 MCP server 发现工具并注册到 agent。
    - 能通过 agent 工具调用桥接到 MCP `tools/call`。
    - 进程退出、JSON-RPC 错误、未知工具有明确错误。
    - 使用测试内 fake MCP server 覆盖发现、调用和错误路径。

- [x] **任务 5：会话状态管理**
  - 完善内容：补充 reset、snapshot、restore、fork 等上下文管理能力，方便业务持久化和恢复会话。
  - 验收目标：
    - 可清空当前上下文。
    - 可导出不可变快照并恢复到 agent。
    - 可从当前 agent 派生独立副本，后续消息互不影响。
    - 覆盖深拷贝隔离和恢复后继续运行。

- [x] **任务 6：结构化错误与生命周期事件增强**
  - 完善内容：细化模型、工具、审批、schema、MCP、压缩、子 agent 等错误类型；事件补充 request ID、轮次、耗时、token 估算和错误分类。
  - 验收目标：
    - 调用方可用 `errors.Is` 或 `errors.As` 识别主要错误类别。
    - hooks 能拿到足够的审计字段。
    - 不破坏现有 sentinel error 兼容性。
    - 覆盖错误包装、事件字段填充和兼容性。

- [x] **任务 7：生产级观测接口**
  - 完善内容：增加轻量 logger、tracer/metrics 接口或 hook 辅助实现，记录模型调用、工具调用、审批、压缩、子 agent 消息和耗时。
  - 验收目标：
    - 用户可通过 option 注入观测实现。
    - 默认无依赖、无输出。
    - 观测数据不泄漏敏感参数，支持按事件采集必要元数据。
    - 覆盖默认 no-op、自定义 recorder、错误路径。

- [x] **任务 8：更安全的审批策略**
  - 完善内容：补充 deny-by-default、按工具名 allowlist、只读/写入风险标记、审批原因记录；文档建议生产环境使用安全策略。
  - 验收目标：
    - SDK 提供可组合审批策略。
    - 未授权工具被拒绝且不执行。
    - 审批结果能被 hook/观测记录。
    - 覆盖 allow、deny、组合策略、原因传递。

- [x] **任务 9：示例和文档完善**
  - 完善内容：补充可运行示例和 README 章节，覆盖 OpenAI-compatible 模型、工具 schema、流式输出、MCP stdio、状态恢复、审批策略和观测。
  - 验收目标：
    - 示例可编译或可通过测试验证。
    - README 给出最小可运行路径和生产集成建议。
    - 文档明确哪些能力由 SDK 提供，哪些由应用方接入。

- [x] **任务 10：发布工程与最终验收**
  - 完善内容：补充 CI、许可证、变更日志、贡献说明和最终质量门禁。
  - 验收目标：
    - CI 至少运行 `go test ./...`、`go vet ./...`。
    - 项目包含许可证、变更日志和贡献说明。
    - 最终执行 `go test ./...`、`go test -race ./...`、`go vet ./...` 全部通过。
    - 清单中所有任务已完成并通过复审。

## 实施记录

- 任务 1：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 2：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 3：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 4：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 5：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 6：初审发现失败路径审计字段不足，已返工；复审通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 7：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 8：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 9：已完成，独立 review 通过；本地 `go test ./...` 与 `go vet ./...` 通过。
- 任务 10：已完成，独立 review 通过；本地 `go test ./...`、`go test -race ./...` 与 `go vet ./...` 通过。
- 全部任务：已完成并通过复审。
