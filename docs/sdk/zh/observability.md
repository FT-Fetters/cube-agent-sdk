# 可观测性

SDK 暴露两个生命周期扩展点：

- Hooks 可以观察事件，并通过返回错误拒绝操作。
- Observers 接收脱敏遥测，不能改变执行结果。

## Hooks

```go
hook := func(ctx context.Context, event agent.Event) error {
	if event.Type == agent.EventBeforeTool && event.ToolRisk == agent.ToolRiskDestructive {
		return fmt.Errorf("destructive tools require a separate workflow")
	}
	return nil
}

bot, err := agent.New(cfg, model, agent.WithHook(hook))
```

Hooks 接收模型调用、审批、工具、压缩、skill 激活和 subagent 消息对应的 `Event`。

每次 `Run` 和 `RunStream` 都有一个 run ID，同一次调用发出的所有生命周期事件
共享它。可以传入 `agent.WithRunID("trace-123")` 使用应用自己的 trace ID；
否则 SDK 会基于 agent ID 和本地序列生成非空 ID。

当应用同时需要 run ID 和外部 trace ID 时，应把它们作为不同字段使用。可以把
trace 元数据附加到 context：

```go
ctx = agent.WithTraceContext(ctx, agent.TraceContext{
	TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
	SpanID:     "00f067aa0ba902b7",
	TraceState: "vendor=state",
})
```

SDK 会把 `TraceID`、`SpanID` 和 `TraceState` 传播到 events、observations 和
`AgentError`。如果没有传入 `WithRunID`，SDK 仍会生成 run ID，而不会用
`TraceID` 替代它。

## Request IDs

默认 request ID 保持现有 `<agent-id>-request-<sequence>` 格式。如果应用需要
request ID 符合上游日志或 tracing 规范，可以安装 `WithRequestIDGenerator`：

```go
bot, err := agent.New(cfg, model,
	agent.WithRequestIDGenerator(func(ctx agent.RequestIDContext) string {
		return fmt.Sprintf("%s.%s.%d", ctx.RunID, ctx.Operation, ctx.Sequence)
	}),
)
```

`RequestIDContext` 只包含安全关联字段：agent ID、run ID、trace metadata、event
type、operation、本地 sequence、round、parent request ID、tool name 和
subagent ID。它不会包含 prompts、message content、tool arguments、tool results、
raw errors、credentials、provider URL 或 MCP settings。

生成器应返回非空 ID。如果返回空字符串、只包含空白字符，或发生 panic，SDK 会对该
request 回退到默认 ID。向 `WithRequestIDGenerator` 传入 nil generator 会返回配置错误。
SDK 不会对自定义 ID 去重；如果生成器多次返回同一个值，observations 和
`ParentRequestID` 会原样保留该值。

## Observers

```go
observer := agent.ObserverFunc(func(ctx context.Context, observation agent.Observation) {
	log.Printf("type=%s request=%s parent=%s round=%d failed=%v",
		observation.Type,
		observation.RequestID,
		observation.ParentRequestID,
		observation.Round,
		observation.Failed,
	)
	if observation.Type == agent.EventAfterTool {
		timing := observation.ToolTiming
		log.Printf("tool_timing validation=%s approval=%s execution=%s",
			timing.Validation,
			timing.Approval,
			timing.Execution,
		)
	}
})

bot, err := agent.New(cfg, model, agent.WithObserver(observer))
```

如需使用标准库结构化日志，可以显式配置 `SlogObserver`：

```go
slogObserver := agent.NewSlogObserver(agent.SlogObserverOptions{
	Logger:  slog.Default(),
	Level:   slog.LevelInfo,
	Message: "agent observation",
})

bot, err := agent.New(cfg, model, agent.WithObserver(slogObserver))
```

如需接入指标系统，可以在应用中实现 `MetricSink` 并挂载 `MetricsObserver`：

```go
type appMetricSink struct{}

func (appMetricSink) AddCounter(ctx context.Context, name string, delta int64, labels []agent.MetricLabel) {
	// Forward the counter update to your metrics backend.
}

func (appMetricSink) RecordDuration(ctx context.Context, name string, duration time.Duration, labels []agent.MetricLabel) {
	// Forward the duration to your metrics backend or histogram.
}

metricsObserver := agent.NewMetricsObserver(agent.MetricsObserverOptions{
	Sink: appMetricSink{},
})

bot, err := agent.New(cfg, model, agent.WithObserver(metricsObserver))
```

可以使用 `Observers` 或 `MultiObserver` 把脱敏 observations 分发给多个 observer：

