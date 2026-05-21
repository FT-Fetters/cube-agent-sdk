package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultAndNoopObserverDoNotAffectRun(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		options []Option
	}{
		{name: "default"},
		{name: "explicit noop", options: []Option{WithObserver(NoopObserver{})}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &recordingModel{responses: []ModelResponse{
				{Message: Message{Role: RoleAssistant, Content: "ok"}},
			}}
			bot, err := New(Config{ID: "noop-observer-agent", SystemPrompt: "base"}, model, tt.options...)
			if err != nil {
				t.Fatal(err)
			}

			response, err := bot.Run(ctx, "hello")
			if err != nil {
				t.Fatal(err)
			}
			if response.Content != "ok" {
				t.Fatalf("response content = %q, want ok", response.Content)
			}
		})
	}
}

func TestObserverReceivesSanitizedLifecycleMetadata(t *testing.T) {
	ctx := context.Background()
	const secret = "do-not-record-sensitive-content"
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": secret}}}},
		{Message: Message{Role: RoleAssistant, Content: secret}},
	}}
	compactor := &recordingCompactor{
		result: []Message{{Role: RoleSystem, Content: "summary"}},
	}
	bot, err := New(Config{
		ID:           "observed-agent",
		SystemPrompt: "base",
		Compact: CompactConfig{
			MaxTokens: 2,
			Threshold: 1,
		},
	}, model,
		WithObserver(recorder),
		WithCompactor(compactor),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 1 })),
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: secret}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	bot.AppendMessage(Message{Role: RoleUser, Content: secret})

	if _, err := bot.Run(ctx, secret); err != nil {
		t.Fatal(err)
	}
	child, err := bot.SpawnSubagent(ctx, SubagentOptions{
		ID:    "worker-1",
		Model: &recordingModel{responses: []ModelResponse{{Message: Message{Role: RoleAssistant, Content: secret}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bot.SendMessageToSubagent(ctx, child.ID(), secret); err != nil {
		t.Fatal(err)
	}

	observations := recorder.Observations()
	if len(observations) == 0 {
		t.Fatal("observer did not receive observations")
	}
	for _, observation := range observations {
		if observation.AgentID == "" {
			t.Fatalf("observation agent ID = %q, want non-empty", observation.AgentID)
		}
		assertObservationDoesNotContain(t, observation, secret)
	}

	beforeModel := firstObservationOfType(t, observations, EventBeforeModel)
	afterModel := firstObservationOfType(t, observations, EventAfterModel)
	if beforeModel.RequestID == "" || beforeModel.RequestID != afterModel.RequestID || beforeModel.Round != 1 {
		t.Fatalf("model observation request/round = %#v/%#v, want matching request and first round", beforeModel, afterModel)
	}
	if beforeModel.ParentRequestID != "" || afterModel.ParentRequestID != "" {
		t.Fatalf("first model observation parent request IDs = %q/%q, want empty roots", beforeModel.ParentRequestID, afterModel.ParentRequestID)
	}
	if afterModel.Duration <= 0 || afterModel.EstimatedTokens <= 0 || afterModel.Failed {
		t.Fatalf("after model observation = %#v, want duration, tokens, and success", afterModel)
	}

	afterTool := firstObservationOfType(t, observations, EventAfterTool)
	if afterTool.ToolName != "echo" || afterTool.RequestID == "" || afterTool.Duration <= 0 || afterTool.EstimatedTokens <= 0 {
		t.Fatalf("after tool observation = %#v, want safe tool metadata", afterTool)
	}
	if afterTool.ParentRequestID != beforeModel.RequestID {
		t.Fatalf("after tool observation parent request ID = %q, want model request ID %q", afterTool.ParentRequestID, beforeModel.RequestID)
	}

	afterApproval := firstObservationOfType(t, observations, EventAfterApproval)
	if afterApproval.ToolName != "echo" || afterApproval.RequestID == "" || afterApproval.Duration <= 0 || afterApproval.Failed {
		t.Fatalf("after approval observation = %#v, want successful approval metadata", afterApproval)
	}
	if afterApproval.ParentRequestID != beforeModel.RequestID {
		t.Fatalf("after approval observation parent request ID = %q, want model request ID %q", afterApproval.ParentRequestID, beforeModel.RequestID)
	}

	afterCompact := firstObservationOfType(t, observations, EventAfterCompact)
	if afterCompact.RequestID == "" || afterCompact.Duration <= 0 || afterCompact.EstimatedTokens <= 0 {
		t.Fatalf("after compact observation = %#v, want compact audit metadata", afterCompact)
	}

	subagentMessage := firstObservationOfType(t, observations, EventSubagentMessage)
	if subagentMessage.SubagentID != "worker-1" || subagentMessage.RequestID == "" {
		t.Fatalf("subagent observation = %#v, want subagent metadata", subagentMessage)
	}
}

func TestToolSchemaHashTelemetryIsDeterministicAcrossSchemaMapOrder(t *testing.T) {
	ctx := context.Background()
	const schemaSecret = "raw-schema-description"
	var want string

	for i := 0; i < 20; i++ {
		order := []string{"account_id", "include_history", "limit", "region", "tier", "active"}
		if i%2 == 1 {
			order = []string{"active", "tier", "region", "limit", "include_history", "account_id"}
		}
		_, observations := runObservedSchemaTool(t, ctx, observedSchemaToolOptions{
			propertyOrder: order,
			argumentText:  "safe argument",
			schemaText:    schemaSecret,
		})

		got := toolSchemaHashFromObservation(t, firstObservationOfType(t, observations, EventBeforeTool))
		if got == "" {
			t.Fatal("tool schema hash = empty, want deterministic hash")
		}
		if strings.Contains(got, schemaSecret) {
			t.Fatalf("tool schema hash leaked raw schema text: %q", got)
		}
		if want == "" {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("tool schema hash = %q, want deterministic %q", got, want)
		}
	}
}

func TestToolLifecycleTelemetryCarriesSchemaHashWhenSchemaExists(t *testing.T) {
	ctx := context.Background()
	events, observations := runObservedSchemaTool(t, ctx, observedSchemaToolOptions{
		propertyOrder: []string{"account_id", "include_history"},
		argumentText:  "customer-123",
		schemaText:    "schema description",
	})

	want := toolSchemaHashFromObservation(t, firstObservationOfType(t, observations, EventBeforeTool))
	if want == "" {
		t.Fatal("tool schema hash = empty, want hash on tool lifecycle telemetry")
	}

	for _, eventType := range toolLifecycleEventTypes() {
		event := firstEventOfType(t, events, eventType)
		if got := toolSchemaHashFromEvent(t, event); got != want {
			t.Fatalf("%s event tool schema hash = %q, want %q", eventType, got, want)
		}
		observation := firstObservationOfType(t, observations, eventType)
		if got := toolSchemaHashFromObservation(t, observation); got != want {
			t.Fatalf("%s observation tool schema hash = %q, want %q", eventType, got, want)
		}
	}
}

func TestToolSchemaHashTelemetryIsEmptyWithoutSchema(t *testing.T) {
	ctx := context.Background()
	events, observations := runObservedSchemaTool(t, ctx, observedSchemaToolOptions{
		argumentText: "customer-123",
	})

	for _, eventType := range toolLifecycleEventTypes() {
		event := firstEventOfType(t, events, eventType)
		if got := toolSchemaHashFromEvent(t, event); got != "" {
			t.Fatalf("%s event tool schema hash = %q, want empty", eventType, got)
		}
		observation := firstObservationOfType(t, observations, eventType)
		if got := toolSchemaHashFromObservation(t, observation); got != "" {
			t.Fatalf("%s observation tool schema hash = %q, want empty", eventType, got)
		}
	}
}

func TestToolSchemaHashObservationDoesNotLeakArgumentsResultsOrRawSchema(t *testing.T) {
	ctx := context.Background()
	const (
		argumentSecret = "argument-secret-value"
		resultSecret   = "result-secret-value"
		schemaSecret   = "raw-schema-secret-value"
	)
	_, observations := runObservedSchemaTool(t, ctx, observedSchemaToolOptions{
		propertyOrder: []string{"account_id", "include_history"},
		argumentText:  argumentSecret,
		resultText:    resultSecret,
		schemaText:    schemaSecret,
	})

	for _, observation := range observations {
		assertObservationDoesNotContain(t, observation, argumentSecret)
		assertObservationDoesNotContain(t, observation, resultSecret)
		assertObservationDoesNotContain(t, observation, schemaSecret)
	}
}

func TestToolResultMetadataObservationIncludesSafeSizeSortedKeysAndMCPStatus(t *testing.T) {
	const (
		resultContent          = "safe result payload"
		metadataValueSecret    = "metadata-value-secret"
		structuredValueSecret  = "structured-content-secret"
		unstructuredRawMessage = "raw tool result error secret"
	)
	observation := ObservationFromEvent(Event{
		Type: EventAfterTool,
		ToolResult: ToolResult{
			Content: resultContent,
			Metadata: map[string]any{
				"zeta":                 metadataValueSecret,
				"mcpStructuredContent": map[string]any{"token": structuredValueSecret},
				"mcpIsError":           true,
				"alpha":                []any{metadataValueSecret},
			},
		},
		Error: errors.New(unstructuredRawMessage),
	})

	metadata := toolResultMetadataFromObservation(t, observation)
	if metadata.contentBytes != len(resultContent) {
		t.Fatalf("tool result content bytes = %d, want %d", metadata.contentBytes, len(resultContent))
	}
	wantKeys := []string{"alpha", "mcpIsError", "mcpStructuredContent", "zeta"}
	if !reflect.DeepEqual(metadata.metadataKeys, wantKeys) {
		t.Fatalf("tool result metadata keys = %#v, want %#v", metadata.metadataKeys, wantKeys)
	}
	if metadata.mcpIsError == nil || *metadata.mcpIsError != true {
		t.Fatalf("tool result MCP error status = %#v, want true", metadata.mcpIsError)
	}
	for _, unsafe := range []string{
		resultContent,
		metadataValueSecret,
		structuredValueSecret,
		unstructuredRawMessage,
	} {
		assertObservationDoesNotContain(t, observation, unsafe)
	}
}

func TestToolResultMetadataObservationFromToolExecutionDoesNotLeakPayloads(t *testing.T) {
	ctx := context.Background()
	const (
		resultSecret          = "classified-tool-result"
		metadataValueSecret   = "classified-metadata-value"
		structuredValueSecret = "classified-structured-content"
		rawErrorSecret        = "classified-raw-tool-error"
	)
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "lookup", Arguments: map[string]any{"query": "safe"}}}},
	}}
	bot, err := New(Config{ID: "tool-result-metadata-agent", SystemPrompt: "base"}, model,
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName:        "lookup",
			ToolDescription: "Lookup data",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{
					CallID:  call.ID,
					Name:    call.Name,
					Content: resultSecret,
					Metadata: map[string]any{
						"safeKey":              metadataValueSecret,
						"mcpStructuredContent": map[string]any{"secret": structuredValueSecret},
						"mcpIsError":           false,
					},
				}, errors.New(rawErrorSecret)
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "run lookup")
	if err == nil {
		t.Fatal("Run error = nil, want tool error")
	}

	observation := firstObservationOfType(t, recorder.Observations(), EventAfterTool)
	if !observation.Failed || observation.ErrorCategory != ErrorCategoryTool {
		t.Fatalf("after tool observation = %#v, want failed tool category", observation)
	}
	metadata := toolResultMetadataFromObservation(t, observation)
	if metadata.contentBytes != len(resultSecret) {
		t.Fatalf("tool result content bytes = %d, want %d", metadata.contentBytes, len(resultSecret))
	}
	wantKeys := []string{"mcpIsError", "mcpStructuredContent", "safeKey"}
	if !reflect.DeepEqual(metadata.metadataKeys, wantKeys) {
		t.Fatalf("tool result metadata keys = %#v, want %#v", metadata.metadataKeys, wantKeys)
	}
	if metadata.mcpIsError == nil || *metadata.mcpIsError != false {
		t.Fatalf("tool result MCP error status = %#v, want false", metadata.mcpIsError)
	}
	for _, unsafe := range []string{
		resultSecret,
		metadataValueSecret,
		structuredValueSecret,
		rawErrorSecret,
	} {
		assertObservationDoesNotContain(t, observation, unsafe)
	}
}

