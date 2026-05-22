package agent

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

const defaultSlogObserverMessage = "agent observation"

// SlogObserverOptions configures a standard-library slog observer.
type SlogObserverOptions struct {
	// Logger receives structured observation records. If nil, slog.Default is used.
	Logger *slog.Logger
	// Level is the slog level used for every observation. The zero value is info.
	Level slog.Level
	// Message is the slog record message. If empty, "agent observation" is used.
	Message string
}

// SlogObserver logs sanitized lifecycle telemetry with Go's standard log/slog
// package. It emits stable agent.* fields, keeps legacy field aliases for
// compatibility, and omits other zero-value attributes.
type SlogObserver struct {
	logger  *slog.Logger
	level   slog.Level
	message string
}

// NewSlogObserver returns an Observer backed by Go's standard log/slog package.
// The observer only logs fields present on Observation and does not log prompts,
// message content, tool arguments, tool result content or metadata values, raw
// errors, API keys, full URLs, or MCP environment values.
func NewSlogObserver(options SlogObserverOptions) SlogObserver {
	message := options.Message
	if message == "" {
		message = defaultSlogObserverMessage
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return SlogObserver{
		logger:  logger,
		level:   options.Level,
		message: message,
	}
}

func (o SlogObserver) Observe(ctx context.Context, observation Observation) {
	logger := o.logger
	if logger == nil {
		logger = slog.Default()
	}
	message := o.message
	if message == "" {
		message = defaultSlogObserverMessage
	}
	logger.LogAttrs(ctx, o.level, message, slogObservationAttrs(observation)...)
}

func slogObservationAttrs(observation Observation) []slog.Attr {
	attrs := stableSlogObservationAttrs(observation)
	return append(attrs, legacySlogObservationAttrs(observation)...)
}

func stableSlogObservationAttrs(observation Observation) []slog.Attr {
	attrs := []slog.Attr{
		slog.String(TelemetryAttrEvent, string(observation.Type)),
		slog.Bool(TelemetryAttrFailed, observation.Failed),
	}
	attrs = appendSlogString(attrs, TelemetryAttrAgentID, observation.AgentID)
	attrs = appendSlogString(attrs, TelemetryAttrRunID, observation.RunID)
	attrs = appendSlogString(attrs, TelemetryAttrSubagentID, observation.SubagentID)
	attrs = appendSlogString(attrs, TelemetryAttrTraceID, observation.TraceID)
	attrs = appendSlogString(attrs, TelemetryAttrSpanID, observation.SpanID)
	attrs = appendSlogString(attrs, TelemetryAttrTraceState, observation.TraceState)
	attrs = appendSlogString(attrs, TelemetryAttrRequestID, observation.RequestID)
	attrs = appendSlogString(attrs, TelemetryAttrParentRequestID, observation.ParentRequestID)
	if observation.Round > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrRound, observation.Round))
	}
	if observation.Duration > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrDurationMS, durationMilliseconds(observation.Duration)))
	}
	if observation.EstimatedTokens > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrEstimatedTokens, observation.EstimatedTokens))
	}
	attrs = appendStableTokenUsageSlogAttrs(attrs, observation.TokenUsage)
	attrs = appendStableStreamTelemetrySlogAttrs(attrs, observation.StreamTelemetry)
	attrs = appendStableToolSlogAttrs(attrs, observation)
	attrs = appendSlogString(attrs, TelemetryAttrSkillName, observation.SkillName)
	attrs = appendStableApprovalSlogAttrs(attrs, observation)
	attrs = appendSlogString(attrs, TelemetryAttrErrorCategory, string(observation.ErrorCategory))
	attrs = appendSlogString(attrs, TelemetryAttrErrorModelSubcategory, string(observation.ModelErrorSubcategory))
	attrs = appendStableProviderDiagnosticsSlogAttrs(attrs, observation.ProviderDiagnostics)
	return attrs
}

