package main

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	agent "github.com/cubence/cube-agent-sdk"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const observationEventName = "cube_agent.observation"

// OTelObserver maps sanitized SDK observations to OpenTelemetry spans.
//
// It only reads fields exposed on agent.Observation. That keeps prompts,
// message content, tool arguments, tool result content, metadata values, raw
// errors, credentials, and MCP environment values outside the tracing layer.
type OTelObserver struct {
	tracer trace.Tracer

	mu              sync.Mutex
	active          map[spanKey]trace.Span
	requestContexts map[requestKey]trace.SpanContext
}

type requestKey struct {
	runID     string
	requestID string
}

type spanKey struct {
	requestKey
	kind string
}

// NewOTelObserver returns an SDK observer that emits OpenTelemetry spans.
func NewOTelObserver(tracer trace.Tracer) *OTelObserver {
	if tracer == nil {
		tracer = otel.Tracer("github.com/cubence/cube-agent-sdk/examples/opentelemetry")
	}
	return &OTelObserver{
		tracer:          tracer,
		active:          map[spanKey]trace.Span{},
		requestContexts: map[requestKey]trace.SpanContext{},
	}
}

func (o *OTelObserver) Observe(ctx context.Context, observation agent.Observation) {
	if o == nil {
		return
	}
	tracer := o.tracer
	if tracer == nil {
		tracer = otel.Tracer("github.com/cubence/cube-agent-sdk/examples/opentelemetry")
	}

	kind := observationKind(observation.Type)
	key := spanKey{
		requestKey: requestKey{runID: observation.RunID, requestID: observation.RequestID},
		kind:       kind,
	}

	attrs := observationAttributes(observation)
	span, endSpan := o.spanForObservation(ctx, tracer, key, observation)
	span.SetAttributes(attrs...)
	span.AddEvent(observationEventName, trace.WithAttributes(attrs...))
	if observation.Failed {
		description := strings.TrimSpace(string(observation.ErrorCategory))
		if description == "" {
			description = "failed"
		}
		span.SetStatus(codes.Error, description)
	}
	if endSpan {
		span.End()
	}
}

func (o *OTelObserver) spanForObservation(ctx context.Context, tracer trace.Tracer, key spanKey, observation agent.Observation) (trace.Span, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if span, ok := o.active[key]; ok {
		if isEndingObservation(observation.Type) {
			delete(o.active, key)
			o.rememberSpanContext(key.requestKey, span.SpanContext())
			return span, true
		}
		o.rememberSpanContext(key.requestKey, span.SpanContext())
		return span, false
	}

	startCtx := o.parentContext(ctx, observation)
	startOptions := []trace.SpanStartOption{}
	if !isStartingObservation(observation.Type) && observation.Duration > 0 {
		// After-only observations can still render with useful elapsed time.
		startOptions = append(startOptions, trace.WithTimestamp(time.Now().Add(-observation.Duration)))
	}
	_, span := tracer.Start(startCtx, "cube_agent."+observationKind(observation.Type), startOptions...)
	o.rememberSpanContext(key.requestKey, span.SpanContext())

	if isStartingObservation(observation.Type) && key.requestID != "" {
		o.active[key] = span
		return span, false
	}
	return span, true
}

func (o *OTelObserver) parentContext(ctx context.Context, observation agent.Observation) context.Context {
	parentRequestID := strings.TrimSpace(observation.ParentRequestID)
	if parentRequestID == "" {
		return ctx
	}
	if parent, ok := o.requestContexts[requestKey{runID: observation.RunID, requestID: parentRequestID}]; ok && parent.IsValid() {
		return trace.ContextWithSpanContext(ctx, parent)
	}
	return ctx
}

func (o *OTelObserver) rememberSpanContext(key requestKey, spanContext trace.SpanContext) {
	if key.requestID == "" || !spanContext.IsValid() {
		return
	}
	o.requestContexts[key] = spanContext
}

func observationKind(eventType agent.EventType) string {
	switch eventType {
	case agent.EventBeforeModel, agent.EventAfterModel:
		return "model"
	case agent.EventBeforeTool, agent.EventAfterTool:
		return "tool"
	case agent.EventBeforeApproval, agent.EventAfterApproval:
		return "approval"
	case agent.EventBeforeCompact, agent.EventAfterCompact:
		return "compact"
	case agent.EventStreamStart, agent.EventStreamFirstDelta, agent.EventStreamDone, agent.EventStreamError:
		return "stream"
	case agent.EventSkillActivated:
		return "skill"
	case agent.EventSubagentMessage:
		return "subagent"
	default:
		return "observation"
	}
}

