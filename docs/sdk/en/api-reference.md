# API Reference

This page groups the exported SDK surface by area. Use Go documentation for
method-level details while keeping this page as a navigation aid.

## Core Runtime

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

## Messages and Models

- `Role`, `RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool`
- `Message`
- `Model`
- `StreamModel`
- `ModelRequest`
- `ModelResponse`
- `TokenUsage`
- `StreamEvent`
- `StreamEventType`
- `StreamEventDelta`, `StreamEventDone`, `StreamEventError`

## Model Adapters

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

## Tools and Schemas

- `Tool`
- `ToolFunc`
- `ToolCall`
- `ToolResult`
- `ToolDescriptor`
- `ToolParametersSchema`
- `ToolParametersSchemaProvider`
- `ToolRiskProvider`
- `ToolRisk`
- `ToolRiskRead`, `ToolRiskWrite`, `ToolRiskDestructive`, `ToolRiskUnspecified`
- `SchemaType`
- `SchemaTypeString`, `SchemaTypeNumber`, `SchemaTypeInteger`,
  `SchemaTypeBoolean`, `SchemaTypeObject`, `SchemaTypeArray`
- `ToolValidationError`

## Approvals

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
- `MCPTransportStdio`, `MCPTransportSSE`, `MCPTransportHTTP`
- `WithMCPServers`
- `StartMCPStdioClient`
- `MCPStdioClient`
- `MCPRPCError`
- `MCPToolDescriptor`
- `MCPContent`
- `MCPToolCallResult`

## Sessions and Subagents

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

## Skills and Compaction

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

## Observability

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
- `StreamTelemetry`
- `Observation`
- `ObservationFromEvent`
- `WithObserver`
- `TraceContext`
- `WithTraceContext`
- `TraceContextFromContext`

## Errors

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