func legacySlogObservationAttrs(observation Observation) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event", string(observation.Type)),
		slog.Bool("failed", observation.Failed),
	}
	attrs = appendSlogString(attrs, "agent_id", observation.AgentID)
	attrs = appendSlogString(attrs, "run_id", observation.RunID)
	attrs = appendSlogString(attrs, "subagent_id", observation.SubagentID)
	attrs = appendSlogString(attrs, "trace_id", observation.TraceID)
	attrs = appendSlogString(attrs, "span_id", observation.SpanID)
	attrs = appendSlogString(attrs, "trace_state", observation.TraceState)
	attrs = appendSlogString(attrs, "request_id", observation.RequestID)
	attrs = appendSlogString(attrs, "parent_request_id", observation.ParentRequestID)
	if observation.Round > 0 {
		attrs = append(attrs, slog.Int("round", observation.Round))
	}
	if observation.Duration > 0 {
		attrs = append(attrs, slog.Float64("duration_ms", durationMilliseconds(observation.Duration)))
	}
	if observation.EstimatedTokens > 0 {
		attrs = append(attrs, slog.Int("estimated_tokens", observation.EstimatedTokens))
	}
	if tokenAttrs := tokenUsageSlogAttrs(observation.TokenUsage); tokenAttrs != nil {
		attrs = append(attrs, slogGroupAttr("token_usage", tokenAttrs))
	}
	if streamAttrs := streamTelemetrySlogAttrs(observation.StreamTelemetry); streamAttrs != nil {
		attrs = append(attrs, slogGroupAttr("stream_telemetry", streamAttrs))
	}
	if toolAttrs := toolSlogAttrs(observation); toolAttrs != nil {
		attrs = append(attrs, slogGroupAttr("tool", toolAttrs))
	}
	attrs = appendSlogString(attrs, "skill_name", observation.SkillName)
	if approvalAttrs := approvalSlogAttrs(observation); approvalAttrs != nil {
		attrs = append(attrs, slogGroupAttr("approval", approvalAttrs))
	}
	attrs = appendSlogString(attrs, "error_category", string(observation.ErrorCategory))
	attrs = appendSlogString(attrs, "model_error_subcategory", string(observation.ModelErrorSubcategory))
	if providerAttrs := providerDiagnosticsSlogAttrs(observation.ProviderDiagnostics); providerAttrs != nil {
		attrs = append(attrs, slogGroupAttr("provider_diagnostics", providerAttrs))
	}
	return attrs
}

func appendStableTokenUsageSlogAttrs(attrs []slog.Attr, usage TokenUsage) []slog.Attr {
	if usage.InputTokens > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrTokensInput, usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrTokensOutput, usage.OutputTokens))
	}
	if usage.TotalTokens > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrTokensTotal, usage.TotalTokens))
	}
	return attrs
}

func appendStableStreamTelemetrySlogAttrs(attrs []slog.Attr, telemetry StreamTelemetry) []slog.Attr {
	if telemetry.TimeToFirstToken > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrStreamTimeToFirstTokenMS, durationMilliseconds(telemetry.TimeToFirstToken)))
	}
	if telemetry.DeltaCount > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrStreamDeltaCount, telemetry.DeltaCount))
	}
	if telemetry.ByteCount > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrStreamByteCount, telemetry.ByteCount))
	}
	if telemetry.ThroughputBytesPerSecond > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrStreamThroughputBytesPerSec, telemetry.ThroughputBytesPerSecond))
	}
	return attrs
}

func appendStableToolSlogAttrs(attrs []slog.Attr, observation Observation) []slog.Attr {
	attrs = appendSlogString(attrs, TelemetryAttrToolName, observation.ToolName)
	attrs = appendSlogString(attrs, TelemetryAttrToolRisk, string(observation.ToolRisk))
	attrs = appendSlogString(attrs, TelemetryAttrToolSchemaHash, observation.ToolSchemaHash)
	attrs = appendStableToolTimingSlogAttrs(attrs, observation.ToolTiming)
	attrs = appendStableToolSafetySlogAttrs(attrs, observation.ToolSafety)
	attrs = appendStableToolResultMetadataSlogAttrs(attrs, observation.ToolResultMetadata)
	return attrs
}