func isStartingObservation(eventType agent.EventType) bool {
	switch eventType {
	case agent.EventBeforeModel,
		agent.EventBeforeTool,
		agent.EventBeforeApproval,
		agent.EventBeforeCompact,
		agent.EventStreamStart:
		return true
	default:
		return false
	}
}

func isEndingObservation(eventType agent.EventType) bool {
	switch eventType {
	case agent.EventAfterModel,
		agent.EventAfterTool,
		agent.EventAfterApproval,
		agent.EventAfterCompact,
		agent.EventStreamDone,
		agent.EventStreamError:
		return true
	default:
		return false
	}
}

func observationAttributes(observation agent.Observation) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("cube.agent.event", string(observation.Type)),
		attribute.Bool("cube.agent.failed", observation.Failed),
	}
	attrs = appendStringAttr(attrs, "cube.agent.id", observation.AgentID)
	attrs = appendStringAttr(attrs, "cube.agent.run_id", observation.RunID)
	attrs = appendStringAttr(attrs, "cube.agent.subagent_id", observation.SubagentID)
	attrs = appendStringAttr(attrs, "cube.agent.trace_id", observation.TraceID)
	attrs = appendStringAttr(attrs, "cube.agent.span_id", observation.SpanID)
	attrs = appendStringAttr(attrs, "cube.agent.trace_state", observation.TraceState)
	attrs = appendStringAttr(attrs, "cube.agent.request_id", observation.RequestID)
	attrs = appendStringAttr(attrs, "cube.agent.parent_request_id", observation.ParentRequestID)
	if observation.Round > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.round", observation.Round))
	}
	if observation.Duration > 0 {
		attrs = append(attrs, attribute.Float64("cube.agent.duration_ms", durationMilliseconds(observation.Duration)))
	}
	if observation.EstimatedTokens > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.estimated_tokens", observation.EstimatedTokens))
	}
	attrs = appendTokenUsageAttrs(attrs, observation.TokenUsage)
	attrs = appendStreamAttrs(attrs, observation.StreamTelemetry)
	attrs = appendToolAttrs(attrs, observation)
	attrs = appendStringAttr(attrs, "cube.agent.skill_name", observation.SkillName)
	attrs = appendApprovalAttrs(attrs, observation)
	attrs = appendStringAttr(attrs, "cube.agent.error_category", string(observation.ErrorCategory))
	attrs = appendStringAttr(attrs, "cube.agent.model_error_subcategory", string(observation.ModelErrorSubcategory))
	attrs = appendProviderAttrs(attrs, observation.ProviderDiagnostics)
	return attrs
}

func appendTokenUsageAttrs(attrs []attribute.KeyValue, usage agent.TokenUsage) []attribute.KeyValue {
	if usage.InputTokens > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.tokens.input", usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.tokens.output", usage.OutputTokens))
	}
	if usage.TotalTokens > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.tokens.total", usage.TotalTokens))
	}
	return attrs
}

func appendStreamAttrs(attrs []attribute.KeyValue, telemetry agent.StreamTelemetry) []attribute.KeyValue {
	if telemetry.TimeToFirstToken > 0 {
		attrs = append(attrs, attribute.Float64("cube.agent.stream.time_to_first_token_ms", durationMilliseconds(telemetry.TimeToFirstToken)))
	}
	if telemetry.DeltaCount > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.stream.delta_count", telemetry.DeltaCount))
	}
	if telemetry.ByteCount > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.stream.byte_count", telemetry.ByteCount))
	}
	if telemetry.ThroughputBytesPerSecond > 0 {
		attrs = append(attrs, attribute.Float64("cube.agent.stream.throughput_bytes_per_second", telemetry.ThroughputBytesPerSecond))
	}
	return attrs
}