func TestToolLifecycleTimingObservationSegmentsSuccessDeniedAndValidationFailure(t *testing.T) {
	ctx := context.Background()
	const (
		argumentSecret = "timing-argument-secret"
		resultSecret   = "timing-result-secret"
		metadataSecret = "timing-metadata-secret"
		rawErrorSecret = "timing-raw-error-secret"
	)
	tests := []struct {
		name          string
		toolCall      ToolCall
		options       []Option
		wantErr       error
		wantCategory  ErrorCategory
		wantApproval  bool
		wantExecution bool
	}{
		{
			name: "success",
			toolCall: ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: map[string]any{"query": argumentSecret},
			},
			options: []Option{WithTools(ToolFunc{
				ToolName:        "lookup",
				ToolDescription: "Lookup data",
				Parameters:      requiredQuerySchema(),
				ToolRisk:        ToolRiskRead,
				Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
					return ToolResult{
						CallID:  call.ID,
						Name:    call.Name,
						Content: resultSecret,
						Metadata: map[string]any{
							"secretKey": metadataSecret,
						},
					}, nil
				},
			})},
			wantApproval:  true,
			wantExecution: true,
		},
		{
			name: "approval denied",
			toolCall: ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: map[string]any{"query": argumentSecret},
			},
			options: []Option{
				WithTools(ToolFunc{
					ToolName:        "lookup",
					ToolDescription: "Lookup data",
					Parameters:      requiredQuerySchema(),
					ToolRisk:        ToolRiskWrite,
					Fn: func(context.Context, ToolCall) (ToolResult, error) {
						t.Fatal("tool should not execute when approval is denied")
						return ToolResult{}, nil
					},
				}),
				WithApprovalPolicy(ApprovalFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
					return ApprovalDecision{Approved: false, Reason: "blocked by policy"}, nil
				})),
			},
			wantErr:      ErrApprovalDenied,
			wantCategory: ErrorCategoryApproval,
			wantApproval: true,
		},
		{
			name: "validation failure",
			toolCall: ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: map[string]any{"query": 42},
			},
			options: []Option{WithTools(ToolFunc{
				ToolName:        "lookup",
				ToolDescription: "Lookup data",
				Parameters:      requiredQuerySchema(),
				ToolRisk:        ToolRiskRead,
				Fn: func(context.Context, ToolCall) (ToolResult, error) {
					t.Fatal("tool should not execute when validation fails")
					return ToolResult{}, nil
				},
			})},
			wantErr:      ErrToolValidation,
			wantCategory: ErrorCategorySchema,
		},
		{
			name: "approval policy error",
			toolCall: ToolCall{
				ID:        "call-1",
				Name:      "lookup",
				Arguments: map[string]any{"query": argumentSecret},
			},
			options: []Option{
				WithTools(ToolFunc{
					ToolName:        "lookup",
					ToolDescription: "Lookup data",
					Parameters:      requiredQuerySchema(),
					ToolRisk:        ToolRiskWrite,
					Fn: func(context.Context, ToolCall) (ToolResult, error) {
						t.Fatal("tool should not execute when approval returns an error")
						return ToolResult{}, nil
					},
				}),
				WithApprovalPolicy(ApprovalFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
					return ApprovalDecision{}, errors.New(rawErrorSecret)
				})),
			},
			wantErr:      errors.New(rawErrorSecret),
			wantCategory: ErrorCategoryApproval,
			wantApproval: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingObserver{}
			model := &recordingModel{responses: []ModelResponse{
				{ToolCalls: []ToolCall{tt.toolCall}},
				{Message: Message{Role: RoleAssistant, Content: "done"}},
			}}
			options := append([]Option{WithObserver(recorder)}, tt.options...)
			bot, err := New(Config{ID: "tool-timing-agent", SystemPrompt: "base"}, model, options...)
			if err != nil {
				t.Fatal(err)
			}

			_, err = bot.Run(ctx, "use tool")
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Run error = nil, want %v", tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			afterTool := firstObservationOfType(t, recorder.Observations(), EventAfterTool)
			timing := toolLifecycleTimingFromObservation(t, afterTool)
			assertNonNegativeToolLifecycleTiming(t, timing)
			if afterTool.Duration <= 0 {
				t.Fatalf("after tool duration = %s, want positive total duration", afterTool.Duration)
			}
			if timing.validation <= 0 {
				t.Fatalf("tool validation timing = %s, want positive duration", timing.validation)
			}
			if tt.wantApproval && timing.approval <= 0 {
				t.Fatalf("tool approval timing = %s, want positive duration", timing.approval)
			}
			if !tt.wantApproval && timing.approval != 0 {
				t.Fatalf("tool approval timing = %s, want zero before approval starts", timing.approval)
			}
			if tt.wantExecution && timing.execution <= 0 {
				t.Fatalf("tool execution timing = %s, want positive duration", timing.execution)
			}
			if !tt.wantExecution && timing.execution != 0 {
				t.Fatalf("tool execution timing = %s, want zero when tool execution is skipped", timing.execution)
			}
			if tt.wantCategory != "" && (!afterTool.Failed || afterTool.ErrorCategory != tt.wantCategory) {
				t.Fatalf("after tool error fields = failed %t category %q, want failed %q", afterTool.Failed, afterTool.ErrorCategory, tt.wantCategory)
			}
			for _, unsafe := range []string{argumentSecret, resultSecret, metadataSecret, rawErrorSecret} {
				assertObservationDoesNotContain(t, afterTool, unsafe)
			}
		})
	}
}