func appendStableToolTimingSlogAttrs(attrs []slog.Attr, timing ToolLifecycleTiming) []slog.Attr {
	if timing.Validation > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrToolTimingValidationMS, durationMilliseconds(timing.Validation)))
	}
	if timing.Approval > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrToolTimingApprovalMS, durationMilliseconds(timing.Approval)))
	}
	if timing.Execution > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrToolTimingExecutionMS, durationMilliseconds(timing.Execution)))
	}
	return attrs
}

func toolSafetyMetadataIsZero(metadata ToolSafetyMetadata) bool {
	return !metadata.TimeoutConfigured && metadata.Timeout == 0 && metadata.MaxConcurrency == 0 &&
		metadata.MaxResultBytes == 0 && metadata.ScopeCount == 0 && metadata.ScopeHash == "" &&
		metadata.BusinessReasonHash == ""
}

func appendStableToolSafetySlogAttrs(attrs []slog.Attr, metadata ToolSafetyMetadata) []slog.Attr {
	if toolSafetyMetadataIsZero(metadata) {
		return attrs
	}
	if metadata.TimeoutConfigured {
		attrs = append(attrs, slog.Bool(TelemetryAttrToolTimeoutConfigured, true))
	}
	if metadata.Timeout > 0 {
		attrs = append(attrs, slog.Float64(TelemetryAttrToolTimeoutMS, durationMilliseconds(metadata.Timeout)))
	}
	if metadata.MaxConcurrency > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrToolMaxConcurrency, metadata.MaxConcurrency))
	}
	if metadata.MaxResultBytes > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrToolMaxResultBytes, metadata.MaxResultBytes))
	}
	if metadata.ScopeCount > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrToolScopeCount, metadata.ScopeCount))
	}
	attrs = appendSlogString(attrs, TelemetryAttrToolScopeHash, metadata.ScopeHash)
	attrs = appendSlogString(attrs, TelemetryAttrToolBusinessReasonHash, metadata.BusinessReasonHash)
	return attrs
}

func appendStableToolResultMetadataSlogAttrs(attrs []slog.Attr, metadata ToolResultMetadata) []slog.Attr {
	if metadata.ContentBytes > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrToolResultContentBytes, metadata.ContentBytes))
	}
	if len(metadata.MetadataKeys) > 0 {
		attrs = append(attrs, slog.Any(TelemetryAttrToolResultMetadataKeys, append([]string(nil), metadata.MetadataKeys...)))
	}
	if metadata.MCPIsError != nil {
		attrs = append(attrs, slog.Bool(TelemetryAttrToolResultMCPIsError, *metadata.MCPIsError))
	}
	return attrs
}

func appendStableApprovalSlogAttrs(attrs []slog.Attr, observation Observation) []slog.Attr {
	if observation.Type == EventBeforeApproval || observation.Type == EventAfterApproval || observation.Approved || observation.ApprovalReason != "" {
		attrs = append(attrs, slog.Bool(TelemetryAttrApprovalApproved, observation.Approved))
	}
	return appendSlogString(attrs, TelemetryAttrApprovalReason, observation.ApprovalReason)
}

