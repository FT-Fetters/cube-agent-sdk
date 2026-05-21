package agent

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestObservationFromEventCopiesOnlySafeContractFields(t *testing.T) {
	const (
		messageSecret             = "observation-contract-message-secret"
		toolArgumentSecret        = "observation-contract-tool-argument-secret"
		toolResultSecret          = "observation-contract-tool-result-secret"
		toolMetadataValueSecret   = "observation-contract-tool-metadata-value-secret"
		rawErrorSecret            = "observation-contract-raw-error-secret"
		mcpEnvironmentValueSecret = "observation-contract-mcp-env-secret"
		fullProviderURL           = "https://user:password@api.example.test/v1/responses?api_key=observation-contract-api-key#fragment"
	)

	event := Event{
		Type:            EventAfterTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		SubagentID:      "worker-1",
		ToolName:        "lookup",
		ToolRisk:        ToolRiskWrite,
		ToolSchemaHash:  "schema-hash-1",
		SkillName:       "review",
		TraceID:         "trace-1",
		SpanID:          "span-1",
		TraceState:      "state-1",
		RequestID:       "request-1",
		ParentRequestID: "parent-1",
		Round:           3,
		Duration:        42 * time.Millisecond,
		EstimatedTokens: 123,
		ToolTiming: ToolLifecycleTiming{
			Validation: time.Millisecond,
			Approval:   2 * time.Millisecond,
			Execution:  3 * time.Millisecond,
		},
		TokenUsage: TokenUsage{
			InputTokens:  11,
			OutputTokens: 13,
			TotalTokens:  24,
		},
		StreamTelemetry: StreamTelemetry{
			TimeToFirstToken:         4 * time.Millisecond,
			DeltaCount:               5,
			ByteCount:                67,
			ThroughputBytesPerSecond: 89.5,
		},
		ProviderDiagnostics: ProviderDiagnostics{
			Provider:           " provider-a ",
			HTTPStatus:         429,
			EndpointHost:       fullProviderURL,
			RequestID:          " provider-request-1 ",
			RetryAfter:         " 30 ",
			RateLimitLimit:     " 100 ",
			RateLimitRemaining: " 0 ",
			RateLimitReset:     " 60 ",
		},
		ModelErrorSubcategory: ModelErrorSubcategoryRateLimited,
		Approved:              true,
		ApprovalReason:        "safe approval reason",
		ErrorCategory:         ErrorCategoryTool,
		Message: Message{
			Role:    RoleAssistant,
			Content: messageSecret,
			ToolCalls: []ToolCall{{
				ID:        "message-tool-call",
				Name:      "lookup",
				Arguments: map[string]any{"query": toolArgumentSecret},
			}},
			Metadata: map[string]any{"mcpEnv": mcpEnvironmentValueSecret},
		},
		ToolCall: ToolCall{
			ID:        "call-1",
			Name:      "lookup",
			Arguments: map[string]any{"query": toolArgumentSecret},
		},
		ToolResult: ToolResult{
			CallID:  "call-1",
			Name:    "lookup",
			Content: toolResultSecret,
			Metadata: map[string]any{
				"zeta":       toolMetadataValueSecret,
				"alpha":      map[string]any{"secret": toolMetadataValueSecret},
				"mcpIsError": true,
			},
		},
		Error: errors.New(rawErrorSecret),
	}

	want := Observation{
		Type:            EventAfterTool,
		AgentID:         "agent-1",
		RunID:           "run-1",
		SubagentID:      "worker-1",
		ToolName:        "lookup",
		ToolRisk:        ToolRiskWrite,
		ToolSchemaHash:  "schema-hash-1",
		SkillName:       "review",
		TraceID:         "trace-1",
		SpanID:          "span-1",
		TraceState:      "state-1",
		RequestID:       "request-1",
		ParentRequestID: "parent-1",
		Round:           3,
		Duration:        42 * time.Millisecond,
		EstimatedTokens: 123,
		ToolTiming: ToolLifecycleTiming{
			Validation: time.Millisecond,
			Approval:   2 * time.Millisecond,
			Execution:  3 * time.Millisecond,
		},
		TokenUsage: TokenUsage{
			InputTokens:  11,
			OutputTokens: 13,
			TotalTokens:  24,
		},
		StreamTelemetry: StreamTelemetry{
			TimeToFirstToken:         4 * time.Millisecond,
			DeltaCount:               5,
			ByteCount:                67,
			ThroughputBytesPerSecond: 89.5,
		},
		ProviderDiagnostics: ProviderDiagnostics{
			Provider:           "provider-a",
			HTTPStatus:         429,
			EndpointHost:       "api.example.test",
			RequestID:          "provider-request-1",
			RetryAfter:         "30",
			RateLimitLimit:     "100",
			RateLimitRemaining: "0",
			RateLimitReset:     "60",
		},
		ToolResultMetadata: ToolResultMetadata{
			ContentBytes: len(toolResultSecret),
			MetadataKeys: []string{
				"alpha",
				"mcpIsError",
				"zeta",
			},
			MCPIsError: boolPointer(true),
		},
		ModelErrorSubcategory: ModelErrorSubcategoryRateLimited,
		Approved:              true,
		ApprovalReason:        "safe approval reason",
		ErrorCategory:         ErrorCategoryTool,
		Failed:                true,
	}

	observation := ObservationFromEvent(event)
	if !reflect.DeepEqual(observation, want) {
		t.Fatalf("ObservationFromEvent() = %#v, want %#v", observation, want)
	}
	for _, unsafe := range []string{
		messageSecret,
		toolArgumentSecret,
		toolResultSecret,
		toolMetadataValueSecret,
		rawErrorSecret,
		mcpEnvironmentValueSecret,
		fullProviderURL,
		"user:password",
		"api_key=observation-contract-api-key",
	} {
		assertObservationDoesNotContain(t, observation, unsafe)
	}
}

