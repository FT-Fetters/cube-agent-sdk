# API 参考

本页按领域整理导出的 SDK 表面。方法级细节以 Go documentation 为准，本页作为
导航辅助。

## 核心运行时

- `New`
- `Agent`
- `Config`
- `Option`
- `RunOption`
- `WithInstructionFiles`
- `AppendMessage`
- `Messages`
- `Run`
- `RunStream`
- `WithRunID`
- `WithStreamObservations`

## Messages 和 Models

- `Role`、`RoleSystem`、`RoleUser`、`RoleAssistant`、`RoleTool`
- `Message`
- `Model`
- `StreamModel`
- `ModelRequest`
- `ModelResponse`
- `TokenUsage`
- `StreamEvent`
- `StreamEventType`
- `StreamEventDelta`、`StreamEventDone`、`StreamEventError`

## 模型适配器

- `NewModel`
- `ModelConfig`
- `ModelAPIType`
- `ModelAPIOpenAICompatible`
- `ModelAPIOpenAIResponses`
- `ModelAPIAnthropicMessages`
- `NewOpenAICompatibleModel`
- `OpenAICompatibleConfig`
- `OpenAICompatibleModel`
- `NewOpenAIResponsesModel`
- `OpenAIResponsesConfig`
- `OpenAIResponsesModel`
- `NewAnthropicMessagesModel`
- `AnthropicMessagesConfig`
- `AnthropicMessagesModel`

## 工具和 Schemas

- `Tool`
- `ToolFunc`
- `ToolCall`
- `ToolResult`
- `ToolDescriptor`
- `ToolParametersSchema`
- `ToolParametersSchemaProvider`
- `ToolRiskProvider`
- `ToolRisk`
- `ToolRiskRead`、`ToolRiskWrite`、`ToolRiskDestructive`、`ToolRiskUnspecified`
- `SchemaType`
- `SchemaTypeString`、`SchemaTypeNumber`、`SchemaTypeInteger`、
  `SchemaTypeBoolean`、`SchemaTypeObject`、`SchemaTypeArray`
- `ToolValidationError`

## 审批

- `ApprovalPolicy`
- `ApprovalFunc`
- `ApprovalRequest`
- `ApprovalDecision`
- `AllowAllApproval`
- `DenyAllApproval`
- `AllowToolsApproval`
- `DenyToolsApproval`
- `AllowRisksApproval`
- `RequireAllApprovals`
- `WithApprovalPolicy`

## MCP

- `MCPServerConfig`
- `MCPTransport`
- `MCPTransportStdio`、`MCPTransportSSE`、`MCPTransportHTTP`
- `WithMCPServers`
- `StartMCPStdioClient`
- `MCPStdioClient`
- `MCPRPCError`
- `MCPToolDescriptor`
- `MCPContent`
- `MCPToolCallResult`

## 会话和子 Agent

- `SessionSnapshot`
- `NewSessionSnapshot`
- `Reset`
- `Snapshot`
- `Restore`
- `Fork`
- `SubagentOptions`
- `SubagentMessage`
- `SpawnSubagent`
- `SendMessageToSubagent`
- `SendToParent`
- `DrainSubagentMessages`

## Skills 和压缩

- `Skill`
- `SkillMatcher`
- `LoadSkills`
- `WithSkills`
- `WithRunSkills`
- `ActivateSkill`
- `DeactivateSkill`
- `HasSkill`
- `CompactConfig`
- `Compactor`
- `SummaryCompactor`
- `ModelCompactor`
- `TokenCounter`
- `TokenCounterFunc`
- `ApproxTokenCounter`
- `WithCompactor`
- `WithTokenCounter`

## 可观测性