func appendStableProviderDiagnosticsSlogAttrs(attrs []slog.Attr, diagnostics ProviderDiagnostics) []slog.Attr {
	if diagnostics.IsZero() {
		return attrs
	}
	attrs = appendSlogString(attrs, TelemetryAttrProviderName, diagnostics.Provider)
	if diagnostics.HTTPStatus > 0 {
		attrs = append(attrs, slog.Int(TelemetryAttrProviderHTTPStatus, diagnostics.HTTPStatus))
	}
	attrs = appendSlogString(attrs, TelemetryAttrProviderEndpointHost, safeSlogEndpointHost(diagnostics.EndpointHost))
	attrs = appendSlogString(attrs, TelemetryAttrProviderRequestID, diagnostics.RequestID)
	attrs = appendSlogString(attrs, TelemetryAttrProviderRetryAfter, diagnostics.RetryAfter)
	attrs = appendSlogString(attrs, TelemetryAttrProviderRateLimitLimit, diagnostics.RateLimitLimit)
	attrs = appendSlogString(attrs, TelemetryAttrProviderRateLimitRemaining, diagnostics.RateLimitRemaining)
	attrs = appendSlogString(attrs, TelemetryAttrProviderRateLimitReset, diagnostics.RateLimitReset)
	return attrs
}

func slogGroupAttr(key string, attrs []slog.Attr) slog.Attr {
	return slog.Attr{Key: key, Value: slog.GroupValue(attrs...)}
}

func appendSlogString(attrs []slog.Attr, key string, value string) []slog.Attr {
	if value == "" {
		return attrs
	}
	return append(attrs, slog.String(key, value))
}

func tokenUsageSlogAttrs(usage TokenUsage) []slog.Attr {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return []slog.Attr{
		slog.Int("input_tokens", usage.InputTokens),
		slog.Int("output_tokens", usage.OutputTokens),
		slog.Int("total_tokens", usage.TotalTokens),
	}
}

func streamTelemetrySlogAttrs(telemetry StreamTelemetry) []slog.Attr {
	if telemetry.TimeToFirstToken == 0 && telemetry.DeltaCount == 0 && telemetry.ByteCount == 0 && telemetry.ThroughputBytesPerSecond == 0 {
		return nil
	}
	var attrs []slog.Attr
	if telemetry.TimeToFirstToken > 0 {
		timeToFirstTokenMS := float64(telemetry.TimeToFirstToken) / float64(time.Millisecond)
		attrs = append(attrs, slog.Float64("time_to_first_token_ms", timeToFirstTokenMS))
	}
	if telemetry.DeltaCount > 0 {
		attrs = append(attrs, slog.Int("delta_count", telemetry.DeltaCount))
	}
	if telemetry.ByteCount > 0 {
		attrs = append(attrs, slog.Int("byte_count", telemetry.ByteCount))
	}
	if telemetry.ThroughputBytesPerSecond > 0 {
		attrs = append(attrs, slog.Float64("throughput_bytes_per_second", telemetry.ThroughputBytesPerSecond))
	}
	return attrs
}

func toolSlogAttrs(observation Observation) []slog.Attr {
	var attrs []slog.Attr
	attrs = appendSlogString(attrs, "name", observation.ToolName)
	attrs = appendSlogString(attrs, "risk", string(observation.ToolRisk))
	attrs = appendSlogString(attrs, "schema_hash", observation.ToolSchemaHash)
	if timingAttrs := toolLifecycleTimingSlogAttrs(observation.ToolTiming); timingAttrs != nil {
		attrs = append(attrs, slogGroupAttr("timing", timingAttrs))
	}
	attrs = append(attrs, toolSafetySlogAttrs(observation.ToolSafety)...)
	attrs = append(attrs, toolResultMetadataSlogAttrs(observation.ToolResultMetadata)...)
	return attrs
}

func toolLifecycleTimingSlogAttrs(timing ToolLifecycleTiming) []slog.Attr {
	if timing.Validation == 0 && timing.Approval == 0 && timing.Execution == 0 {
		return nil
	}
	var attrs []slog.Attr
	if timing.Validation > 0 {
		attrs = append(attrs, slog.Float64("validation_ms", durationMilliseconds(timing.Validation)))
	}
	if timing.Approval > 0 {
		attrs = append(attrs, slog.Float64("approval_ms", durationMilliseconds(timing.Approval)))
	}
	if timing.Execution > 0 {
		attrs = append(attrs, slog.Float64("execution_ms", durationMilliseconds(timing.Execution)))
	}
	return attrs
}

