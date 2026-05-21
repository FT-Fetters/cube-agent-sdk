package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSlogObserverEmitsStructuredObservationAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: dropSlogTime,
	}))
	observer := NewSlogObserver(SlogObserverOptions{
		Logger:  logger,
		Level:   slog.LevelWarn,
		Message: "sdk observation",
	})

	observation := observationWithToolSchemaHash(t, Observation{
		Type:            EventAfterTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		SubagentID:      "worker-1",
		TraceID:         "trace-1",
		SpanID:          "span-1",
		TraceState:      "vendor=state",
		RequestID:       "request-1",
		ParentRequestID: "parent-request-1",
		Round:           3,
		Duration:        1250 * time.Millisecond,
		EstimatedTokens: 321,
		TokenUsage: TokenUsage{
			InputTokens:  100,
			OutputTokens: 40,
			TotalTokens:  140,
		},
		ToolName:              "lookup",
		ToolRisk:              ToolRiskRead,
		SkillName:             "research",
		ProviderDiagnostics:   testProviderDiagnostics(),
		ModelErrorSubcategory: ModelErrorSubcategoryRateLimited,
		Approved:              true,
		ApprovalReason:        "allowed by policy",
		ErrorCategory:         ErrorCategoryTool,
		Failed:                true,
	}, "sha256:slog-schema-hash")

	observer.Observe(context.Background(), observation)

	record := decodeSlogRecord(t, buf.String())
	assertSlogField(t, record, "level", "WARN")
	assertSlogField(t, record, "msg", "sdk observation")
	assertSlogField(t, record, "event", string(EventAfterTool))
	assertSlogField(t, record, "agent_id", "agent-1")
	assertSlogField(t, record, "run_id", "run-1")
	assertSlogField(t, record, "subagent_id", "worker-1")
	assertSlogField(t, record, "trace_id", "trace-1")
	assertSlogField(t, record, "span_id", "span-1")
	assertSlogField(t, record, "trace_state", "vendor=state")
	assertSlogField(t, record, "request_id", "request-1")
	assertSlogField(t, record, "parent_request_id", "parent-request-1")
	assertSlogField(t, record, "round", float64(3))
	assertSlogField(t, record, "duration_ms", float64(1250))
	assertSlogField(t, record, "estimated_tokens", float64(321))
	assertSlogField(t, record, "skill_name", "research")
	assertSlogField(t, record, "error_category", string(ErrorCategoryTool))
	assertSlogField(t, record, "model_error_subcategory", string(ModelErrorSubcategoryRateLimited))
	assertSlogField(t, record, "failed", true)

	tokenUsage := assertSlogGroup(t, record, "token_usage")
	assertSlogField(t, tokenUsage, "input_tokens", float64(100))
	assertSlogField(t, tokenUsage, "output_tokens", float64(40))
	assertSlogField(t, tokenUsage, "total_tokens", float64(140))

	tool := assertSlogGroup(t, record, "tool")
	assertSlogField(t, tool, "name", "lookup")
	assertSlogField(t, tool, "risk", string(ToolRiskRead))
	assertSlogField(t, tool, "schema_hash", "sha256:slog-schema-hash")

	approval := assertSlogGroup(t, record, "approval")
	assertSlogField(t, approval, "approved", true)
	assertSlogField(t, approval, "reason", "allowed by policy")

	provider := assertSlogGroup(t, record, "provider_diagnostics")
	assertSlogField(t, provider, "provider", "openai-compatible")
	assertSlogField(t, provider, "http_status", float64(429))
	assertSlogField(t, provider, "endpoint_host", "api.example.test")
	assertSlogField(t, provider, "request_id", "provider-request-1")
	assertSlogField(t, provider, "retry_after", "30")
	assertSlogField(t, provider, "rate_limit_limit", "1000")
	assertSlogField(t, provider, "rate_limit_remaining", "0")
	assertSlogField(t, provider, "rate_limit_reset", "60")
}

func TestSlogObserverOmitsZeroObservationAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: dropSlogTime,
	}))
	observer := NewSlogObserver(SlogObserverOptions{Logger: logger})

	observer.Observe(context.Background(), Observation{Type: EventBeforeModel})

	record := decodeSlogRecord(t, buf.String())
	assertSlogField(t, record, "level", "INFO")
	assertSlogField(t, record, "msg", "agent observation")
	assertSlogField(t, record, "event", string(EventBeforeModel))
	assertSlogField(t, record, "failed", false)

	for _, field := range []string{
		"agent_id",
		"run_id",
		"request_id",
		"parent_request_id",
		"round",
		"duration_ms",
		"estimated_tokens",
		"token_usage",
		"stream_telemetry",
		"tool",
		"approval",
		"provider_diagnostics",
		"error_category",
		"model_error_subcategory",
	} {
		if _, ok := record[field]; ok {
			t.Fatalf("slog record field %q = %#v, want omitted", field, record[field])
		}
	}
}

