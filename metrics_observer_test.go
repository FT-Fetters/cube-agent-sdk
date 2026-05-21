package agent

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestMetricsObserverEmitsCountersAndDurationWithSafeLabels(t *testing.T) {
	sink := &recordingMetricSink{}
	observer := NewMetricsObserver(MetricsObserverOptions{Sink: sink})

	observation := observationWithToolSchemaHash(t, Observation{
		Type:                  EventAfterTool,
		AgentID:               "agent-1",
		RunID:                 "run-1",
		SubagentID:            "worker-1",
		TraceID:               "trace-1",
		SpanID:                "span-1",
		TraceState:            "vendor=state",
		RequestID:             "request-1",
		ParentRequestID:       "parent-request-1",
		Duration:              1500 * time.Millisecond,
		ToolName:              "lookup",
		ToolRisk:              ToolRiskRead,
		ProviderDiagnostics:   testProviderDiagnostics(),
		ErrorCategory:         ErrorCategoryTool,
		ModelErrorSubcategory: ModelErrorSubcategoryRateLimited,
		Failed:                true,
	}, "sha256:metric-schema-hash")

	observer.Observe(context.Background(), observation)

	wantLabels := []MetricLabel{
		{Name: "event", Value: string(EventAfterTool)},
		{Name: "failed", Value: "true"},
		{Name: "error_category", Value: string(ErrorCategoryTool)},
		{Name: "model_error_subcategory", Value: string(ModelErrorSubcategoryRateLimited)},
		{Name: "tool_name", Value: "lookup"},
		{Name: "tool_risk", Value: string(ToolRiskRead)},
		{Name: "provider", Value: "openai-compatible"},
		{Name: "http_status", Value: "429"},
	}
	calls := sink.Calls()
	if len(calls) != 3 {
		t.Fatalf("metric calls = %d, want 3: %#v", len(calls), calls)
	}
	assertMetricCounterCall(t, calls[0], DefaultMetricsEventCounterName, 1, wantLabels)
	assertMetricCounterCall(t, calls[1], DefaultMetricsFailureCounterName, 1, wantLabels)
	assertMetricDurationCall(t, calls[2], DefaultMetricsDurationName, 1500*time.Millisecond, wantLabels)
	assertMetricLabelsOmitHighCardinalityFields(t, calls)
}

func TestMetricsObserverSkipsFailureAndDurationWhenNotApplicable(t *testing.T) {
	sink := &recordingMetricSink{}
	observer := NewMetricsObserver(MetricsObserverOptions{Sink: sink})

	observer.Observe(context.Background(), Observation{
		Type:     EventBeforeModel,
		Duration: 0,
		Failed:   false,
	})

	wantLabels := []MetricLabel{
		{Name: "event", Value: string(EventBeforeModel)},
		{Name: "failed", Value: "false"},
	}
	calls := sink.Calls()
	if len(calls) != 1 {
		t.Fatalf("metric calls = %d, want 1: %#v", len(calls), calls)
	}
	assertMetricCounterCall(t, calls[0], DefaultMetricsEventCounterName, 1, wantLabels)
}

func TestMetricsObserverDoesNotLabelToolResultMetadata(t *testing.T) {
	sink := &recordingMetricSink{}
	observer := NewMetricsObserver(MetricsObserverOptions{Sink: sink})
	observation := ObservationFromEvent(Event{
		Type:     EventAfterTool,
		ToolName: "lookup",
		ToolResult: ToolResult{
			Content: "tool result payload",
			Metadata: map[string]any{
				"customerID":           "metadata-value-secret",
				"mcpStructuredContent": map[string]any{"secret": "structured-value-secret"},
				"mcpIsError":           true,
			},
		},
	})
	metadata := toolResultMetadataFromObservation(t, observation)
	if metadata.contentBytes != len("tool result payload") || metadata.mcpIsError == nil || *metadata.mcpIsError != true {
		t.Fatalf("tool result metadata = %#v, want size and MCP error status", metadata)
	}

	observer.Observe(context.Background(), observation)

	calls := sink.Calls()
	if len(calls) != 1 {
		t.Fatalf("metric calls = %d, want 1: %#v", len(calls), calls)
	}
	assertMetricLabelsOmitToolResultMetadata(t, calls)
}

func TestMetricsObserverRecordsToolLifecycleTimingSegmentsWithSafeLabels(t *testing.T) {
	sink := &recordingMetricSink{}
	observer := NewMetricsObserver(MetricsObserverOptions{Sink: sink})
	observation := observationWithToolLifecycleTiming(t, Observation{
		Type:            EventAfterTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		SubagentID:      "worker-1",
		TraceID:         "trace-1",
		SpanID:          "span-1",
		TraceState:      "vendor=state",
		RequestID:       "request-1",
		ParentRequestID: "parent-request-1",
		ToolName:        "lookup",
		ToolRisk:        ToolRiskWrite,
		ErrorCategory:   ErrorCategoryApproval,
		Failed:          true,
	}, 11*time.Millisecond, 22*time.Millisecond, 0)

	observer.Observe(context.Background(), observation)

	calls := sink.Calls()
	if len(calls) != 4 {
		t.Fatalf("metric calls = %d, want 4: %#v", len(calls), calls)
	}
	baseLabels := []MetricLabel{
		{Name: "event", Value: string(EventAfterTool)},
		{Name: "failed", Value: "true"},
		{Name: "error_category", Value: string(ErrorCategoryApproval)},
		{Name: "tool_name", Value: "lookup"},
		{Name: "tool_risk", Value: string(ToolRiskWrite)},
	}
	assertMetricCounterCall(t, calls[0], DefaultMetricsEventCounterName, 1, baseLabels)
	assertMetricCounterCall(t, calls[1], DefaultMetricsFailureCounterName, 1, baseLabels)
	assertMetricDurationCall(t, calls[2], DefaultMetricsToolLifecycleDurationName, 11*time.Millisecond, []MetricLabel{
		{Name: "event", Value: string(EventAfterTool)},
		{Name: "failed", Value: "true"},
		{Name: "error_category", Value: string(ErrorCategoryApproval)},
		{Name: "tool_risk", Value: string(ToolRiskWrite)},
		{Name: "tool_phase", Value: "validation"},
	})
	assertMetricDurationCall(t, calls[3], DefaultMetricsToolLifecycleDurationName, 22*time.Millisecond, []MetricLabel{
		{Name: "event", Value: string(EventAfterTool)},
		{Name: "failed", Value: "true"},
		{Name: "error_category", Value: string(ErrorCategoryApproval)},
		{Name: "tool_risk", Value: string(ToolRiskWrite)},
		{Name: "tool_phase", Value: "approval"},
	})
	assertMetricLabelsOmitHighCardinalityFields(t, calls)
	assertMetricLabelsOmitToolLifecycleTimingHighCardinalityFields(t, calls)
}