func toolSafetySlogAttrs(metadata ToolSafetyMetadata) []slog.Attr {
	if toolSafetyMetadataIsZero(metadata) {
		return nil
	}
	var attrs []slog.Attr
	if metadata.TimeoutConfigured {
		attrs = append(attrs, slog.Bool("timeout_configured", true))
	}
	if metadata.Timeout > 0 {
		attrs = append(attrs, slog.Float64("timeout_ms", durationMilliseconds(metadata.Timeout)))
	}
	if metadata.MaxConcurrency > 0 {
		attrs = append(attrs, slog.Int("max_concurrency", metadata.MaxConcurrency))
	}
	if metadata.MaxResultBytes > 0 {
		attrs = append(attrs, slog.Int("max_result_bytes", metadata.MaxResultBytes))
	}
	if metadata.ScopeCount > 0 {
		attrs = append(attrs, slog.Int("scope_count", metadata.ScopeCount))
	}
	attrs = appendSlogString(attrs, "scope_hash", metadata.ScopeHash)
	attrs = appendSlogString(attrs, "business_reason_hash", metadata.BusinessReasonHash)
	return attrs
}

func toolResultMetadataSlogAttrs(metadata ToolResultMetadata) []slog.Attr {
	var attrs []slog.Attr
	if metadata.ContentBytes > 0 {
		attrs = append(attrs, slog.Int("result_content_bytes", metadata.ContentBytes))
	}
	if len(metadata.MetadataKeys) > 0 {
		attrs = append(attrs, slog.Any("result_metadata_keys", append([]string(nil), metadata.MetadataKeys...)))
	}
	if metadata.MCPIsError != nil {
		attrs = append(attrs, slog.Bool("mcp_is_error", *metadata.MCPIsError))
	}
	return attrs
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func approvalSlogAttrs(observation Observation) []slog.Attr {
	if observation.Type != EventBeforeApproval && observation.Type != EventAfterApproval && !observation.Approved && observation.ApprovalReason == "" {
		return nil
	}
	attrs := []slog.Attr{slog.Bool("approved", observation.Approved)}
	attrs = appendSlogString(attrs, "reason", observation.ApprovalReason)
	return attrs
}

func providerDiagnosticsSlogAttrs(diagnostics ProviderDiagnostics) []slog.Attr {
	if diagnostics.IsZero() {
		return nil
	}
	var attrs []slog.Attr
	attrs = appendSlogString(attrs, "provider", diagnostics.Provider)
	if diagnostics.HTTPStatus > 0 {
		attrs = append(attrs, slog.Int("http_status", diagnostics.HTTPStatus))
	}
	attrs = appendSlogString(attrs, "endpoint_host", safeSlogEndpointHost(diagnostics.EndpointHost))
	attrs = appendSlogString(attrs, "request_id", diagnostics.RequestID)
	attrs = appendSlogString(attrs, "retry_after", diagnostics.RetryAfter)
	attrs = appendSlogString(attrs, "rate_limit_limit", diagnostics.RateLimitLimit)
	attrs = appendSlogString(attrs, "rate_limit_remaining", diagnostics.RateLimitRemaining)
	attrs = appendSlogString(attrs, "rate_limit_reset", diagnostics.RateLimitReset)
	return attrs
}

func safeSlogEndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		return stripSlogUserInfo(parsed.Host)
	}
	host := endpoint
	for _, separator := range []string{"/", "?", "#"} {
		if index := strings.Index(host, separator); index >= 0 {
			host = host[:index]
		}
	}
	return stripSlogUserInfo(strings.TrimSpace(host))
}

func stripSlogUserInfo(host string) string {
	if index := strings.LastIndex(host, "@"); index >= 0 {
		return host[index+1:]
	}
	return host
}
