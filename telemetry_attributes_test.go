package agent

import (
	"reflect"
	"testing"
)

func TestStableTelemetryAttributeNamesDoNotDrift(t *testing.T) {
	want := []string{
		"agent.event",
		"agent.failed",
		"agent.id",
		"agent.run_id",
		"agent.subagent_id",
		"agent.trace_id",
		"agent.span_id",
		"agent.trace_state",
		"agent.request_id",
		"agent.parent_request_id",
		"agent.round",
		"agent.duration_ms",
		"agent.estimated_tokens",
		"agent.tokens.input",
		"agent.tokens.output",
		"agent.tokens.total",
		"agent.stream.time_to_first_token_ms",
		"agent.stream.delta_count",
		"agent.stream.byte_count",
		"agent.stream.throughput_bytes_per_second",
		"agent.tool.name",
		"agent.tool.risk",
		"agent.tool.schema_hash",
		"agent.tool.timing.validation_ms",
		"agent.tool.timing.approval_ms",
		"agent.tool.timing.execution_ms",
		"agent.tool.timeout_configured",
		"agent.tool.timeout_ms",
		"agent.tool.max_concurrency",
		"agent.tool.max_result_bytes",
		"agent.tool.scope.count",
		"agent.tool.scope.hash",
		"agent.tool.business_reason.hash",
		"agent.tool.result.content_bytes",
		"agent.tool.result.metadata_keys",
		"agent.tool.result.mcp_is_error",
		"agent.skill.name",
		"agent.approval.approved",
		"agent.approval.reason",
		"agent.error.category",
		"agent.error.model_subcategory",
		"agent.provider.name",
		"agent.provider.http_status",
		"agent.provider.endpoint_host",
		"agent.provider.request_id",
		"agent.provider.retry_after",
		"agent.provider.rate_limit.limit",
		"agent.provider.rate_limit.remaining",
		"agent.provider.rate_limit.reset",
	}
	if got := StableTelemetryAttributeNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("stable telemetry attribute names = %#v, want %#v", got, want)
	}

	got := StableTelemetryAttributeNames()
	got[0] = "mutated"
	if StableTelemetryAttributeNames()[0] == "mutated" {
		t.Fatal("stable telemetry attribute names returned mutable package storage")
	}
}

func TestTelemetryCardinalityListsDoNotDrift(t *testing.T) {
	lowCardinality := []string{
		"agent.event",
		"agent.failed",
		"agent.tool.risk",
		"agent.tool.result.mcp_is_error",
		"agent.tool.timeout_configured",
		"agent.approval.approved",
		"agent.error.category",
		"agent.error.model_subcategory",
		"agent.provider.name",
		"agent.provider.http_status",
	}
	if got := LowCardinalityTelemetryAttributeNames(); !reflect.DeepEqual(got, lowCardinality) {
		t.Fatalf("low-cardinality telemetry attribute names = %#v, want %#v", got, lowCardinality)
	}
	gotLow := LowCardinalityTelemetryAttributeNames()
	gotLow[0] = "mutated"
	if LowCardinalityTelemetryAttributeNames()[0] == "mutated" {
		t.Fatal("low-cardinality telemetry attribute names returned mutable package storage")
	}

	highCardinality := []string{
		"agent.id",
		"agent.run_id",
		"agent.subagent_id",
		"agent.trace_id",
		"agent.span_id",
		"agent.trace_state",
		"agent.request_id",
		"agent.parent_request_id",
		"agent.tool.name",
		"agent.tool.schema_hash",
		"agent.tool.scope.hash",
		"agent.tool.business_reason.hash",
		"agent.tool.result.metadata_keys",
		"agent.skill.name",
		"agent.approval.reason",
		"agent.provider.endpoint_host",
		"agent.provider.request_id",
		"agent.provider.retry_after",
		"agent.provider.rate_limit.limit",
		"agent.provider.rate_limit.remaining",
		"agent.provider.rate_limit.reset",
	}
	if got := HighCardinalityTelemetryAttributeNames(); !reflect.DeepEqual(got, highCardinality) {
		t.Fatalf("high-cardinality telemetry attribute names = %#v, want %#v", got, highCardinality)
	}
	gotHigh := HighCardinalityTelemetryAttributeNames()
	gotHigh[0] = "mutated"
	if HighCardinalityTelemetryAttributeNames()[0] == "mutated" {
		t.Fatal("high-cardinality telemetry attribute names returned mutable package storage")
	}

	assertTelemetryListSubset(t, lowCardinality, StableTelemetryAttributeNames())
	assertTelemetryListSubset(t, highCardinality, StableTelemetryAttributeNames())
	assertTelemetryListsDoNotOverlap(t, lowCardinality, highCardinality)
}

func TestStableTelemetryMetricLabelNamesDoNotDrift(t *testing.T) {
	want := []string{
		"event",
		"failed",
		"error_category",
		"model_error_subcategory",
		"tool_name",
		"tool_risk",
		"provider",
		"http_status",
		"tool_phase",
	}
	if got := StableTelemetryMetricLabelNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("stable telemetry metric label names = %#v, want %#v", got, want)
	}
	got := StableTelemetryMetricLabelNames()
	got[0] = "mutated"
	if StableTelemetryMetricLabelNames()[0] == "mutated" {
		t.Fatal("stable telemetry metric label names returned mutable package storage")
	}
}

func TestForbiddenTelemetryFieldNamesDoNotDrift(t *testing.T) {
	want := []string{
		"prompts",
		"message_content",
		"tool_arguments",
		"tool_result_content",
		"tool_result_metadata_values",
		"raw_errors",
		"credentials",
		"full_provider_urls",
		"mcp_environment_values",
		"tool_scope_values",
		"tool_business_reasons",
	}
	if got := ForbiddenTelemetryFieldNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("forbidden telemetry field names = %#v, want %#v", got, want)
	}
	got := ForbiddenTelemetryFieldNames()
	got[0] = "mutated"
	if ForbiddenTelemetryFieldNames()[0] == "mutated" {
		t.Fatal("forbidden telemetry field names returned mutable package storage")
	}
}

func assertTelemetryListSubset(t *testing.T, subset []string, superset []string) {
	t.Helper()
	values := make(map[string]struct{}, len(superset))
	for _, value := range superset {
		values[value] = struct{}{}
	}
	for _, value := range subset {
		if _, ok := values[value]; !ok {
			t.Fatalf("telemetry list member %q not present in stable attribute names", value)
		}
	}
}

func assertTelemetryListsDoNotOverlap(t *testing.T, a []string, b []string) {
	t.Helper()
	values := make(map[string]struct{}, len(a))
	for _, value := range a {
		values[value] = struct{}{}
	}
	for _, value := range b {
		if _, ok := values[value]; ok {
			t.Fatalf("telemetry lists overlap on %q", value)
		}
	}
}