func TestObserverReceivesErrorCategoryWithoutInterruptingErrorPath(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	cause := errors.New("model unavailable")
	bot, err := New(Config{ID: "failing-agent", SystemPrompt: "base"}, failingModel{err: cause},
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "hello")
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want model cause", err)
	}

	afterModel := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	if !afterModel.Failed || afterModel.ErrorCategory != ErrorCategoryModel {
		t.Fatalf("after model observation = %#v, want failed model category", afterModel)
	}
	if afterModel.RequestID == "" || afterModel.Round != 1 || afterModel.Duration <= 0 {
		t.Fatalf("after model audit metadata = %#v, want request ID, round, and duration", afterModel)
	}
}

func TestObserverPanicDoesNotInterruptRun(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "ok"}},
	}}
	bot, err := New(Config{ID: "panic-observer-agent", SystemPrompt: "base"}, model,
		WithObserver(ObserverFunc(func(context.Context, Observation) {
			panic("observer failed")
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := bot.Run(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "ok" {
		t.Fatalf("response content = %q, want ok", response.Content)
	}
}

func TestObserversFanOutToEachChild(t *testing.T) {
	ctx := context.Background()
	first := &recordingObserver{}
	second := &recordingObserver{}
	observation := Observation{
		Type:            EventAfterModel,
		AgentID:         "fanout-agent",
		RunID:           "run-1",
		RequestID:       "request-1",
		Round:           2,
		EstimatedTokens: 12,
	}

	Observers(first, second).Observe(ctx, observation)

	for name, recorder := range map[string]*recordingObserver{
		"first":  first,
		"second": second,
	} {
		observations := recorder.Observations()
		if !reflect.DeepEqual(observations, []Observation{observation}) {
			t.Fatalf("%s observations = %#v, want %#v", name, observations, []Observation{observation})
		}
	}
}

func TestObserversIgnoreNilChildren(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	var nilObserver Observer
	observation := Observation{Type: EventBeforeModel, AgentID: "nil-child-agent"}

	Observers(nil, nilObserver, recorder, nil).Observe(ctx, observation)
	MultiObserver{nil, recorder, nilObserver}.Observe(ctx, observation)

	observations := recorder.Observations()
	if !reflect.DeepEqual(observations, []Observation{observation, observation}) {
		t.Fatalf("observations = %#v, want two delivered observations", observations)
	}
}

func TestObserversIsolateChildPanics(t *testing.T) {
	ctx := context.Background()
	first := &recordingObserver{}
	second := &recordingObserver{}
	observation := Observation{Type: EventAfterTool, AgentID: "panic-child-agent", ToolName: "echo"}

	Observers(
		first,
		ObserverFunc(func(context.Context, Observation) {
			panic("child observer failed")
		}),
		second,
	).Observe(ctx, observation)

	if !reflect.DeepEqual(first.Observations(), []Observation{observation}) {
		t.Fatalf("first observations = %#v, want observation", first.Observations())
	}
	if !reflect.DeepEqual(second.Observations(), []Observation{observation}) {
		t.Fatalf("second observations = %#v, want observation after panic", second.Observations())
	}
}

func TestObserversWithObserverReceivesSanitizedObservations(t *testing.T) {
	ctx := context.Background()
	const secret = "fanout-secret-content"
	first := &recordingObserver{}
	second := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: secret}},
	}}
	bot, err := New(Config{ID: "fanout-with-observer-agent", SystemPrompt: secret}, model,
		WithObserver(Observers(first, second)),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := bot.Run(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != secret {
		t.Fatalf("response content = %q, want model content", response.Content)
	}

	for name, recorder := range map[string]*recordingObserver{
		"first":  first,
		"second": second,
	} {
		observations := recorder.Observations()
		if len(observations) == 0 {
			t.Fatalf("%s observer did not receive observations", name)
		}
		for _, observation := range observations {
			assertObservationDoesNotContain(t, observation, secret)
		}
	}
}

type recordingObserver struct {
	observations []Observation
}

func (r *recordingObserver) Observe(ctx context.Context, observation Observation) {
	r.observations = append(r.observations, observation)
}

func (r *recordingObserver) Observations() []Observation {
	return append([]Observation(nil), r.observations...)
}

func firstObservationOfType(t *testing.T, observations []Observation, eventType EventType) Observation {
	t.Helper()
	for _, observation := range observations {
		if observation.Type == eventType {
			return observation
		}
	}
	t.Fatalf("observations = %#v, want %s", observations, eventType)
	return Observation{}
}

func assertObservationDoesNotContain(t *testing.T, observation Observation, secret string) {
	t.Helper()
	assertValueDoesNotContain(t, reflect.ValueOf(observation), secret, "Observation")
}

func assertValueDoesNotContain(t *testing.T, value reflect.Value, secret string, path string) {
	t.Helper()
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		if strings.Contains(value.String(), secret) {
			t.Fatalf("observation leaked sensitive content in %s", path)
		}
	case reflect.Struct:
		valueType := value.Type()
		for i := 0; i < value.NumField(); i++ {
			fieldInfo := valueType.Field(i)
			if fieldInfo.PkgPath != "" {
				continue
			}
			assertValueDoesNotContain(t, value.Field(i), secret, path+"."+fieldInfo.Name)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			assertValueDoesNotContain(t, value.Index(i), secret, path)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			assertValueDoesNotContain(t, key, secret, path)
			assertValueDoesNotContain(t, value.MapIndex(key), secret, path)
		}
	}
}

type observedSchemaToolOptions struct {
	propertyOrder []string
	argumentText  string
	resultText    string
	schemaText    string
}

func requiredQuerySchema() *ToolParametersSchema {
	return &ToolParametersSchema{
		Type:     SchemaTypeObject,
		Required: []string{"query"},
		Properties: map[string]ToolParametersSchema{
			"query": {Type: SchemaTypeString},
		},
	}
}

func runObservedSchemaTool(t *testing.T, ctx context.Context, options observedSchemaToolOptions) ([]Event, []Observation) {
	t.Helper()
	recorder := &recordingObserver{}
	var events []Event
	schema := observedToolSchema(options.propertyOrder, options.schemaText)
	resultText := options.resultText
	if resultText == "" {
		resultText = "tool result"
	}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "lookup_account",
			Arguments: map[string]any{
				"account_id":      options.argumentText,
				"include_history": true,
				"limit":           3,
				"region":          "us",
				"tier":            "pro",
				"active":          true,
			},
		}}},
		{Message: Message{Role: RoleAssistant, Content: "ok"}},
	}}
	bot, err := New(Config{ID: "tool-schema-hash-agent", SystemPrompt: "base"}, model,
		WithObserver(recorder),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
		WithTools(ToolFunc{
			ToolName:        "lookup_account",
			ToolDescription: "Lookup account",
			Parameters:      schema,
			ToolRisk:        ToolRiskRead,
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: resultText}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "lookup"); err != nil {
		t.Fatal(err)
	}
	return events, recorder.Observations()
}