```go
combined := agent.Observers(slogObserver, metricsObserver)

bot, err := agent.New(cfg, model, agent.WithObserver(combined))
```

可以用 `NewSamplingObserver` 包装任意 observer，在保持遥测字段脱敏的同时降低
observation 数量：

```go
sampled := agent.NewSamplingObserver(agent.SamplingObserverOptions{
	Child:                combined,
	EventTypes:           []agent.EventType{agent.EventAfterModel, agent.EventAfterTool},
	FailureStatus:        agent.SampleAllObservations,
	Ratio:                0.1,
	AlwaysSampleFailures: true,
})

bot, err := agent.New(cfg, model, agent.WithObserver(sampled))
```

`EventTypes` 非空时按 event type 过滤，`FailureStatus` 可以只保留失败或成功的
observations，`Ratio` 会应用到符合条件的 observations。`AlwaysSampleFailures`
会在 ratio 很低时仍保留符合条件的失败 observations。nil `Child` 会让 sampling
observer 成为 no-op。默认 ratio sampler 是确定性的，并且只哈希脱敏后的
`Observation` 字段；如果测试或部署需要调用方控制决策，可以使用
`ObservationSampler` 或 `ObservationSamplerFunc`。

nil 子 observer 会被忽略。Observer panic 会被 recover 并忽略，包括 fan-out group
内部的 panic，因此一个子 observer 不会阻止后续子 observer 收到 observation。
遥测是 best-effort，不能改变 agent 行为。默认 observer 仍是 `NoopObserver`；只有应用通过
`WithObserver` 挂载 `SlogObserver` 时才会输出 slog 日志；只有应用挂载带 sink 的
`MetricsObserver` 时才会输出指标。

## OpenTelemetry 集成

核心 SDK 不导入也不要求 OpenTelemetry。使用 OpenTelemetry 的应用可以在自己的
module 中把脱敏的 `Observation` 表面桥接到 tracing 系统。

`examples/opentelemetry` 下的无凭证示例是一个单独的 Go module，OpenTelemetry
依赖只存在于该示例中：

```bash
go -C examples/opentelemetry test ./...
go -C examples/opentelemetry run .
```

该示例把 observations 映射为 OpenTelemetry spans、span events 和 attributes，
覆盖 run/request/parent 关联、event type、agent ID、tool name 和 risk、duration、
error category、token usage、streaming telemetry、tool lifecycle timing、
tool schema hash、安全的工具结果元数据，以及安全 provider diagnostics。它不会映射
prompts、message content、tool arguments、tool result content、tool result
metadata values、raw errors、credentials、完整 provider URL 或 MCP environment
values。

## 遥测属性命名

稳定命名表面在代码中由 `TelemetryAttr*` 常量和
`StableTelemetryAttributeNames()` 提供。日志、traces、自定义 observers 和
OpenTelemetry 示例应优先使用带 `agent.*` 前缀的点号属性名。现有
`SlogObserver` 的 snake_case 字段和结构化 group 会作为兼容别名保留，但新的集成
应读取 `agent.*` 字段。`MetricsObserver` 的 label 名称也是稳定的，但会保持现有
snake_case 名称，因为 metric 名本身已经带有 agent 语义。

这些名称属于 SDK 可观测性契约。兼容版本可以新增 attribute 或 label，但不应在
major-version 兼容性破坏之外删除、重命名或改变现有名称的含义。

