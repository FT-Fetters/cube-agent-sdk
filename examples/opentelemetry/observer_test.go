package main

import (
	"context"
	"strings"
	"testing"
	"time"

	agent "github.com/cubence/cube-agent-sdk"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOTelObserverMapsSafeObservationFields(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	observer := NewOTelObserver(provider.Tracer("test"))
	mcpIsError := true

	observer.Observe(context.Background(), agent.Observation{
		Type:            agent.EventAfterTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		SubagentID:      "worker-1",
		ToolName:        "lookup_account",
		ToolRisk:        agent.ToolRiskRead,
		ToolSchemaHash:  "tool-schema-hash",
		SkillName:       "research",
		TraceID:         "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:          "00f067aa0ba902b7",
		TraceState:      "vendor=state",
		RequestID:       "tool-request-1",
		ParentRequestID: "model-request-1",
		Round:           2,
		Duration:        1500 * time.Millisecond,
		EstimatedTokens: 42,
		ToolTiming: agent.ToolLifecycleTiming{
			Validation: 5 * time.Millisecond,
			Approval:   10 * time.Millisecond,
			Execution:  20 * time.Millisecond,
		},
		TokenUsage: agent.TokenUsage{
			InputTokens:  11,
			OutputTokens: 7,
			TotalTokens:  18,
		},
		StreamTelemetry: agent.StreamTelemetry{
			TimeToFirstToken:         25 * time.Millisecond,
			DeltaCount:               3,
			ByteCount:                24,
			ThroughputBytesPerSecond: 12.5,
		},
		ProviderDiagnostics: agent.ProviderDiagnostics{
			Provider:           "openai-compatible",
			HTTPStatus:         429,
			EndpointHost:       "api.example.test",
			RequestID:          "provider-request-1",
			RetryAfter:         "2",
			RateLimitLimit:     "100",
			RateLimitRemaining: "0",
			RateLimitReset:     "123",
		},
		ToolResultMetadata: agent.ToolResultMetadata{
			ContentBytes: 28,
			MetadataKeys: []string{
				"mcpIsError",
				"row_count",
			},
			MCPIsError: &mcpIsError,
		},
		ModelErrorSubcategory: agent.ModelErrorSubcategoryRateLimited,
		Approved:              true,
		ApprovalReason:        "policy allowed read-only tool",
		ErrorCategory:         agent.ErrorCategoryTool,
		Failed:                true,
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name() != "cube_agent.tool" {
		t.Fatalf("span name = %q, want cube_agent.tool", span.Name())
	}
	assertSpanStringAttr(t, span, "cube.agent.event", string(agent.EventAfterTool))
	assertSpanStringAttr(t, span, "cube.agent.id", "agent-1")
	assertSpanStringAttr(t, span, "cube.agent.run_id", "run-1")
	assertSpanStringAttr(t, span, "cube.agent.request_id", "tool-request-1")
	assertSpanStringAttr(t, span, "cube.agent.parent_request_id", "model-request-1")
	assertSpanStringAttr(t, span, "cube.agent.trace_id", "4bf92f3577b34da6a3ce929d0e0e4736")
	assertSpanStringAttr(t, span, "cube.agent.span_id", "00f067aa0ba902b7")
	assertSpanStringAttr(t, span, "cube.agent.trace_state", "vendor=state")
	assertSpanStringAttr(t, span, "cube.agent.tool.name", "lookup_account")
	assertSpanStringAttr(t, span, "cube.agent.tool.risk", string(agent.ToolRiskRead))
	assertSpanStringAttr(t, span, "cube.agent.error_category", string(agent.ErrorCategoryTool))
	assertSpanStringAttr(t, span, "cube.agent.model_error_subcategory", string(agent.ModelErrorSubcategoryRateLimited))
	assertSpanIntAttr(t, span, "cube.agent.round", 2)
	assertSpanIntAttr(t, span, "cube.agent.estimated_tokens", 42)
	assertSpanIntAttr(t, span, "cube.agent.tokens.input", 11)
	assertSpanIntAttr(t, span, "cube.agent.tokens.output", 7)
	assertSpanIntAttr(t, span, "cube.agent.tokens.total", 18)
	assertSpanFloatAttr(t, span, "cube.agent.duration_ms", 1500)
	assertSpanFloatAttr(t, span, "cube.agent.tool.timing.validation_ms", 5)
	assertSpanFloatAttr(t, span, "cube.agent.tool.timing.approval_ms", 10)
	assertSpanFloatAttr(t, span, "cube.agent.tool.timing.execution_ms", 20)
	assertSpanFloatAttr(t, span, "cube.agent.stream.time_to_first_token_ms", 25)
	assertSpanIntAttr(t, span, "cube.agent.stream.delta_count", 3)
	assertSpanIntAttr(t, span, "cube.agent.stream.byte_count", 24)
	assertSpanFloatAttr(t, span, "cube.agent.stream.throughput_bytes_per_second", 12.5)
	assertSpanIntAttr(t, span, "cube.agent.tool.result.content_bytes", 28)
	assertSpanBoolAttr(t, span, "cube.agent.tool.result.mcp_is_error", true)
	assertSpanStringSliceAttr(t, span, "cube.agent.tool.result.metadata_keys", []string{"mcpIsError", "row_count"})
	assertSpanStringAttr(t, span, "cube.agent.provider.name", "openai-compatible")
	assertSpanIntAttr(t, span, "cube.agent.provider.http_status", 429)
	assertSpanStringAttr(t, span, "cube.agent.provider.endpoint_host", "api.example.test")
	assertSpanStringAttr(t, span, "cube.agent.provider.request_id", "provider-request-1")
	assertSpanBoolAttr(t, span, "cube.agent.failed", true)

	if span.Status().Code.String() != "Error" {
		t.Fatalf("span status = %s, want Error", span.Status().Code)
	}
	assertObservationEvent(t, span, string(agent.EventAfterTool))
	assertSpanDoesNotContain(t, span,
		"secret-prompt",
		"message content",
		"account-secret",
		"tool-result-secret",
		"metadata-value-secret",
		"raw-error-secret",
		"sk-test-key",
	)
}

func TestOTelObserverUsesRequestParentCorrelation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := provider.Tracer("test")
	observer := NewOTelObserver(tracer)
	ctx, root := tracer.Start(context.Background(), "root")

	observer.Observe(ctx, agent.Observation{
		Type:      agent.EventBeforeModel,
		AgentID:   "agent-1",
		RunID:     "run-1",
		RequestID: "model-request-1",
		Round:     1,
	})
	observer.Observe(ctx, agent.Observation{
		Type:      agent.EventAfterModel,
		AgentID:   "agent-1",
		RunID:     "run-1",
		RequestID: "model-request-1",
		Round:     1,
		Duration:  10 * time.Millisecond,
	})
	observer.Observe(ctx, agent.Observation{
		Type:            agent.EventBeforeTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		RequestID:       "tool-request-1",
		ParentRequestID: "model-request-1",
		ToolName:        "lookup_account",
	})
	observer.Observe(ctx, agent.Observation{
		Type:            agent.EventAfterTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		RequestID:       "tool-request-1",
		ParentRequestID: "model-request-1",
		ToolName:        "lookup_account",
		Duration:        5 * time.Millisecond,
	})
	root.End()

	modelSpan := endedSpanByRequestID(t, recorder.Ended(), "model-request-1")
	toolSpan := endedSpanByRequestID(t, recorder.Ended(), "tool-request-1")
	if modelSpan.Parent().SpanID() != root.SpanContext().SpanID() {
		t.Fatalf("model parent span = %s, want root span %s", modelSpan.Parent().SpanID(), root.SpanContext().SpanID())
	}
	if toolSpan.Parent().SpanID() != modelSpan.SpanContext().SpanID() {
		t.Fatalf("tool parent span = %s, want model span %s", toolSpan.Parent().SpanID(), modelSpan.SpanContext().SpanID())
	}
}

func endedSpanByRequestID(t *testing.T, spans []sdktrace.ReadOnlySpan, requestID string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "cube.agent.request_id" && attr.Value.AsString() == requestID {
				return span
			}
		}
	}
	t.Fatalf("span with request id %q not found", requestID)
	return nil
}

