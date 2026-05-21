package agent

// Stable telemetry names are part of the SDK observability contract. Existing
// names should remain stable across compatible releases; add new names instead
// of changing or reusing an existing name for a different meaning.
const (
	TelemetryAttrEvent                       = "agent.event"
	TelemetryAttrFailed                      = "agent.failed"
	TelemetryAttrAgentID                     = "agent.id"
	TelemetryAttrRunID                       = "agent.run_id"
	TelemetryAttrSubagentID                  = "agent.subagent_id"
	TelemetryAttrTraceID                     = "agent.trace_id"
	TelemetryAttrSpanID                      = "agent.span_id"
	TelemetryAttrTraceState                  = "agent.trace_state"
	TelemetryAttrRequestID                   = "agent.request_id"
	TelemetryAttrParentRequestID             = "agent.parent_request_id"
	TelemetryAttrRound                       = "agent.round"
	TelemetryAttrDurationMS                  = "agent.duration_ms"
	TelemetryAttrEstimatedTokens             = "agent.estimated_tokens"
	TelemetryAttrTokensInput                 = "agent.tokens.input"
	TelemetryAttrTokensOutput                = "agent.tokens.output"
	TelemetryAttrTokensTotal                 = "agent.tokens.total"
	TelemetryAttrStreamTimeToFirstTokenMS    = "agent.stream.time_to_first_token_ms"
	TelemetryAttrStreamDeltaCount            = "agent.stream.delta_count"
	TelemetryAttrStreamByteCount             = "agent.stream.byte_count"
	TelemetryAttrStreamThroughputBytesPerSec = "agent.stream.throughput_bytes_per_second"
	TelemetryAttrToolName                    = "agent.tool.name"
	TelemetryAttrToolRisk                    = "agent.tool.risk"
	TelemetryAttrToolSchemaHash              = "agent.tool.schema_hash"
	TelemetryAttrToolTimingValidationMS      = "agent.tool.timing.validation_ms"
	TelemetryAttrToolTimingApprovalMS        = "agent.tool.timing.approval_ms"
	TelemetryAttrToolTimingExecutionMS       = "agent.tool.timing.execution_ms"
	TelemetryAttrToolResultContentBytes      = "agent.tool.result.content_bytes"
	TelemetryAttrToolResultMetadataKeys      = "agent.tool.result.metadata_keys"
	TelemetryAttrToolResultMCPIsError        = "agent.tool.result.mcp_is_error"
	TelemetryAttrSkillName                   = "agent.skill.name"
	TelemetryAttrApprovalApproved            = "agent.approval.approved"
	TelemetryAttrApprovalReason              = "agent.approval.reason"
	TelemetryAttrErrorCategory               = "agent.error.category"
	TelemetryAttrErrorModelSubcategory       = "agent.error.model_subcategory"
	TelemetryAttrProviderName                = "agent.provider.name"
	TelemetryAttrProviderHTTPStatus          = "agent.provider.http_status"
	TelemetryAttrProviderEndpointHost        = "agent.provider.endpoint_host"
	TelemetryAttrProviderRequestID           = "agent.provider.request_id"
	TelemetryAttrProviderRetryAfter          = "agent.provider.retry_after"
	TelemetryAttrProviderRateLimitLimit      = "agent.provider.rate_limit.limit"
	TelemetryAttrProviderRateLimitRemaining  = "agent.provider.rate_limit.remaining"
	TelemetryAttrProviderRateLimitReset      = "agent.provider.rate_limit.reset"
)

// Stable metric label names keep MetricsObserver output compatible with
// existing installations. Prefer only low-cardinality labels for backend
// metrics; use the agent.* attributes above for logs and traces.
const (
	TelemetryMetricLabelEvent                 = "event"
	TelemetryMetricLabelFailed                = "failed"
	TelemetryMetricLabelErrorCategory         = "error_category"
	TelemetryMetricLabelModelErrorSubcategory = "model_error_subcategory"
	TelemetryMetricLabelToolName              = "tool_name"
	TelemetryMetricLabelToolRisk              = "tool_risk"
	TelemetryMetricLabelProvider              = "provider"
	TelemetryMetricLabelHTTPStatus            = "http_status"
	TelemetryMetricLabelToolPhase             = "tool_phase"
)