| Attribute | 信号 | 基数 | 说明 |
| --- | --- | --- | --- |
| `agent.event` | logs、traces、metrics 中为 `event` | 低 | SDK event type。 |
| `agent.failed` | logs、traces、metrics 中为 `failed` | 低 | 布尔失败状态。 |
| `agent.id` | logs、traces | 高 | Agent 标识。 |
| `agent.run_id` | logs、traces | 高 | Run 关联 ID。 |
| `agent.subagent_id` | logs、traces | 高 | Subagent 标识。 |
| `agent.trace_id` | logs、traces | 高 | 调用方提供的 trace ID。 |
| `agent.span_id` | logs、traces | 高 | 调用方提供的 span ID。 |
| `agent.trace_state` | logs、traces | 高 | 调用方提供的 trace state。 |
| `agent.request_id` | logs、traces | 高 | Request 关联 ID。 |
| `agent.parent_request_id` | logs、traces | 高 | Parent request 关联 ID。 |
| `agent.round` | logs、traces | 数值 | 模型/工具轮次。 |
| `agent.duration_ms` | logs、traces | 数值 | Observation duration，单位毫秒。 |
| `agent.estimated_tokens` | logs、traces | 数值 | SDK 请求侧估算。 |
| `agent.tokens.input` | logs、traces | 数值 | Provider 返回的 input tokens。 |
| `agent.tokens.output` | logs、traces | 数值 | Provider 返回的 output tokens。 |
| `agent.tokens.total` | logs、traces | 数值 | Provider 返回的 total tokens。 |
| `agent.stream.time_to_first_token_ms` | logs、traces | 数值 | Streaming 首个 delta 耗时。 |
| `agent.stream.delta_count` | logs、traces | 数值 | Streamed delta 数量。 |
| `agent.stream.byte_count` | logs、traces | 数值 | Streamed delta 字节数。 |
| `agent.stream.throughput_bytes_per_second` | logs、traces | 数值 | Stream 吞吐量。 |
| `agent.tool.name` | logs、traces、metrics 中为 `tool_name` | 有界/高 | 注册工具名；用于 metrics 时应确保工具集合有界。 |
| `agent.tool.risk` | logs、traces、metrics 中为 `tool_risk` | 低 | Tool risk label。 |
| `agent.tool.schema_hash` | logs、traces | 高 | 安全的 schema drift 标识。 |
| `agent.tool.timing.validation_ms` | logs、traces | 数值 | 工具参数校验耗时。 |
| `agent.tool.timing.approval_ms` | logs、traces | 数值 | 审批等待耗时。 |
| `agent.tool.timing.execution_ms` | logs、traces | 数值 | 工具执行耗时。 |
| `agent.tool.timeout_configured` | logs、traces | 低 | 是否配置了工具 timeout。 |
| `agent.tool.timeout_ms` | logs、traces | 数值 | 已配置工具 timeout，单位毫秒。 |
| `agent.tool.max_concurrency` | logs、traces | 数值 | 已配置的单 agent 工具并发限制。 |
| `agent.tool.max_result_bytes` | logs、traces | 数值 | 已配置的工具结果内容字节限制。 |
| `agent.tool.scope.count` | logs、traces | 数值 | 已配置 tool scopes 数量。 |
| `agent.tool.scope.hash` | logs、traces | 高 | scope kind/value 对的 hash，不包含原始 value。 |
| `agent.tool.business_reason.hash` | logs、traces | 高 | 工具业务原因的 hash。 |
| `agent.tool.result.content_bytes` | logs、traces | 数值 | 工具结果内容字节长度。 |
| `agent.tool.result.metadata_keys` | logs、traces | 高 | 仅 metadata key 名称，不含 value。 |
| `agent.tool.result.mcp_is_error` | logs、traces | 低 | MCP result error 标志。 |
| `agent.skill.name` | logs、traces | 高 | 激活的 skill 名称。 |
| `agent.approval.approved` | logs、traces | 低 | 审批结果。 |
| `agent.approval.reason` | logs、traces | 高 | 审批原因文本。 |
| `agent.error.category` | logs、traces、metrics 中为 `error_category` | 低 | 安全 error category。 |
| `agent.error.model_subcategory` | logs、traces、metrics 中为 `model_error_subcategory` | 低 | 安全 model error subcategory。 |
| `agent.provider.name` | logs、traces、metrics 中为 `provider` | 低 | Provider adapter 名称。 |
| `agent.provider.http_status` | logs、traces、metrics 中为 `http_status` | 低 | HTTP status code。 |
| `agent.provider.endpoint_host` | logs、traces | 高 | 只包含 host，不包含完整 URL。 |
| `agent.provider.request_id` | logs、traces | 高 | Provider request ID。 |
| `agent.provider.retry_after` | logs、traces | 高 | Retry header 值。 |
| `agent.provider.rate_limit.limit` | logs、traces | 高 | Provider rate limit header 值。 |
| `agent.provider.rate_limit.remaining` | logs、traces | 高 | Provider remaining quota header 值。 |
| `agent.provider.rate_limit.reset` | logs、traces | 高 | Provider reset header 值。 |

`StableTelemetryMetricLabelNames()` 返回内置 metric label 名称：`event`、
`failed`、`error_category`、`model_error_subcategory`、`tool_name`、
`tool_risk`、`provider`、`http_status` 和 `tool_phase`。默认 metrics 不会包含
run ID、request ID、trace ID、span ID、trace state、provider request ID、tool
schema hash、tool scope hashes、tool business-reason hashes、工具结果 metadata keys、工具结果 metadata values、原始 scope value、工具业务原因或 MCP environment
values。`tool_name` 应按有界标签使用：工具集合受控时它很有用；如果工具 catalog
是动态高基数，就不应作为后端 label。