func appendToolAttrs(attrs []attribute.KeyValue, observation agent.Observation) []attribute.KeyValue {
	attrs = appendStringAttr(attrs, "cube.agent.tool.name", observation.ToolName)
	attrs = appendStringAttr(attrs, "cube.agent.tool.risk", string(observation.ToolRisk))
	attrs = appendStringAttr(attrs, "cube.agent.tool.schema_hash", observation.ToolSchemaHash)
	attrs = appendToolTimingAttrs(attrs, observation.ToolTiming)
	attrs = appendToolResultMetadataAttrs(attrs, observation.ToolResultMetadata)
	return attrs
}

func appendToolTimingAttrs(attrs []attribute.KeyValue, timing agent.ToolLifecycleTiming) []attribute.KeyValue {
	if timing.Validation > 0 {
		attrs = append(attrs, attribute.Float64("cube.agent.tool.timing.validation_ms", durationMilliseconds(timing.Validation)))
	}
	if timing.Approval > 0 {
		attrs = append(attrs, attribute.Float64("cube.agent.tool.timing.approval_ms", durationMilliseconds(timing.Approval)))
	}
	if timing.Execution > 0 {
		attrs = append(attrs, attribute.Float64("cube.agent.tool.timing.execution_ms", durationMilliseconds(timing.Execution)))
	}
	return attrs
}

func appendToolResultMetadataAttrs(attrs []attribute.KeyValue, metadata agent.ToolResultMetadata) []attribute.KeyValue {
	if metadata.ContentBytes > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.tool.result.content_bytes", metadata.ContentBytes))
	}
	if len(metadata.MetadataKeys) > 0 {
		attrs = append(attrs, attribute.StringSlice("cube.agent.tool.result.metadata_keys", append([]string(nil), metadata.MetadataKeys...)))
	}
	if metadata.MCPIsError != nil {
		attrs = append(attrs, attribute.Bool("cube.agent.tool.result.mcp_is_error", *metadata.MCPIsError))
	}
	return attrs
}

func appendApprovalAttrs(attrs []attribute.KeyValue, observation agent.Observation) []attribute.KeyValue {
	if observation.Type == agent.EventBeforeApproval || observation.Type == agent.EventAfterApproval || observation.Approved || observation.ApprovalReason != "" {
		attrs = append(attrs, attribute.Bool("cube.agent.approval.approved", observation.Approved))
	}
	attrs = appendStringAttr(attrs, "cube.agent.approval.reason", observation.ApprovalReason)
	return attrs
}

func appendProviderAttrs(attrs []attribute.KeyValue, diagnostics agent.ProviderDiagnostics) []attribute.KeyValue {
	if diagnostics.IsZero() {
		return attrs
	}
	attrs = appendStringAttr(attrs, "cube.agent.provider.name", diagnostics.Provider)
	if diagnostics.HTTPStatus > 0 {
		attrs = append(attrs, attribute.Int("cube.agent.provider.http_status", diagnostics.HTTPStatus))
	}
	attrs = appendStringAttr(attrs, "cube.agent.provider.endpoint_host", safeEndpointHost(diagnostics.EndpointHost))
	attrs = appendStringAttr(attrs, "cube.agent.provider.request_id", diagnostics.RequestID)
	attrs = appendStringAttr(attrs, "cube.agent.provider.retry_after", diagnostics.RetryAfter)
	attrs = appendStringAttr(attrs, "cube.agent.provider.rate_limit.limit", diagnostics.RateLimitLimit)
	attrs = appendStringAttr(attrs, "cube.agent.provider.rate_limit.remaining", diagnostics.RateLimitRemaining)
	attrs = appendStringAttr(attrs, "cube.agent.provider.rate_limit.reset", diagnostics.RateLimitReset)
	return attrs
}

func appendStringAttr(attrs []attribute.KeyValue, key string, value string) []attribute.KeyValue {
	value = strings.TrimSpace(value)
	if value == "" {
		return attrs
	}
	return append(attrs, attribute.String(key, value))
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func safeEndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		return stripUserInfo(parsed.Host)
	}
	host := endpoint
	for _, separator := range []string{"/", "?", "#"} {
		if index := strings.Index(host, separator); index >= 0 {
			host = host[:index]
		}
	}
	return stripUserInfo(strings.TrimSpace(host))
}

func stripUserInfo(host string) string {
	if index := strings.LastIndex(host, "@"); index >= 0 {
		return host[index+1:]
	}
	return host
}
