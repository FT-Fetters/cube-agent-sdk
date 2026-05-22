# Cube Agent SDK Feature Roadmap Checklist

> 状态规则：`[ ]` 待评估或待实施，`[~]` 实施中，`[x]` 已完成并通过验证。
> 该清单聚焦 SDK 从“核心可用”走向“生产好用、稳定可扩展”的后续能力。

## Checklist

- [x] **1. 内置 provider 流式支持**
  - 为 OpenAI-compatible、OpenAI Responses、Anthropic Messages 等内置适配器补充真实流式输出能力。
  - 验收目标：
    - 内置 provider 可直接用于 `RunStream`。
    - 能正确输出 delta、done、error 事件。
    - 流式 token usage、首 token 延迟和错误诊断能进入现有 observability 体系。
    - 对不支持流式的 provider 或模型返回明确错误。

- [x] **2. 模型调用可靠性中间件**
  - 提供可组合的模型 wrapper，用于 timeout、retry、backoff、rate limit、circuit breaker 和 token/cost budget 控制。
  - 验收目标：
    - 用户可通过标准 wrapper 包装任意 `Model` 或 `StreamModel`。
    - retry 只针对安全、可重试的错误类别触发。
    - backoff、最大重试次数、总耗时和预算限制可配置。
    - 观测事件能区分原始模型失败、重试、最终失败和预算拒绝。

- [x] **3. 持久化 session 与 event log 接口**
  - 在现有 snapshot/restore 基础上，补充轻量 `SessionStore` 或 append-only run event log 契约。
  - 验收目标：
    - SDK 提供接口和内存实现，不绑定具体数据库。
    - 应用可接入 Redis、Postgres、S3 或文件存储。
    - 支持 session 版本号、迁移提示、恢复校验和错误分类。
    - 不持久化敏感凭据，文档明确加密、访问控制和保留策略由应用负责。

- [x] **4. 更完整的 MCP runtime**
  - 扩展 MCP 支持，从 stdio 工具桥接走向 HTTP/SSE client、连接管理、工具刷新和健康检查。
  - 验收目标：
    - 支持 MCP HTTP/SSE 工具发现和调用。
    - stdio、HTTP、SSE 共享一致的错误分类和 tool bridge 行为。
    - 支持 server health、连接重试、工具列表刷新和关闭清理。
    - MCP 环境变量、URL 参数、工具参数和工具结果继续满足 redaction 约束。

- [x] **5. Tool schema 生成与更丰富校验**
  - 提供 Go struct 到 tool schema 的生成工具，并扩展当前轻量 JSON Schema 子集。
  - 验收目标：
    - 可从结构体 tag 生成工具参数 schema。
    - 支持 enum、default、min/max、pattern、additionalProperties 等常见约束。
    - schema 生成和运行时校验保持一致。
    - 错误信息能指出具体参数路径，且不会泄漏敏感参数值。

- [ ] **6. Agent eval 与 replay 测试工具**
  - 提供测试 harness，帮助 SDK 使用者录制、回放和断言 agent 行为。
  - 验收目标：
    - 支持 scripted fake model、golden transcript、tool call 断言和 observation 断言。
    - 支持从已保存 run/event log 中 replay。
    - 测试输出稳定，适合 CI 使用。
    - 文档给出典型 eval 用例：工具选择、审批拒绝、模型错误、上下文压缩和流式失败。

- [ ] **7. Provider capability matrix**
  - 为模型适配器补充能力声明，描述 tools、streaming、JSON mode、reasoning、parallel tool calls、MCP 等支持情况。
  - 验收目标：
    - provider 可暴露 `Capabilities` 类信息。
    - agent 能在运行前检测不兼容配置并返回清晰错误。
    - 文档列出内置 provider 的能力矩阵。
    - 能支持应用按能力选择模型或进行降级。

- [ ] **8. 安全边界强化**
  - 在现有审批策略基础上，补充工具级超时、并发限制、结果大小限制、作用域控制和审计能力。
  - 验收目标：
    - 工具可配置 timeout、最大并发、最大结果大小和风险级别。
    - write/destructive 工具能绑定更细粒度的作用域或业务审批原因。
    - 观测事件包含安全审计所需的非敏感元数据。
    - 对 MCP 工具、文件工具、网络工具有明确的生产安全建议。

## Suggested Priority

推荐优先级：

1. 内置 provider 流式支持。
2. 模型调用可靠性中间件。
3. Agent eval 与 replay 测试工具。
4. Tool schema 生成与更丰富校验。
5. 持久化 session 与 event log 接口。
6. 更完整的 MCP runtime。
7. Provider capability matrix。
8. 安全边界强化。

优先级理由：前五项能最快提升真实项目接入体验和生产稳定性；后三项更适合在工具生态、MCP 接入和多 provider 场景逐步扩大后推进。