type recordingMetricSink struct {
	calls []metricCall
}

type metricCall struct {
	kind     string
	name     string
	value    int64
	duration time.Duration
	labels   []MetricLabel
}

func (s *recordingMetricSink) AddCounter(ctx context.Context, name string, delta int64, labels []MetricLabel) {
	s.calls = append(s.calls, metricCall{
		kind:   "counter",
		name:   name,
		value:  delta,
		labels: append([]MetricLabel(nil), labels...),
	})
}

func (s *recordingMetricSink) RecordDuration(ctx context.Context, name string, duration time.Duration, labels []MetricLabel) {
	s.calls = append(s.calls, metricCall{
		kind:     "duration",
		name:     name,
		duration: duration,
		labels:   append([]MetricLabel(nil), labels...),
	})
}

func (s *recordingMetricSink) Calls() []metricCall {
	return append([]metricCall(nil), s.calls...)
}

func assertMetricCounterCall(t *testing.T, call metricCall, name string, value int64, labels []MetricLabel) {
	t.Helper()
	if call.kind != "counter" || call.name != name || call.value != value {
		t.Fatalf("metric counter call = %#v, want kind counter name %q value %d", call, name, value)
	}
	assertMetricLabels(t, call.labels, labels)
}

func assertMetricDurationCall(t *testing.T, call metricCall, name string, duration time.Duration, labels []MetricLabel) {
	t.Helper()
	if call.kind != "duration" || call.name != name || call.duration != duration {
		t.Fatalf("metric duration call = %#v, want kind duration name %q duration %s", call, name, duration)
	}
	assertMetricLabels(t, call.labels, labels)
}

func assertMetricLabels(t *testing.T, got []MetricLabel, want []MetricLabel) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metric labels = %#v, want %#v", got, want)
	}
}

func assertMetricLabelsOmitHighCardinalityFields(t *testing.T, calls []metricCall) {
	t.Helper()
	disallowedNames := map[string]struct{}{
		"agent_id":            {},
		"run_id":              {},
		"subagent_id":         {},
		"trace_id":            {},
		"span_id":             {},
		"trace_state":         {},
		"request_id":          {},
		"parent_request_id":   {},
		"provider_request_id": {},
		"tool_schema_hash":    {},
	}
	disallowedValues := map[string]struct{}{
		"agent-1":                   {},
		"run-1":                     {},
		"worker-1":                  {},
		"trace-1":                   {},
		"span-1":                    {},
		"vendor=state":              {},
		"request-1":                 {},
		"parent-request-1":          {},
		"provider-request-1":        {},
		"sha256:metric-schema-hash": {},
	}
	for _, call := range calls {
		for _, label := range call.labels {
			if _, ok := disallowedNames[label.Name]; ok {
				t.Fatalf("metric labels included high-cardinality label name %q in call %#v", label.Name, call)
			}
			if _, ok := disallowedValues[label.Value]; ok {
				t.Fatalf("metric labels included high-cardinality label value %q in call %#v", label.Value, call)
			}
		}
	}
}

func assertMetricLabelsOmitToolResultMetadata(t *testing.T, calls []metricCall) {
	t.Helper()
	disallowedNames := map[string]struct{}{
		"tool_result_content_bytes": {},
		"tool_result_metadata_keys": {},
		"result_content_bytes":      {},
		"result_metadata_keys":      {},
		"mcp_is_error":              {},
	}
	disallowedValues := map[string]struct{}{
		"customerID":              {},
		"mcpStructuredContent":    {},
		"metadata-value-secret":   {},
		"structured-value-secret": {},
	}
	for _, call := range calls {
		for _, label := range call.labels {
			if _, ok := disallowedNames[label.Name]; ok {
				t.Fatalf("metric labels included tool result metadata label name %q in call %#v", label.Name, call)
			}
			if _, ok := disallowedValues[label.Value]; ok {
				t.Fatalf("metric labels included tool result metadata value %q in call %#v", label.Value, call)
			}
		}
	}
}

func assertMetricLabelsOmitToolLifecycleTimingHighCardinalityFields(t *testing.T, calls []metricCall) {
	t.Helper()
	for _, call := range calls {
		if call.name != DefaultMetricsToolLifecycleDurationName {
			continue
		}
		for _, label := range call.labels {
			if label.Name == "tool_name" || label.Value == "lookup" {
				t.Fatalf("tool lifecycle metric labels included high-cardinality tool name in call %#v", call)
			}
		}
	}
}