var stableTelemetryAttributeNames = []string{
	TelemetryAttrEvent,
	TelemetryAttrFailed,
	TelemetryAttrAgentID,
	TelemetryAttrRunID,
	TelemetryAttrSubagentID,
	TelemetryAttrTraceID,
	TelemetryAttrSpanID,
	TelemetryAttrTraceState,
	TelemetryAttrRequestID,
	TelemetryAttrParentRequestID,
	TelemetryAttrRound,
	TelemetryAttrDurationMS,
	TelemetryAttrEstimatedTokens,
	TelemetryAttrTokensInput,
	TelemetryAttrTokensOutput,
	TelemetryAttrTokensTotal,
	TelemetryAttrStreamTimeToFirstTokenMS,
	TelemetryAttrStreamDeltaCount,
	TelemetryAttrStreamByteCount,
	TelemetryAttrStreamThroughputBytesPerSec,
	TelemetryAttrToolName,
	TelemetryAttrToolRisk,
	TelemetryAttrToolSchemaHash,
	TelemetryAttrToolTimingValidationMS,
	TelemetryAttrToolTimingApprovalMS,
	TelemetryAttrToolTimingExecutionMS,
	TelemetryAttrToolResultContentBytes,
	TelemetryAttrToolResultMetadataKeys,
	TelemetryAttrToolResultMCPIsError,
	TelemetryAttrSkillName,
	TelemetryAttrApprovalApproved,
	TelemetryAttrApprovalReason,
	TelemetryAttrErrorCategory,
	TelemetryAttrErrorModelSubcategory,
	TelemetryAttrProviderName,
	TelemetryAttrProviderHTTPStatus,
	TelemetryAttrProviderEndpointHost,
	TelemetryAttrProviderRequestID,
	TelemetryAttrProviderRetryAfter,
	TelemetryAttrProviderRateLimitLimit,
	TelemetryAttrProviderRateLimitRemaining,
	TelemetryAttrProviderRateLimitReset,
}

var lowCardinalityTelemetryAttributeNames = []string{
	TelemetryAttrEvent,
	TelemetryAttrFailed,
	TelemetryAttrToolRisk,
	TelemetryAttrToolResultMCPIsError,
	TelemetryAttrApprovalApproved,
	TelemetryAttrErrorCategory,
	TelemetryAttrErrorModelSubcategory,
	TelemetryAttrProviderName,
	TelemetryAttrProviderHTTPStatus,
}

var highCardinalityTelemetryAttributeNames = []string{
	TelemetryAttrAgentID,
	TelemetryAttrRunID,
	TelemetryAttrSubagentID,
	TelemetryAttrTraceID,
	TelemetryAttrSpanID,
	TelemetryAttrTraceState,
	TelemetryAttrRequestID,
	TelemetryAttrParentRequestID,
	TelemetryAttrToolName,
	TelemetryAttrToolSchemaHash,
	TelemetryAttrToolResultMetadataKeys,
	TelemetryAttrSkillName,
	TelemetryAttrApprovalReason,
	TelemetryAttrProviderEndpointHost,
	TelemetryAttrProviderRequestID,
	TelemetryAttrProviderRetryAfter,
	TelemetryAttrProviderRateLimitLimit,
	TelemetryAttrProviderRateLimitRemaining,
	TelemetryAttrProviderRateLimitReset,
}

var stableTelemetryMetricLabelNames = []string{
	TelemetryMetricLabelEvent,
	TelemetryMetricLabelFailed,
	TelemetryMetricLabelErrorCategory,
	TelemetryMetricLabelModelErrorSubcategory,
	TelemetryMetricLabelToolName,
	TelemetryMetricLabelToolRisk,
	TelemetryMetricLabelProvider,
	TelemetryMetricLabelHTTPStatus,
	TelemetryMetricLabelToolPhase,
}

var forbiddenTelemetryFieldNames = []string{
	"prompts",
	"message_content",
	"tool_arguments",
	"tool_result_content",
	"tool_result_metadata_values",
	"raw_errors",
	"credentials",
	"full_provider_urls",
	"mcp_environment_values",
}

// StableTelemetryAttributeNames returns the safe, stable agent.* attribute names
// used by logs, traces, custom observers, and documentation.
func StableTelemetryAttributeNames() []string {
	return append([]string(nil), stableTelemetryAttributeNames...)
}

// LowCardinalityTelemetryAttributeNames returns stable attributes that are safe
// to use as metric labels when the backend also allows their value domains.
func LowCardinalityTelemetryAttributeNames() []string {
	return append([]string(nil), lowCardinalityTelemetryAttributeNames...)
}

// HighCardinalityTelemetryAttributeNames returns safe attributes that should
// stay in logs or traces rather than default metric labels.
func HighCardinalityTelemetryAttributeNames() []string {
	return append([]string(nil), highCardinalityTelemetryAttributeNames...)
}

// StableTelemetryMetricLabelNames returns MetricsObserver's stable label names.
func StableTelemetryMetricLabelNames() []string {
	return append([]string(nil), stableTelemetryMetricLabelNames...)
}

// ForbiddenTelemetryFieldNames returns sensitive data classes that built-in
// observers must not emit as attributes, labels, or span events.
func ForbiddenTelemetryFieldNames() []string {
	return append([]string(nil), forbiddenTelemetryFieldNames...)
}
