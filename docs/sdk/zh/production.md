# 生产集成

SDK 有意把生产基础设施留在核心运行时之外。使用 SDK 原语，但在应用侧决定部署、
安全、遥测和存储策略。

本页聚焦生产观测。[可观测性](./observability.md) 文档说明精确的 SDK API 和稳定
遥测命名；本页则作为真实部署时的落地清单。

## 生产观测清单

生产启用 agent 前，逐项确认：

1. 先确定关联模型：用 `WithRunID` 传入应用 run ID，用 `WithTraceContext`
   附加上游 trace metadata；只有当平台要求自定义 request ID 格式时，才安装
   `WithRequestIDGenerator`。
2. 显式安装 `Observer`。默认值是 `NoopObserver`，所以只有应用接入后，生产日志、
   指标和 traces 才会出现。
3. 通过 `SlogObserver` 或只读取安全 `Observation` 表面的自定义 observer 导出结构化日志。
4. 通过 `MetricsObserver` 和应用自有的 `MetricSink` 导出 counters 与 duration
   histograms。
5. 在应用侧桥接 tracing，或复用无凭证的 `examples/opentelemetry` module；核心 SDK
   不导入 OpenTelemetry。
6. 决定指标是否必须精确。如果必须精确，把 `MetricsObserver` 放在
   `NewSamplingObserver` 外面，只采样详细日志或 traces。
7. 把高基数字段保留在日志和 traces 中，不要作为默认 metric labels。
8. 对模型失败率、provider 限流、工具失败、stream latency 和 telemetry 缺失配置告警。
9. 文档化隐私红线，并测试禁止字段不会进入 logs、metric labels、traces、span
   events 或 baggage。
10. 发布前运行本地 fake-provider 测试、OpenTelemetry 示例测试，以及可选 live-provider
    测试。

## 信号接入

Observers 是 best-effort 遥测 sink。它们接收脱敏的 `Observation`，不能拒绝或改变
执行结果。需要强制拦截时使用 hooks；需要遥测时使用 observers。

```go
slogObserver := agent.NewSlogObserver(agent.SlogObserverOptions{
	Logger:  slog.Default(),
	Level:   slog.LevelInfo,
	Message: "agent observation",
})

metricsObserver := agent.NewMetricsObserver(agent.MetricsObserverOptions{
	Sink: appMetricSink{},
})

// 保持聚合指标精确，再采样高流量的详细信号。
sampledDetails := agent.NewSamplingObserver(agent.SamplingObserverOptions{
	Child:                agent.Observers(slogObserver, appOpenTelemetryObserver),
	EventTypes:           []agent.EventType{agent.EventAfterModel, agent.EventAfterTool},
	Ratio:                0.1,
	AlwaysSampleFailures: true,
})

observer := agent.Observers(metricsObserver, sampledDetails)

bot, err := agent.New(cfg, model,
	agent.WithObserver(observer),
	agent.WithRequestIDGenerator(func(ctx agent.RequestIDContext) string {
		return fmt.Sprintf("%s.%s.%d", ctx.RunID, ctx.Operation, ctx.Sequence)
	}),
)
```

把上游 trace metadata 附加到请求 context，并与 SDK run ID 分开管理：

```go
ctx = agent.WithTraceContext(ctx, agent.TraceContext{
	TraceID:    upstreamTraceID,
	SpanID:     upstreamSpanID,
	TraceState: upstreamTraceState,
})

reply, err := bot.Run(ctx, input, agent.WithRunID(applicationRunID))
```

`RunID`、`RequestID`、`ParentRequestID` 和 `TraceContext` 是关联字段。它们可以安全
进入 logs 和 traces，但属于高基数字段，不应成为默认 metric labels。

## 日志、指标和 Trace

为每类信号指定明确的运维用途：

| 信号 | 推荐 SDK 来源 | 生产用途 |
| --- | --- | --- |
| 结构化日志 | `SlogObserver` 或基于 `Observation` 的自定义 observer | 请求还原、事故时间线、provider diagnostics 和单次 run 排查。 |
| 指标 | 带应用 `MetricSink` 的 `MetricsObserver` | counters、失败率、latency histograms、工具阶段 histograms 和告警输入。 |
| Traces | 应用侧 OpenTelemetry 桥接或 `examples/opentelemetry` | run/request/parent span 结构、跨服务关联、stream timing 和 tool timing。 |

日志和 traces 使用稳定的 `agent.*` attributes。指标使用稳定的 `MetricsObserver`
label 名称。指标表面有意更窄：默认不会包含 run IDs、request IDs、trace IDs、
provider request IDs、tool schema hashes、tool result metadata keys 或 MCP
environment values。

推荐映射：

