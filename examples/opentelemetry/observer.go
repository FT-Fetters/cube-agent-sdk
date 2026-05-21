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
		attribute.String(agent.TelemetryAttrEvent, string(observation.Type)),
		attribute.Bool(agent.TelemetryAttrFailed, observation.Failed),
	}
	attrs = appendStringAttr(attrs, agent.TelemetryAttrAgentID, observation.AgentID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrRunID, observation.RunID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrSubagentID, observation.SubagentID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrTraceID, observation.TraceID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrSpanID, observation.SpanID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrTraceState, observation.TraceState)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrRequestID, observation.RequestID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrParentRequestID, observation.ParentRequestID)
	if observation.Round > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrRound, observation.Round))
	}
	if observation.Duration > 0 {
		attrs = append(attrs, attribute.Float64(agent.TelemetryAttrDurationMS, durationMilliseconds(observation.Duration)))
	}
	if observation.EstimatedTokens > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrEstimatedTokens, observation.EstimatedTokens))
	}
	attrs = appendTokenUsageAttrs(attrs, observation.TokenUsage)
	attrs = appendStreamAttrs(attrs, observation.StreamTelemetry)
	attrs = appendToolAttrs(attrs, observation)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrSkillName, observation.SkillName)
	attrs = appendApprovalAttrs(attrs, observation)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrErrorCategory, string(observation.ErrorCategory))
	attrs = appendStringAttr(attrs, agent.TelemetryAttrErrorModelSubcategory, string(observation.ModelErrorSubcategory))
	attrs = appendProviderAttrs(attrs, observation.ProviderDiagnostics)
	return attrs
}

func appendTokenUsageAttrs(attrs []attribute.KeyValue, usage agent.TokenUsage) []attribute.KeyValue {
	if usage.InputTokens > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrTokensInput, usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrTokensOutput, usage.OutputTokens))
	}
	if usage.TotalTokens > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrTokensTotal, usage.TotalTokens))
	}
	return attrs
}

func appendStreamAttrs(attrs []attribute.KeyValue, telemetry agent.StreamTelemetry) []attribute.KeyValue {
	if telemetry.TimeToFirstToken > 0 {
		attrs = append(attrs, attribute.Float64(agent.TelemetryAttrStreamTimeToFirstTokenMS, durationMilliseconds(telemetry.TimeToFirstToken)))
	}
	if telemetry.DeltaCount > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrStreamDeltaCount, telemetry.DeltaCount))
	}
	if telemetry.ByteCount > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrStreamByteCount, telemetry.ByteCount))
	}
	if telemetry.ThroughputBytesPerSecond > 0 {
		attrs = append(attrs, attribute.Float64(agent.TelemetryAttrStreamThroughputBytesPerSec, telemetry.ThroughputBytesPerSecond))
	}
	return attrs
}

func appendToolAttrs(attrs []attribute.KeyValue, observation agent.Observation) []attribute.KeyValue {
	attrs = appendStringAttr(attrs, agent.TelemetryAttrToolName, observation.ToolName)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrToolRisk, string(observation.ToolRisk))
	attrs = appendStringAttr(attrs, agent.TelemetryAttrToolSchemaHash, observation.ToolSchemaHash)
	attrs = appendToolTimingAttrs(attrs, observation.ToolTiming)
	attrs = appendToolResultMetadataAttrs(attrs, observation.ToolResultMetadata)
	return attrs
}

func appendToolTimingAttrs(attrs []attribute.KeyValue, timing agent.ToolLifecycleTiming) []attribute.KeyValue {
	if timing.Validation > 0 {
		attrs = append(attrs, attribute.Float64(agent.TelemetryAttrToolTimingValidationMS, durationMilliseconds(timing.Validation)))
	}
	if timing.Approval > 0 {
		attrs = append(attrs, attribute.Float64(agent.TelemetryAttrToolTimingApprovalMS, durationMilliseconds(timing.Approval)))
	}
	if timing.Execution > 0 {
		attrs = append(attrs, attribute.Float64(agent.TelemetryAttrToolTimingExecutionMS, durationMilliseconds(timing.Execution)))
	}
	return attrs
}

func appendToolResultMetadataAttrs(attrs []attribute.KeyValue, metadata agent.ToolResultMetadata) []attribute.KeyValue {
	if metadata.ContentBytes > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrToolResultContentBytes, metadata.ContentBytes))
	}
	if len(metadata.MetadataKeys) > 0 {
		attrs = append(attrs, attribute.StringSlice(agent.TelemetryAttrToolResultMetadataKeys, append([]string(nil), metadata.MetadataKeys...)))
	}
	if metadata.MCPIsError != nil {
		attrs = append(attrs, attribute.Bool(agent.TelemetryAttrToolResultMCPIsError, *metadata.MCPIsError))
	}
	return attrs
}

func appendApprovalAttrs(attrs []attribute.KeyValue, observation agent.Observation) []attribute.KeyValue {
	if observation.Type == agent.EventBeforeApproval || observation.Type == agent.EventAfterApproval || observation.Approved || observation.ApprovalReason != "" {
		attrs = append(attrs, attribute.Bool(agent.TelemetryAttrApprovalApproved, observation.Approved))
	}
	attrs = appendStringAttr(attrs, agent.TelemetryAttrApprovalReason, observation.ApprovalReason)
	return attrs
}

func appendProviderAttrs(attrs []attribute.KeyValue, diagnostics agent.ProviderDiagnostics) []attribute.KeyValue {
	if diagnostics.IsZero() {
		return attrs
	}
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderName, diagnostics.Provider)
	if diagnostics.HTTPStatus > 0 {
		attrs = append(attrs, attribute.Int(agent.TelemetryAttrProviderHTTPStatus, diagnostics.HTTPStatus))
	}
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderEndpointHost, safeEndpointHost(diagnostics.EndpointHost))
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderRequestID, diagnostics.RequestID)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderRetryAfter, diagnostics.RetryAfter)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderRateLimitLimit, diagnostics.RateLimitLimit)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderRateLimitRemaining, diagnostics.RateLimitRemaining)
	attrs = appendStringAttr(attrs, agent.TelemetryAttrProviderRateLimitReset, diagnostics.RateLimitReset)
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
