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
- `WithStreamObservations`

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

## Model Reliability

- `NewReliableModel`
- `ReliableModelOption`
- `WithReliableMaxAttempts`
- `WithReliableBackoff`
- `WithReliablePerAttemptTimeout`
- `WithReliableTotalTimeout`
- `WithReliableRateLimit`
- `WithReliableCircuitBreaker`
- `WithReliableTokenBudget`
- `WithReliableCostBudget`
- `WithReliableTokenCounter`
- `WithReliabilityObserver`
- `ReliableBackoffFunc`
- `ReliabilityObserver`
- `ReliabilityEvent`
- `ReliabilityEventType`
- `ReliabilityOperation`

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

- `CurrentSessionSchemaVersion`
- `SessionSnapshot`
- `NewSessionSnapshot`
- `ValidateSessionSnapshot`
- `SessionRecord`
- `NewSessionRecord`
- `ValidateSessionRecord`
- `SessionStore`
- `SessionEventLog`
- `SessionEvent`
- `SessionEventType`
- `SessionEventRunStarted`, `SessionEventRunCompleted`,
  `SessionEventRunFailed`, `SessionEventSnapshotSaved`
- `MemorySessionStore`
- `NewMemorySessionStore`
- `SessionPersistenceError`
- `ErrSessionNotFound`
- `ErrSessionVersionMismatch`
- `ErrSessionInvalidRecord`
- `ErrSessionEventConflict`
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
- `RequestIDGenerator`
- `RequestIDContext`
- `WithRequestIDGenerator`
- `TraceContext`
- `WithTraceContext`
- `TraceContextFromContext`

`Event` and `Observation` include `ToolSchemaHash` on tool and approval
lifecycle records when the registered tool has a parameter schema.
After-tool observations include `ToolResultMetadata` with result content byte
size, sorted result metadata key names, and MCP `mcpIsError` status when
present.
After-tool observations also include `ToolTiming`, a `ToolLifecycleTiming`
value that separates validation, approval, and execution durations while
`Duration` remains the total tool lifecycle duration.
`TelemetryAttr*` constants define the stable `agent.*` log/trace/custom
observer attribute names. `TelemetryMetricLabel*` constants define the stable
`MetricsObserver` label names. The list helpers return copies for tests,
documentation, and integrations that need drift detection.
`WithRequestIDGenerator` lets applications generate request IDs from safe
metadata such as agent ID, run ID, trace metadata, event type, operation,
sequence, round, parent request ID, tool name, and subagent ID. Empty generator
results fall back to the default request ID format.

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
- `ErrReliableRateLimited`
- `ErrReliableCircuitOpen`
- `ErrReliableBudgetExceeded`