- `agent.event`、`agent.failed`、`agent.error.category` 和
  `agent.error.model_subcategory` 用于错误仪表盘和告警。
- `agent.duration_ms` 和 `agent.tool.timing.*` 用于 latency histograms。
- `agent.stream.time_to_first_token_ms`、`agent.stream.delta_count`、
  `agent.stream.byte_count` 和 `agent.stream.throughput_bytes_per_second`
  用于解释 streaming 质量。
- `agent.provider.*` 字段用于诊断 provider 限流和故障，同时不会暴露请求体或凭证。
- `agent.run_id`、`agent.request_id`、`agent.parent_request_id` 和
  `agent.trace_id` 用于 logs 和 traces 关联。

## 采样策略

SDK 已经生成脱敏 observations 后，再采样详细遥测。不要把采样当作隐私边界。

推荐生产模式：

1. 后端成本允许时，保持低流量 counters 和 SLO histograms 精确。
2. 用 `NewSamplingObserver` 采样详细日志和 trace spans。
3. 使用 `AlwaysSampleFailures`，确保失败 observations 保持可见。
4. 必要时按 event type 采样。`EventAfterModel`、`EventAfterTool` 和 stream lifecycle
   observations 通常最适合保留详细数据。
5. 不同环境使用不同采样率。开发和 canary 可以 100%；高流量生产路径通常需要更低比例。
6. 流量或工具 catalog 变化后，重新评估采样率。

如果把 `MetricsObserver` 放到 sampling observer 后面，counters 和 histograms 只描述
被采样的 observations。这样可以控制成本，但必须是显式决策。

## 高基数字段

只有目标系统能承受时，才使用高基数字段。除非 metrics 后端有明确 cardinality budget，
否则把这些字段保留在 logs 和 traces 中：

- run IDs、request IDs、parent request IDs、trace IDs、span IDs 和 trace state；
- agent IDs、subagent IDs、provider request IDs、endpoint hosts、retry-after values
  和 rate-limit header values；
- 工具 catalog 动态时的 tool names；
- tool schema hashes、tool result metadata keys、skill names 和 approval reason text。

低基数字段通常可以作为 metric labels：event type、failed status、error category、
model error subcategory、tool risk、provider name、HTTP status 和 tool timing phase。
`tool_name` 需要按有界 label 对待：小型受控工具 catalog 中很有用；动态工具场景应移除
或分桶。

## 告警、SLO 和仪表盘

先定义用户影响面的 SLO，再补诊断仪表盘。

建议 SLO：

- Agent 可用性：成功 runs 除以尝试 runs；如果审批拒绝是产品预期行为，可从失败中排除。
- 端到端延迟：模型调用的 p50、p95、p99 `agent.duration_ms`，以及应用边界的总 run 延迟。
- 工具延迟：会调用下游系统的工具，监控 p95、p99 `agent.tool.timing.execution_ms`。
- Streaming 质量：`RunStream` 入口的 p95 time to first token 和 stream error rate。

建议告警：

- 按 `error_category` 或 `model_error_subcategory` 聚合的
  `agent_observation_failures_total` 比率升高。
- 按 provider 和 HTTP status 观察到 provider 429、408、5xx 突增。
- provider rate-limit remaining 接近零，或 retry-after header 反复出现。
- 发布后工具 validation failures 增加，可能代表 schema drift 或 model/tool contract 不匹配。
- write 或 destructive tools 的审批拒绝高于预期基线。
- Stream time to first token 或总 stream duration 超过 SLO。
- 活跃流量没有 observations，通常说明 observer wiring、sampling、exporter 或后端 ingest
  出现问题。

建议仪表盘：

- 总览：runs、failures、latency、token usage、active providers 和 active tool count。
- Provider health：status codes、retry-after、rate-limit headers、request IDs 和
  model error subcategories。
- Tool health：validation、approval、execution durations、error category、tool risk
  和受控 tool name 维度。
- Streaming：time to first token、delta count、byte count、throughput、stream errors，
  以及启用后的 stream lifecycle observations。
- 隐私审计：禁止字段缺失检查，以及意外高基数 metric labels 检查。

## Provider 诊断

内置 provider adapters 会在 provider 失败时附加安全 diagnostics。处理 SDK 错误时读取
`AgentError.ProviderDiagnostics`；直接检查模型适配器返回的错误时，调用
`ProviderDiagnosticsFromError`。

安全 provider diagnostics 可以包含：

- provider adapter name；
- HTTP status；
- 仅 endpoint host，不能是完整 URL；
- provider request ID；
- retry-after 和 rate-limit header values。

它们不得包含 request bodies、response bodies、prompt text、tool arguments、cookies、
credentials 或完整 provider URLs。事故响应时，记录 provider request ID 和 HTTP status，
再通过 provider console 或支持流程，在 provider 的访问控制下查看服务端详情。