func assertObservationEvent(t *testing.T, span sdktrace.ReadOnlySpan, eventType string) {
	t.Helper()
	for _, event := range span.Events() {
		if event.Name != "cube_agent.observation" {
			continue
		}
		for _, attr := range event.Attributes {
			if string(attr.Key) == "cube.agent.event" && attr.Value.AsString() == eventType {
				return
			}
		}
	}
	t.Fatalf("span events do not include observation event %q", eventType)
}

func assertSpanStringAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, want string) {
	t.Helper()
	got, ok := spanAttr(span, key)
	if !ok || got.Value.AsString() != want {
		t.Fatalf("span attribute %s = %q/%t, want %q", key, got.Value.AsString(), ok, want)
	}
}

func assertSpanStringSliceAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, want []string) {
	t.Helper()
	got, ok := spanAttr(span, key)
	if !ok {
		t.Fatalf("span attribute %s missing", key)
	}
	gotSlice := got.Value.AsStringSlice()
	if len(gotSlice) != len(want) {
		t.Fatalf("span attribute %s = %#v, want %#v", key, gotSlice, want)
	}
	for i := range want {
		if gotSlice[i] != want[i] {
			t.Fatalf("span attribute %s = %#v, want %#v", key, gotSlice, want)
		}
	}
}

func assertSpanIntAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, want int64) {
	t.Helper()
	got, ok := spanAttr(span, key)
	if !ok || got.Value.AsInt64() != want {
		t.Fatalf("span attribute %s = %d/%t, want %d", key, got.Value.AsInt64(), ok, want)
	}
}

func assertSpanFloatAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, want float64) {
	t.Helper()
	got, ok := spanAttr(span, key)
	if !ok || got.Value.AsFloat64() != want {
		t.Fatalf("span attribute %s = %f/%t, want %f", key, got.Value.AsFloat64(), ok, want)
	}
}

func assertSpanBoolAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string, want bool) {
	t.Helper()
	got, ok := spanAttr(span, key)
	if !ok || got.Value.AsBool() != want {
		t.Fatalf("span attribute %s = %t/%t, want %t", key, got.Value.AsBool(), ok, want)
	}
}

func spanAttr(span sdktrace.ReadOnlySpan, key string) (attribute.KeyValue, bool) {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr, true
		}
	}
	return attribute.KeyValue{}, false
}

func assertSpanDoesNotContain(t *testing.T, span sdktrace.ReadOnlySpan, unsafeValues ...string) {
	t.Helper()
	text := span.Name() + " " + span.Status().Description
	for _, attr := range span.Attributes() {
		text += " " + string(attr.Key) + "=" + attr.Value.Emit()
	}
	for _, event := range span.Events() {
		text += " " + event.Name
		for _, attr := range event.Attributes {
			text += " " + string(attr.Key) + "=" + attr.Value.Emit()
		}
	}
	for _, unsafe := range unsafeValues {
		if strings.Contains(text, unsafe) {
			t.Fatalf("span contains unsafe value %q in %s", unsafe, text)
		}
	}
}

var _ agent.Observer = (*OTelObserver)(nil)