- `Hook`
- `Event`
- `EventType`
- `WithHook`
- `Observer`
- `ObserverFunc`
- `NoopObserver`
- `MultiObserver`
- `Observers`
- `SamplingObserver`
- `SamplingObserverOptions`
- `SamplingFailureStatus`
- `SampleAllObservations`
- `SampleFailedObservations`
- `SampleSuccessfulObservations`
- `ObservationSampler`
- `ObservationSamplerFunc`
- `NewSamplingObserver`
- `SlogObserver`
- `SlogObserverOptions`
- `NewSlogObserver`
- `MetricLabel`
- `MetricSink`
- `MetricsObserver`
- `MetricsObserverOptions`
- `NewMetricsObserver`
- `DefaultMetricsEventCounterName`
- `DefaultMetricsFailureCounterName`
- `DefaultMetricsDurationName`
- `DefaultMetricsToolLifecycleDurationName`
- `TelemetryAttrEvent`
- `TelemetryAttrFailed`
- `TelemetryAttrAgentID`
- `TelemetryAttrRunID`
- `TelemetryAttrSubagentID`
- `TelemetryAttrTraceID`
- `TelemetryAttrSpanID`
- `TelemetryAttrTraceState`
- `TelemetryAttrRequestID`
- `TelemetryAttrParentRequestID`
- `TelemetryAttrRound`
- `TelemetryAttrDurationMS`
- `TelemetryAttrEstimatedTokens`
- `TelemetryAttrTokensInput`
- `TelemetryAttrTokensOutput`
- `TelemetryAttrTokensTotal`
- `TelemetryAttrStreamTimeToFirstTokenMS`
- `TelemetryAttrStreamDeltaCount`
- `TelemetryAttrStreamByteCount`
- `TelemetryAttrStreamThroughputBytesPerSec`
- `TelemetryAttrToolName`
- `TelemetryAttrToolRisk`
- `TelemetryAttrToolSchemaHash`
- `TelemetryAttrToolTimingValidationMS`
- `TelemetryAttrToolTimingApprovalMS`
- `TelemetryAttrToolTimingExecutionMS`
- `TelemetryAttrToolResultContentBytes`
- `TelemetryAttrToolResultMetadataKeys`
- `TelemetryAttrToolResultMCPIsError`
- `TelemetryAttrSkillName`
- `TelemetryAttrApprovalApproved`
- `TelemetryAttrApprovalReason`
- `TelemetryAttrErrorCategory`
- `TelemetryAttrErrorModelSubcategory`
- `TelemetryAttrProviderName`
- `TelemetryAttrProviderHTTPStatus`
- `TelemetryAttrProviderEndpointHost`
- `TelemetryAttrProviderRequestID`
- `TelemetryAttrProviderRetryAfter`
- `TelemetryAttrProviderRateLimitLimit`
- `TelemetryAttrProviderRateLimitRemaining`
- `TelemetryAttrProviderRateLimitReset`
- `TelemetryMetricLabelEvent`
- `TelemetryMetricLabelFailed`
- `TelemetryMetricLabelErrorCategory`
- `TelemetryMetricLabelModelErrorSubcategory`
- `TelemetryMetricLabelToolName`
- `TelemetryMetricLabelToolRisk`
- `TelemetryMetricLabelProvider`
- `TelemetryMetricLabelHTTPStatus`
- `TelemetryMetricLabelToolPhase`
- `StableTelemetryAttributeNames`
- `LowCardinalityTelemetryAttributeNames`
- `HighCardinalityTelemetryAttributeNames`
- `StableTelemetryMetricLabelNames`
- `ForbiddenTelemetryFieldNames`
- `StreamTelemetry`
- `ToolLifecycleTiming`
- `ToolResultMetadata`
- `Observation`
- `ObservationFromEvent`
- `WithObserver`
- `TraceContext`
- `WithTraceContext`
- `TraceContextFromContext`

当注册工具提供参数 schema 时，工具和审批生命周期的 `Event` 与 `Observation`
会包含 `ToolSchemaHash`。
after-tool observations 会包含 `ToolResultMetadata`，其中包括结果内容字节数、
排序后的结果 metadata key 名称，以及存在时的 MCP `mcpIsError` 状态。
after-tool observations 也会包含 `ToolTiming`，它是 `ToolLifecycleTiming`
值，用于区分 validation、approval 和 execution 耗时；`Duration` 仍表示整个工具生命周期总耗时。
`TelemetryAttr*` 常量定义稳定的 `agent.*` 日志、trace 和自定义 observer 属性名。
`TelemetryMetricLabel*` 常量定义稳定的 `MetricsObserver` label 名称。清单 helper
会返回副本，供测试、文档和需要漂移检测的集成使用。

## 错误

- `AgentError`
- `ErrorCategory`
- `ModelErrorSubcategory`
- `ModelErrorSubcategoryFromError`
- `ProviderDiagnosticsFromError`
- `ErrApprovalDenied`
- `ErrToolNotFound`
- `ErrToolValidation`
- `ErrMaxToolRoundsExceeded`
- `ErrStreamingUnsupported`
- `ErrStreamingToolCallsUnsupported`
- `ErrMCPProcessExited`
- `ErrMCPRPC`
- `ErrMCPToolNotFound`
- `ErrSubagentNotFound`
- `ErrModelAPIUnsupported`