## 流式输出和工具耗时

对于 streaming runs，最终 `EventAfterModel` observation 会把总 stream duration 放在
`Duration`；如果最终 done event 报告 provider `TokenUsage`，也会携带该 usage；并携带脱敏
`StreamTelemetry`：

- time to first token；
- delta count；
- streamed byte count；
- bytes-per-second throughput。

`RunStream` 默认不发出 stream lifecycle observations。需要 observer-only 的
`EventStreamStart`、`EventStreamFirstDelta`、`EventStreamDone` 或 `EventStreamError`
时，在单次调用上增加 `WithStreamObservations()`。SDK 只发出 first-delta lifecycle
observation，不会为每个 delta 发 observation。

after-tool observations 会携带 `ToolTiming`：

- validation duration 表示本地 schema 和参数校验成本；
- approval duration 表示人类或业务审批等待时间；
- execution duration 表示实际工具调用成本。

用这些阶段判断事故属于 model/tool contract drift、审批流程延迟，还是工具调用的下游服务。
工具结果元数据只包含 content byte count、metadata key names 和 MCP `mcpIsError` 状态；
不会包含工具结果内容或 metadata values。

## 隐私和红线

`Observation` 被设计成安全遥测表面，但如果应用把原始输入补充到记录里，生产 observer
仍可能变得不安全。不要把以下类别映射到 logs、metric labels、traces、span events 或
baggage。`ForbiddenTelemetryFieldNames()` 会返回同一份策略清单，供测试使用：

- `prompts`
- `message_content`
- `tool_arguments`
- `tool_result_content`
- `tool_result_metadata_values`
- `raw_errors`
- `credentials`
- `full_provider_urls`
- `mcp_environment_values`

操作规则：

- 在导出前脱敏，不要只依赖后端 UI 隐藏。
- 不要把 prompt 或 tool payloads 放进 OpenTelemetry baggage。
- 不要记录 raw provider errors；使用 `AgentError` categories 和 provider diagnostics。
- 不要把原始工具结果内容写入通用遥测。产品数据应进入带产品访问控制的产品存储。
- 把 session snapshots 当作用户内容处理，配置存储加密、访问控制和保留策略。

## Live Test 和本地验证

常规发布 gate 使用本地测试：

```bash
go test ./... -skip '^TestLiveAPIModelRun$'
go test -race ./...
go vet ./...
go test -count=1 ./...
go -C examples/opentelemetry test ./...
```

Provider adapters 使用 fake HTTP servers 测试，MCP integrations 使用 fake stdio
processes 测试。生产发布前，用低风险账号和模型运行可选 live-provider test：

```bash
MODEL_API_TYPE=anthropic-messages \
MODEL_BASE_URL=https://api.anthropic.com \
MODEL_API_KEY="<your-api-key>" \
MODEL_NAME=claude-sonnet-4-6 \
go test -v -run '^TestLiveAPIModelRun$' .
```

配置不完整时 live test 会 skip。不要在 live diagnostics 中使用生产客户 prompts、真实
tool arguments 或长期凭证。verbose logs 必须保持脱敏；任何凭证一旦出现在 secret
storage 之外，都应轮换。

## 故障排查 Runbook

生产观测缺失或难以解释时，按这个顺序排查：

1. 没有日志：确认已安装 `WithObserver`，`SlogObserver` 使用了进程会输出的 logger 和
   level，且没有被零采样率隐藏。
2. 没有指标：确认 `MetricSink` 已接入；如果期望精确指标，observer 位于 sampling 之外；
   后端接受稳定 label names。
3. 没有 traces：确认 OpenTelemetry bridge 收到了 observations，trace metadata 已通过
   `WithTraceContext` 附加，exporter 正常 flush。
4. 关联断开：对比 `RunID`、`RequestID`、`ParentRequestID` 和 `TraceContext`。自定义
   request ID generator 必须返回非空 ID。
5. Provider 失败不透明：检查 `AgentError` category、model subcategory、HTTP status、
   provider request ID、retry-after 和 rate-limit diagnostics。
6. 工具调用慢：把 `ToolTiming` 拆成 validation、approval 和 execution，定位责任方。
7. Stream 体感慢：比较总 duration、time to first token、delta count、byte count 和
   throughput；需要生命周期细节时，只在一个请求路径上启用 `WithStreamObservations()`。
8. 指标成本突增：查找高基数 labels，尤其是 tool names、request IDs、provider request IDs、
   schema hashes 和 metadata keys。
9. 遥测中出现敏感数据：停止 exporter、保留审计证据、移除不安全 enrichment、轮换暴露凭证，
   并用 `ForbiddenTelemetryFieldNames()` 增加回归测试。