不要把 prompts、message content、tool arguments、tool result content、tool
result metadata values、raw errors、credentials、完整 provider URLs、MCP
environment values、tool scope values 或 tool business reasons 映射到日志、metric labels、traces、span events 或 baggage。
`ForbiddenTelemetryFieldNames()` 会返回这份策略清单，供测试和文档引用。

## 脱敏元数据

事件和 observations 携带 event type、agent ID、run ID、trace ID、span ID、
trace state、subagent ID、request ID、parent request ID、round、duration、
estimated tokens、真实 token usage、streaming telemetry、tool name、tool risk、
tool schema hash、tool lifecycle timing、tool safety 审计元数据、approval result、skill name、error category、
model error subcategory，安全的工具结果元数据，以及模型失败时的安全 provider
diagnostics 等审计字段。
`ParentRequestID` 会把工具和审批事件关联到触发它们的模型请求，也会关联同一 run 内的后续模型请求。

当工具提供参数 schema 时，工具和审批生命周期记录会包含 `ToolSchemaHash`。该 hash
会基于参数 schema 和描述符元数据确定性生成，不包含工具参数、工具结果、prompt 或原始
schema JSON。没有参数 schema 的工具会保持该字段为空。

当工具声明限制、scope 或业务原因时，工具生命周期记录也会包含 `ToolSafety` 审计元数据。该元数据包括已配置 timeout、max concurrency、max result bytes、scope count、scope hash 和 business-reason hash，不包含原始 scope value 或原始业务原因。

after-tool observations 会包含 `ToolResultMetadata`，其中包括工具结果内容的字节数、
排序后的结果 metadata key 名称，以及存在时的 MCP `mcpIsError` 状态。它不会包含
工具结果内容、metadata value、结构化 MCP content value、工具参数、原始错误或 secrets。

after-tool observations 还会包含 `ToolTiming`，用于区分 validation、approval 和
execution 三段耗时。未到达的阶段保持零值，`Duration` 仍表示整个工具生命周期总耗时。
这些字段只包含 duration，不包含工具参数、工具结果、metadata value、原始错误、prompts
或 credentials。

`EstimatedTokens` 是 SDK 在请求侧估算的 token 数，即使 provider 没有返回 usage
也会继续填充。`TokenUsage` 则携带非 streaming `EventAfterModel` 中来自
`ModelResponse.Usage` 的真实 input、output 和 total tokens，也会携带 streaming
`EventAfterModel` 中来自最终 `StreamEvent.Usage` 的真实 usage。如果没有可用的
usage，`TokenUsage` 字段保持零值。

对于 streaming `EventAfterModel` 记录，`Duration` 表示整个 stream 的持续时间。
`StreamTelemetry` 会在至少收到一个 delta 时携带 time to first token、delta 数量、
streamed delta 字节数和 bytes-per-second 吞吐量。如果 stream 在第一个 delta 之前失败，
time to first token 和 stream 计数字段会保持零值，而 `Duration` 仍会记录失败 stream 的持续时间。

`RunStream` 默认不会发出 stream lifecycle observations。可以在单次 `RunStream`
调用上使用 `WithStreamObservations()`，额外发出 observer-only 的
`EventStreamStart`、`EventStreamFirstDelta`、`EventStreamDone` 和
`EventStreamError` observations。只有第一个 delta 会被观察；后续 delta 不会逐条发出
observations。

Observations 有意省略 prompts、message content、tool arguments、tool result
content、tool result metadata values、raw errors、credentials、带 query string
的完整 provider URL 和 MCP environment values。

`SlogObserver` 每条记录都会输出 `agent.event` 和 `agent.failed`，并保留 legacy
`event` 和 `failed` 别名。其他零值字段会被省略；duration 以
`agent.duration_ms` 输出；同时为了兼容保留 legacy grouped token usage、stream
telemetry、工具元数据、`tool.timing`、审批元数据和 provider diagnostics。

`MetricsObserver` 会为每条 observation 递增 `agent_observations_total`，为失败
observation 递增 `agent_observation_failures_total`，并把正数 duration 记录到
`agent_observation_duration`。正数工具生命周期分段会记录到
`agent_tool_lifecycle_duration`，并使用 `validation`、`approval` 或 `execution`
作为低基数 `tool_phase` 标签；这些分段指标不会使用 `tool_name`，以避免高基数标签。
通用 observation 指标标签限定为 `event`、`failed`、`error_category`、
`model_error_subcategory`、`tool_name`、`tool_risk`、`provider` 和存在时的
`http_status`，工具 timing 分段指标只使用低基数标签。默认不会把 run ID、request ID、
trace ID、provider request ID、`ToolSchemaHash`、工具结果 metadata keys 或 MCP
result status 放入指标标签。
