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
- `Observation`
- `ObservationFromEvent`
- `WithObserver`
- `TraceContext`
- `WithTraceContext`
- `TraceContextFromContext`

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