func observedToolSchema(propertyOrder []string, schemaText string) *ToolParametersSchema {
	if len(propertyOrder) == 0 {
		return nil
	}
	schema := &ToolParametersSchema{
		Type:        SchemaTypeObject,
		Description: schemaText,
		Required:    []string{"account_id", "include_history"},
		Properties:  make(map[string]ToolParametersSchema, len(propertyOrder)),
	}
	for _, name := range propertyOrder {
		switch name {
		case "account_id":
			schema.Properties[name] = ToolParametersSchema{Type: SchemaTypeString, Description: schemaText}
		case "include_history":
			schema.Properties[name] = ToolParametersSchema{Type: SchemaTypeBoolean}
		case "limit":
			schema.Properties[name] = ToolParametersSchema{Type: SchemaTypeInteger}
		case "active":
			schema.Properties[name] = ToolParametersSchema{Type: SchemaTypeBoolean}
		default:
			schema.Properties[name] = ToolParametersSchema{Type: SchemaTypeString}
		}
	}
	return schema
}

func toolLifecycleEventTypes() []EventType {
	return []EventType{EventBeforeApproval, EventAfterApproval, EventBeforeTool, EventAfterTool}
}

func toolSchemaHashFromEvent(t *testing.T, event Event) string {
	t.Helper()
	return stringFieldByName(t, event, "ToolSchemaHash")
}