func TestObservationFromEventLifecycleContractCoversEventTypes(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  Observation
	}{
		{
			name: "before model",
			event: Event{
				Type:            EventBeforeModel,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "model-request",
				ParentRequestID: "parent-request",
				Round:           1,
				EstimatedTokens: 12,
			},
			want: Observation{
				Type:            EventBeforeModel,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "model-request",
				ParentRequestID: "parent-request",
				Round:           1,
				EstimatedTokens: 12,
			},
		},
		{
			name: "after model failure",
			event: Event{
				Type:                  EventAfterModel,
				AgentID:               "agent",
				RunID:                 "run",
				RequestID:             "model-request",
				Round:                 1,
				Duration:              20 * time.Millisecond,
				TokenUsage:            TokenUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
				ProviderDiagnostics:   ProviderDiagnostics{Provider: "provider", HTTPStatus: 500, EndpointHost: "https://provider.example.test/v1"},
				ModelErrorSubcategory: ModelErrorSubcategoryServerError,
				ErrorCategory:         ErrorCategoryModel,
			},
			want: Observation{
				Type:                  EventAfterModel,
				AgentID:               "agent",
				RunID:                 "run",
				RequestID:             "model-request",
				Round:                 1,
				Duration:              20 * time.Millisecond,
				TokenUsage:            TokenUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
				ProviderDiagnostics:   ProviderDiagnostics{Provider: "provider", HTTPStatus: 500, EndpointHost: "provider.example.test"},
				ModelErrorSubcategory: ModelErrorSubcategoryServerError,
				ErrorCategory:         ErrorCategoryModel,
				Failed:                true,
			},
		},
		{
			name: "before approval",
			event: Event{
				Type:            EventBeforeApproval,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "write_file",
				ToolRisk:        ToolRiskWrite,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "approval-request",
				ParentRequestID: "model-request",
				Round:           1,
				EstimatedTokens: 12,
			},
			want: Observation{
				Type:            EventBeforeApproval,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "write_file",
				ToolRisk:        ToolRiskWrite,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "approval-request",
				ParentRequestID: "model-request",
				Round:           1,
				EstimatedTokens: 12,
			},
		},
		{
			name: "after approval denied",
			event: Event{
				Type:            EventAfterApproval,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "write_file",
				ToolRisk:        ToolRiskWrite,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "approval-request",
				ParentRequestID: "model-request",
				Duration:        3 * time.Millisecond,
				ApprovalReason:  "denied by policy",
				ErrorCategory:   ErrorCategoryApproval,
			},
			want: Observation{
				Type:            EventAfterApproval,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "write_file",
				ToolRisk:        ToolRiskWrite,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "approval-request",
				ParentRequestID: "model-request",
				Duration:        3 * time.Millisecond,
				ApprovalReason:  "denied by policy",
				ErrorCategory:   ErrorCategoryApproval,
				Failed:          true,
			},
		},
		{
			name: "before tool",
			event: Event{
				Type:            EventBeforeTool,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "lookup",
				ToolRisk:        ToolRiskRead,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "tool-request",
				ParentRequestID: "model-request",
				Round:           1,
				EstimatedTokens: 12,
			},
			want: Observation{
				Type:            EventBeforeTool,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "lookup",
				ToolRisk:        ToolRiskRead,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "tool-request",
				ParentRequestID: "model-request",
				Round:           1,
				EstimatedTokens: 12,
			},
		},
		{
			name: "after tool",
			event: Event{
				Type:            EventAfterTool,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "lookup",
				ToolRisk:        ToolRiskRead,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "tool-request",
				ParentRequestID: "model-request",
				Duration:        8 * time.Millisecond,
				ToolTiming:      ToolLifecycleTiming{Validation: time.Millisecond, Approval: 2 * time.Millisecond, Execution: 5 * time.Millisecond},
				ToolResult: ToolResult{
					Content:  "safe result",
					Metadata: map[string]any{"beta": "value", "alpha": "value"},
				},
			},
			want: Observation{
				Type:            EventAfterTool,
				AgentID:         "agent",
				RunID:           "run",
				ToolName:        "lookup",
				ToolRisk:        ToolRiskRead,
				ToolSchemaHash:  "schema-hash",
				RequestID:       "tool-request",
				ParentRequestID: "model-request",
				Duration:        8 * time.Millisecond,
				ToolTiming:      ToolLifecycleTiming{Validation: time.Millisecond, Approval: 2 * time.Millisecond, Execution: 5 * time.Millisecond},
				ToolResultMetadata: ToolResultMetadata{
					ContentBytes: len("safe result"),
					MetadataKeys: []string{"alpha", "beta"},
				},
			},
		},
		{
			name: "before compact",
			event: Event{
				Type:            EventBeforeCompact,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "compact-request",
				ParentRequestID: "model-request",
				Round:           2,
				EstimatedTokens: 100,
			},
			want: Observation{
				Type:            EventBeforeCompact,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "compact-request",
				ParentRequestID: "model-request",
				Round:           2,
				EstimatedTokens: 100,
			},
		},
		{
			name: "after compact",
			event: Event{
				Type:            EventAfterCompact,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "compact-request",
				ParentRequestID: "model-request",
				Round:           2,
				Duration:        15 * time.Millisecond,
				EstimatedTokens: 40,
			},
			want: Observation{
				Type:            EventAfterCompact,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "compact-request",
				ParentRequestID: "model-request",
				Round:           2,
				Duration:        15 * time.Millisecond,
				EstimatedTokens: 40,
			},
		},
		{
			name: "stream start",
			event: Event{
				Type:            EventStreamStart,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "stream-request",
				ParentRequestID: "parent-request",
				Round:           1,
				EstimatedTokens: 12,
			},
			want: Observation{
				Type:            EventStreamStart,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "stream-request",
				ParentRequestID: "parent-request",
				Round:           1,
				EstimatedTokens: 12,
			},
		},
		{
			name: "stream first delta",
			event: Event{
				Type:            EventStreamFirstDelta,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "stream-request",
				Duration:        5 * time.Millisecond,
				StreamTelemetry: StreamTelemetry{TimeToFirstToken: 5 * time.Millisecond, DeltaCount: 1, ByteCount: 4, ThroughputBytesPerSecond: 80},
			},
			want: Observation{
				Type:            EventStreamFirstDelta,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "stream-request",
				Duration:        5 * time.Millisecond,
				StreamTelemetry: StreamTelemetry{TimeToFirstToken: 5 * time.Millisecond, DeltaCount: 1, ByteCount: 4, ThroughputBytesPerSecond: 80},
			},
		},
		{
			name: "stream done",
			event: Event{
				Type:            EventStreamDone,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "stream-request",
				Duration:        12 * time.Millisecond,
				StreamTelemetry: StreamTelemetry{TimeToFirstToken: 5 * time.Millisecond, DeltaCount: 3, ByteCount: 12, ThroughputBytesPerSecond: 100},
			},
			want: Observation{
				Type:            EventStreamDone,
				AgentID:         "agent",
				RunID:           "run",
				RequestID:       "stream-request",
				Duration:        12 * time.Millisecond,
				StreamTelemetry: StreamTelemetry{TimeToFirstToken: 5 * time.Millisecond, DeltaCount: 3, ByteCount: 12, ThroughputBytesPerSecond: 100},
			},
		},
		{
			name: "stream error",
			event: Event{
				Type:                  EventStreamError,
				AgentID:               "agent",
				RunID:                 "run",
				RequestID:             "stream-request",
				Duration:              12 * time.Millisecond,
				StreamTelemetry:       StreamTelemetry{TimeToFirstToken: 5 * time.Millisecond, DeltaCount: 2, ByteCount: 8, ThroughputBytesPerSecond: 80},
				ErrorCategory:         ErrorCategoryModel,
				ModelErrorSubcategory: ModelErrorSubcategoryTransportError,
			},
			want: Observation{
				Type:                  EventStreamError,
				AgentID:               "agent",
				RunID:                 "run",
				RequestID:             "stream-request",
				Duration:              12 * time.Millisecond,
				StreamTelemetry:       StreamTelemetry{TimeToFirstToken: 5 * time.Millisecond, DeltaCount: 2, ByteCount: 8, ThroughputBytesPerSecond: 80},
				ErrorCategory:         ErrorCategoryModel,
				ModelErrorSubcategory: ModelErrorSubcategoryTransportError,
				Failed:                true,
			},
		},
		{
			name: "subagent message",
			event: Event{
				Type:            EventSubagentMessage,
				AgentID:         "agent",
				RunID:           "run",
				SubagentID:      "worker",
				RequestID:       "subagent-request",
				ParentRequestID: "parent-request",
			},
			want: Observation{
				Type:            EventSubagentMessage,
				AgentID:         "agent",
				RunID:           "run",
				SubagentID:      "worker",
				RequestID:       "subagent-request",
				ParentRequestID: "parent-request",
			},
		},
		{
			name: "skill activated",
			event: Event{
				Type:      EventSkillActivated,
				AgentID:   "agent",
				RunID:     "run",
				SkillName: "review",
			},
			want: Observation{
				Type:      EventSkillActivated,
				AgentID:   "agent",
				RunID:     "run",
				SkillName: "review",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ObservationFromEvent(tt.event); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ObservationFromEvent() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestObservationFromEventErrorContract(t *testing.T) {
	rawErr := errors.New("observation-contract-raw-model-error")

	tests := []struct {
		name  string
		event Event
		want  Observation
	}{
		{
			name: "raw error marks failed without exposing category",
			event: Event{
				Type:  EventAfterModel,
				Error: rawErr,
			},
			want: Observation{
				Type:   EventAfterModel,
				Failed: true,
			},
		},
		{
			name: "error category marks failed without raw error",
			event: Event{
				Type:          EventAfterModel,
				ErrorCategory: ErrorCategoryModel,
			},
			want: Observation{
				Type:          EventAfterModel,
				ErrorCategory: ErrorCategoryModel,
				Failed:        true,
			},
		},
		{
			name: "model subcategory is normalized",
			event: Event{
				Type:                  EventAfterModel,
				ErrorCategory:         ErrorCategoryModel,
				ModelErrorSubcategory: "provider-secret-subcategory",
			},
			want: Observation{
				Type:                  EventAfterModel,
				ErrorCategory:         ErrorCategoryModel,
				ModelErrorSubcategory: ModelErrorSubcategoryUnknown,
				Failed:                true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ObservationFromEvent(tt.event)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ObservationFromEvent() = %#v, want %#v", got, tt.want)
			}
			assertObservationDoesNotContain(t, got, rawErr.Error())
		})
	}
}

func TestObservationStableTelemetryAttributeContractCoversFields(t *testing.T) {
	fieldAttrs := map[string][]string{
		"Type":                  {TelemetryAttrEvent},
		"AgentID":               {TelemetryAttrAgentID},
		"RunID":                 {TelemetryAttrRunID},
		"SubagentID":            {TelemetryAttrSubagentID},
		"ToolName":              {TelemetryAttrToolName},
		"ToolRisk":              {TelemetryAttrToolRisk},
		"ToolSchemaHash":        {TelemetryAttrToolSchemaHash},
		"SkillName":             {TelemetryAttrSkillName},
		"TraceID":               {TelemetryAttrTraceID},
		"SpanID":                {TelemetryAttrSpanID},
		"TraceState":            {TelemetryAttrTraceState},
		"RequestID":             {TelemetryAttrRequestID},
		"ParentRequestID":       {TelemetryAttrParentRequestID},
		"Round":                 {TelemetryAttrRound},
		"Duration":              {TelemetryAttrDurationMS},
		"EstimatedTokens":       {TelemetryAttrEstimatedTokens},
		"ToolTiming":            {TelemetryAttrToolTimingValidationMS, TelemetryAttrToolTimingApprovalMS, TelemetryAttrToolTimingExecutionMS},
		"TokenUsage":            {TelemetryAttrTokensInput, TelemetryAttrTokensOutput, TelemetryAttrTokensTotal},
		"StreamTelemetry":       {TelemetryAttrStreamTimeToFirstTokenMS, TelemetryAttrStreamDeltaCount, TelemetryAttrStreamByteCount, TelemetryAttrStreamThroughputBytesPerSec},
		"ProviderDiagnostics":   {TelemetryAttrProviderName, TelemetryAttrProviderHTTPStatus, TelemetryAttrProviderEndpointHost, TelemetryAttrProviderRequestID, TelemetryAttrProviderRetryAfter, TelemetryAttrProviderRateLimitLimit, TelemetryAttrProviderRateLimitRemaining, TelemetryAttrProviderRateLimitReset},
		"ToolResultMetadata":    {TelemetryAttrToolResultContentBytes, TelemetryAttrToolResultMetadataKeys, TelemetryAttrToolResultMCPIsError},
		"ModelErrorSubcategory": {TelemetryAttrErrorModelSubcategory},
		"Approved":              {TelemetryAttrApprovalApproved},
		"ApprovalReason":        {TelemetryAttrApprovalReason},
		"ErrorCategory":         {TelemetryAttrErrorCategory},
		"Failed":                {TelemetryAttrFailed},
	}

	stableAttrs := stringSet(StableTelemetryAttributeNames())
	observationType := reflect.TypeOf(Observation{})
	for i := 0; i < observationType.NumField(); i++ {
		field := observationType.Field(i)
		attrs, ok := fieldAttrs[field.Name]
		if !ok {
			t.Fatalf("Observation.%s has no stable telemetry attribute contract", field.Name)
		}
		for _, attr := range attrs {
			if _, ok := stableAttrs[attr]; !ok {
				t.Fatalf("Observation.%s maps to %q, missing from StableTelemetryAttributeNames", field.Name, attr)
			}
		}
	}
	for fieldName := range fieldAttrs {
		if _, ok := observationType.FieldByName(fieldName); !ok {
			t.Fatalf("stable telemetry attribute contract references missing Observation.%s", fieldName)
		}
	}
}

func TestObservationForbiddenTelemetryContractCoversSensitiveEventClasses(t *testing.T) {
	forbidden := stringSet(ForbiddenTelemetryFieldNames())
	for _, name := range []string{
		"prompts",
		"message_content",
		"tool_arguments",
		"tool_result_content",
		"tool_result_metadata_values",
		"raw_errors",
		"credentials",
		"full_provider_urls",
		"mcp_environment_values",
	} {
		if _, ok := forbidden[name]; !ok {
			t.Fatalf("ForbiddenTelemetryFieldNames missing %q", name)
		}
	}

	observationType := reflect.TypeOf(Observation{})
	for _, fieldName := range []string{
		"Message",
		"Messages",
		"SystemPrompt",
		"ToolCall",
		"ToolResult",
		"Error",
		"MCPServers",
		"MCPServerConfig",
		"MCPEnvironment",
		"Credentials",
	} {
		if _, ok := observationType.FieldByName(fieldName); ok {
			t.Fatalf("Observation exposes forbidden field %s", fieldName)
		}
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
