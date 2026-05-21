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

## 脱敏元数据

事件和 observations 携带 event type、agent ID、run ID、trace ID、span ID、
trace state、subagent ID、request ID、parent request ID、round、duration、
estimated tokens、真实 token usage、streaming telemetry、tool name、tool risk、
approval result、skill name、error category、model error subcategory，以及模型失败时的安全
provider diagnostics 等审计字段。`ParentRequestID` 会把工具和审批事件关联到触发它们的模型请求，也会关联同一 run 内的后续模型请求。

`EstimatedTokens` 是 SDK 在请求侧估算的 token 数，即使 provider 没有返回 usage
也会继续填充。`TokenUsage` 则携带非 streaming `EventAfterModel` 及其 observation
中来自 `ModelResponse.Usage` 的真实 input、output 和 total tokens。如果没有可用的
usage，`TokenUsage` 字段保持零值。

对于 streaming `EventAfterModel` 记录，`Duration` 表示整个 stream 的持续时间。
`StreamTelemetry` 会在至少收到一个 delta 时携带 time to first token、delta 数量、
streamed delta 字节数和 bytes-per-second 吞吐量。如果 stream 在第一个 delta 之前失败，
time to first token 和 stream 计数字段会保持零值，而 `Duration` 仍会记录失败 stream 的持续时间。

Observations 有意省略消息内容、工具参数、工具结果、原始错误、API keys、带
query string 的完整 provider URL 和 MCP 环境变量。

`SlogObserver` 每条记录都会输出 `event` 和 `failed`。其他零值字段会被省略；
duration 以 `duration_ms` 输出；token usage、stream telemetry、工具元数据、审批元数据和
provider diagnostics 会作为结构化 group 输出。

`MetricsObserver` 会为每条 observation 递增 `agent_observations_total`，为失败
observation 递增 `agent_observation_failures_total`，并把正数 duration 记录到
`agent_observation_duration`。指标标签限定为 `event`、`failed`、
`error_category`、`model_error_subcategory`、`tool_name`、`tool_risk`、
`provider` 和存在时的 `http_status`。默认不会把 run ID、request ID、trace ID
或 provider request ID 放入指标标签。