func toolSchemaHashFromObservation(t *testing.T, observation Observation) string {
	t.Helper()
	return stringFieldByName(t, observation, "ToolSchemaHash")
}

type observedToolResultMetadata struct {
	contentBytes int
	metadataKeys []string
	mcpIsError   *bool
}

type observedToolLifecycleTiming struct {
	validation time.Duration
	approval   time.Duration
	execution  time.Duration
}

func toolLifecycleTimingFromObservation(t *testing.T, observation Observation) observedToolLifecycleTiming {
	t.Helper()
	value := reflect.ValueOf(observation)
	field := value.FieldByName("ToolTiming")
	if !field.IsValid() {
		t.Fatal("Observation.ToolTiming field is missing")
	}
	return observedToolLifecycleTiming{
		validation: durationFieldByName(t, field, "Validation"),
		approval:   durationFieldByName(t, field, "Approval"),
		execution:  durationFieldByName(t, field, "Execution"),
	}
}

func observationWithToolLifecycleTiming(t *testing.T, observation Observation, validation, approval, execution time.Duration) Observation {
	t.Helper()
	value := reflect.ValueOf(&observation).Elem()
	field := value.FieldByName("ToolTiming")
	if !field.IsValid() {
		t.Fatal("Observation.ToolTiming field is missing")
	}
	if field.Kind() != reflect.Struct || !field.CanSet() {
		t.Fatalf("Observation.ToolTiming = %s settable=%t, want settable struct", field.Kind(), field.CanSet())
	}
	setDurationFieldByName(t, field, "Validation", validation)
	setDurationFieldByName(t, field, "Approval", approval)
	setDurationFieldByName(t, field, "Execution", execution)
	return observation
}