func TestSlogObserverEmitsStreamTelemetryAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: dropSlogTime,
	}))
	observer := NewSlogObserver(SlogObserverOptions{Logger: logger})
	observation := Observation{
		Type: EventAfterModel,
		StreamTelemetry: StreamTelemetry{
			TimeToFirstToken:         250 * time.Millisecond,
			DeltaCount:               3,
			ByteCount:                120,
			ThroughputBytesPerSecond: 480,
		},
	}

	observer.Observe(context.Background(), observation)

	record := decodeSlogRecord(t, buf.String())
	stream := assertSlogGroup(t, record, "stream_telemetry")
	assertSlogField(t, stream, "time_to_first_token_ms", float64(250))
	assertSlogField(t, stream, "delta_count", float64(3))
	assertSlogField(t, stream, "byte_count", float64(120))
	assertSlogField(t, stream, "throughput_bytes_per_second", float64(480))
}

func TestSlogObserverDoesNotEmitSensitiveEventPayloads(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: dropSlogTime,
	}))
	observer := NewSlogObserver(SlogObserverOptions{Logger: logger})
	observation := ObservationFromEvent(Event{
		Type:      EventAfterModel,
		AgentID:   "agent-1",
		RequestID: "request-1",
		ProviderDiagnostics: ProviderDiagnostics{
			Provider:     "openai-compatible",
			HTTPStatus:   401,
			EndpointHost: "https://api.example.test/v1/chat?api_key=secret-api-key",
			RequestID:    "provider-request-1",
		},
		Message: Message{
			Role:    RoleAssistant,
			Content: "secret prompt response",
		},
		ToolCall: ToolCall{
			Name: "lookup",
			Arguments: map[string]any{
				"api_key":  "secret-api-key",
				"mcp_env":  "secret-mcp-env",
				"endpoint": "https://api.example.test/v1/chat?api_key=secret-api-key",
			},
		},
		ToolResult: ToolResult{
			Name:    "lookup",
			Content: "secret tool result",
			Metadata: map[string]any{
				"raw": "secret raw metadata",
			},
		},
		Error: errors.New("raw provider error with secret-api-key and secret prompt response"),
	})

	observer.Observe(context.Background(), observation)

	output := buf.String()
	for _, unsafe := range []string{
		"secret prompt response",
		"secret-api-key",
		"secret-mcp-env",
		"secret tool result",
		"secret raw metadata",
		"raw provider error",
		"https://api.example.test/v1/chat",
		"api_key=secret-api-key",
	} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("slog output leaked %q: %s", unsafe, output)
		}
	}
	record := decodeSlogRecord(t, output)
	assertSlogField(t, record, "event", string(EventAfterModel))
	assertSlogField(t, record, "failed", true)
	provider := assertSlogGroup(t, record, "provider_diagnostics")
	assertSlogField(t, provider, "endpoint_host", "api.example.test")
}

func testProviderDiagnostics() ProviderDiagnostics {
	return ProviderDiagnostics{
		Provider:           "openai-compatible",
		HTTPStatus:         429,
		EndpointHost:       "api.example.test",
		RequestID:          "provider-request-1",
		RetryAfter:         "30",
		RateLimitLimit:     "1000",
		RateLimitRemaining: "0",
		RateLimitReset:     "60",
	}
}

func dropSlogTime(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return attr
}

func decodeSlogRecord(t *testing.T, output string) map[string]any {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal([]byte(output), &record); err != nil {
		t.Fatalf("decode slog output %q: %v", output, err)
	}
	return record
}

func assertSlogGroup(t *testing.T, record map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := record[key]
	if !ok {
		t.Fatalf("slog record missing group %q in %#v", key, record)
	}
	group, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("slog record group %q = %#v, want object", key, value)
	}
	return group
}

func assertSlogField(t *testing.T, record map[string]any, key string, want any) {
	t.Helper()
	got, ok := record[key]
	if !ok {
		t.Fatalf("slog record missing field %q in %#v", key, record)
	}
	if got != want {
		t.Fatalf("slog record field %q = %#v, want %#v", key, got, want)
	}
}