func assertNonNegativeToolLifecycleTiming(t *testing.T, timing observedToolLifecycleTiming) {
	t.Helper()
	if timing.validation < 0 || timing.approval < 0 || timing.execution < 0 {
		t.Fatalf("tool lifecycle timing = %#v, want non-negative segment durations", timing)
	}
}

func toolResultMetadataFromObservation(t *testing.T, observation Observation) observedToolResultMetadata {
	t.Helper()
	value := reflect.ValueOf(observation)
	field := value.FieldByName("ToolResultMetadata")
	if !field.IsValid() {
		t.Fatal("Observation.ToolResultMetadata field is missing")
	}
	return observedToolResultMetadata{
		contentBytes: intFieldByName(t, field, "ContentBytes"),
		metadataKeys: stringSliceFieldByName(t, field, "MetadataKeys"),
		mcpIsError:   optionalBoolFieldByName(t, field, "MCPIsError"),
	}
}

func observationWithToolSchemaHash(t *testing.T, observation Observation, hash string) Observation {
	t.Helper()
	value := reflect.ValueOf(&observation).Elem()
	field := value.FieldByName("ToolSchemaHash")
	if !field.IsValid() {
		t.Fatal("Observation.ToolSchemaHash field is missing")
	}
	if field.Kind() != reflect.String || !field.CanSet() {
		t.Fatalf("Observation.ToolSchemaHash = %s settable=%t, want settable string", field.Kind(), field.CanSet())
	}
	field.SetString(hash)
	return observation
}

func intFieldByName(t *testing.T, value reflect.Value, name string) int {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s.%s field is missing", value.Type(), name)
	}
	if field.Kind() != reflect.Int {
		t.Fatalf("%s.%s kind = %s, want int", value.Type(), name, field.Kind())
	}
	return int(field.Int())
}

func durationFieldByName(t *testing.T, value reflect.Value, name string) time.Duration {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s.%s field is missing", value.Type(), name)
	}
	if field.Type() != reflect.TypeOf(time.Duration(0)) {
		t.Fatalf("%s.%s type = %s, want time.Duration", value.Type(), name, field.Type())
	}
	return time.Duration(field.Int())
}

func setDurationFieldByName(t *testing.T, value reflect.Value, name string, duration time.Duration) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s.%s field is missing", value.Type(), name)
	}
	if field.Type() != reflect.TypeOf(time.Duration(0)) || !field.CanSet() {
		t.Fatalf("%s.%s type = %s settable=%t, want settable time.Duration", value.Type(), name, field.Type(), field.CanSet())
	}
	field.SetInt(int64(duration))
}

func stringFieldByName(t *testing.T, value any, name string) string {
	t.Helper()
	reflected := reflect.ValueOf(value)
	field := reflected.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%T.%s field is missing", value, name)
	}
	if field.Kind() != reflect.String {
		t.Fatalf("%T.%s kind = %s, want string", value, name, field.Kind())
	}
	return field.String()
}

func stringSliceFieldByName(t *testing.T, value reflect.Value, name string) []string {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s.%s field is missing", value.Type(), name)
	}
	if field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.String {
		t.Fatalf("%s.%s kind = %s, want []string", value.Type(), name, field.Kind())
	}
	keys := make([]string, field.Len())
	for i := 0; i < field.Len(); i++ {
		keys[i] = field.Index(i).String()
	}
	return keys
}

func optionalBoolFieldByName(t *testing.T, value reflect.Value, name string) *bool {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s.%s field is missing", value.Type(), name)
	}
	switch field.Kind() {
	case reflect.Pointer:
		if field.IsNil() {
			return nil
		}
		if field.Type().Elem().Kind() != reflect.Bool {
			t.Fatalf("%s.%s kind = %s, want *bool", value.Type(), name, field.Kind())
		}
		got := field.Elem().Bool()
		return &got
	case reflect.Bool:
		got := field.Bool()
		return &got
	default:
		t.Fatalf("%s.%s kind = %s, want *bool", value.Type(), name, field.Kind())
		return nil
	}
}
