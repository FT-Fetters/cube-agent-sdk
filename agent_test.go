package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAgentWrapsApprovalDeniedWithStructuredError(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "danger", Arguments: map[string]any{"path": "/"}}}},
	}}
	var events []Event
	bot, err := New(Config{ID: "audit-agent", SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "danger",
			ToolDescription: "Dangerous operation",
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				t.Fatal("tool should not execute when approval is denied")
				return ToolResult{}, nil
			},
		}),
		WithApprovalPolicy(ApprovalFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
			return ApprovalDecision{Approved: false, Reason: "blocked"}, nil
		})),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "run danger")
	if !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("err = %v, want ErrApprovalDenied compatibility", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryApproval || agentErr.Operation != "tool.approval" {
		t.Fatalf("agent error category/operation = %q/%q, want approval/tool.approval", agentErr.Category, agentErr.Operation)
	}
	if agentErr.AgentID != "audit-agent" || agentErr.ToolName != "danger" || agentErr.Round != 1 {
		t.Fatalf("agent error context = %#v, want agent/tool/round context", agentErr)
	}
	if agentErr.RequestID == "" {
		t.Fatalf("agent error request ID = %q, want non-empty request ID", agentErr.RequestID)
	}
	beforeModel := firstEventOfType(t, events, EventBeforeModel)
	if agentErr.ParentRequestID != beforeModel.RequestID {
		t.Fatalf("agent error parent request ID = %q, want model request ID %q", agentErr.ParentRequestID, beforeModel.RequestID)
	}
}

func TestAgentLifecycleEventsCarryAuditFields(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	var events []Event
	bot, err := New(Config{ID: "audit-agent", SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: call.Arguments["text"].(string)}, nil
			},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "use echo"); err != nil {
		t.Fatal(err)
	}

	beforeModel := firstEventOfType(t, events, EventBeforeModel)
	afterModel := firstEventOfType(t, events, EventAfterModel)
	if beforeModel.RequestID == "" || beforeModel.RequestID != afterModel.RequestID {
		t.Fatalf("model request IDs = %q/%q, want matching non-empty IDs", beforeModel.RequestID, afterModel.RequestID)
	}
	if beforeModel.Round != 1 || afterModel.Round != 1 {
		t.Fatalf("model rounds = %d/%d, want first round", beforeModel.Round, afterModel.Round)
	}
	if beforeModel.EstimatedTokens <= 0 || afterModel.EstimatedTokens != beforeModel.EstimatedTokens {
		t.Fatalf("model estimated tokens = %d/%d, want positive matching estimate", beforeModel.EstimatedTokens, afterModel.EstimatedTokens)
	}
	if afterModel.Duration <= 0 || afterModel.ErrorCategory != "" {
		t.Fatalf("after model duration/category = %s/%q, want positive duration and no category", afterModel.Duration, afterModel.ErrorCategory)
	}

	beforeTool := firstEventOfType(t, events, EventBeforeTool)
	afterTool := firstEventOfType(t, events, EventAfterTool)
	if beforeTool.RequestID == "" || beforeTool.RequestID != afterTool.RequestID {
		t.Fatalf("tool request IDs = %q/%q, want matching non-empty IDs", beforeTool.RequestID, afterTool.RequestID)
	}
	if beforeTool.Round != 1 || afterTool.Round != 1 || afterTool.Duration <= 0 {
		t.Fatalf("tool audit fields = before round %d after round %d duration %s, want first round and duration", beforeTool.Round, afterTool.Round, afterTool.Duration)
	}
	if beforeTool.EstimatedTokens <= 0 || afterTool.EstimatedTokens <= 0 {
		t.Fatalf("tool estimated tokens = %d/%d, want positive estimates", beforeTool.EstimatedTokens, afterTool.EstimatedTokens)
	}
	if beforeTool.ToolName != "echo" || afterTool.ToolName != "echo" {
		t.Fatalf("tool event names = %q/%q, want echo", beforeTool.ToolName, afterTool.ToolName)
	}
}

func TestAgentAfterModelObservabilityCarriesRealTokenUsage(t *testing.T) {
	ctx := context.Background()
	wantUsage := TokenUsage{InputTokens: 21, OutputTokens: 8, TotalTokens: 29}
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "ok"}, Usage: wantUsage},
	}}
	var events []Event
	bot, err := New(Config{ID: "usage-agent", SystemPrompt: "base"}, model,
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "hello"); err != nil {
		t.Fatal(err)
	}

	beforeModel := firstEventOfType(t, events, EventBeforeModel)
	afterModel := firstEventOfType(t, events, EventAfterModel)
	if beforeModel.TokenUsage != (TokenUsage{}) {
		t.Fatalf("before model token usage = %#v, want zero usage", beforeModel.TokenUsage)
	}
	if afterModel.TokenUsage != wantUsage {
		t.Fatalf("after model token usage = %#v, want %#v", afterModel.TokenUsage, wantUsage)
	}
	if beforeModel.EstimatedTokens <= 0 || afterModel.EstimatedTokens != beforeModel.EstimatedTokens {
		t.Fatalf("model estimated tokens = %d/%d, want positive matching estimate", beforeModel.EstimatedTokens, afterModel.EstimatedTokens)
	}

	afterObservation := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	if afterObservation.TokenUsage != wantUsage {
		t.Fatalf("after model observation token usage = %#v, want %#v", afterObservation.TokenUsage, wantUsage)
	}
	if afterObservation.EstimatedTokens != afterModel.EstimatedTokens {
		t.Fatalf("after model observation estimated tokens = %d, want %d", afterObservation.EstimatedTokens, afterModel.EstimatedTokens)
	}
}

func TestAgentAfterModelObservabilityLeavesUsageZeroWhenUnavailable(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "ok"}},
	}}
	var events []Event
	bot, err := New(Config{ID: "usage-agent", SystemPrompt: "base"}, model,
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "hello"); err != nil {
		t.Fatal(err)
	}

	afterModel := firstEventOfType(t, events, EventAfterModel)
	if afterModel.TokenUsage != (TokenUsage{}) {
		t.Fatalf("after model token usage = %#v, want zero usage", afterModel.TokenUsage)
	}
	if afterModel.EstimatedTokens <= 0 {
		t.Fatalf("after model estimated tokens = %d, want positive estimate", afterModel.EstimatedTokens)
	}

	afterObservation := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	if afterObservation.TokenUsage != (TokenUsage{}) {
		t.Fatalf("after model observation token usage = %#v, want zero usage", afterObservation.TokenUsage)
	}
	if afterObservation.EstimatedTokens <= 0 {
		t.Fatalf("after model observation estimated tokens = %d, want positive estimate", afterObservation.EstimatedTokens)
	}
}

func TestAgentLifecycleEventsCarryParentRequestIDs(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	var events []Event
	bot, err := New(Config{ID: "parent-agent", SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: call.Arguments["text"].(string)}, nil
			},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "use echo"); err != nil {
		t.Fatal(err)
	}

	firstBeforeModel := firstEventOfTypeAndRound(t, events, EventBeforeModel, 1)
	firstAfterModel := firstEventOfTypeAndRound(t, events, EventAfterModel, 1)
	if firstBeforeModel.ParentRequestID != "" || firstAfterModel.ParentRequestID != "" {
		t.Fatalf("first model parent request IDs = %q/%q, want empty roots", firstBeforeModel.ParentRequestID, firstAfterModel.ParentRequestID)
	}
	if firstBeforeModel.RequestID == "" || firstBeforeModel.RequestID != firstAfterModel.RequestID {
		t.Fatalf("first model request IDs = %q/%q, want matching non-empty IDs", firstBeforeModel.RequestID, firstAfterModel.RequestID)
	}

	beforeApproval := firstEventOfTypeAndRound(t, events, EventBeforeApproval, 1)
	afterApproval := firstEventOfTypeAndRound(t, events, EventAfterApproval, 1)
	beforeTool := firstEventOfTypeAndRound(t, events, EventBeforeTool, 1)
	afterTool := firstEventOfTypeAndRound(t, events, EventAfterTool, 1)
	for _, event := range []Event{beforeApproval, afterApproval, beforeTool, afterTool} {
		if event.ParentRequestID != firstBeforeModel.RequestID {
			t.Fatalf("%s parent request ID = %q, want first model request ID %q", event.Type, event.ParentRequestID, firstBeforeModel.RequestID)
		}
	}

	secondBeforeModel := firstEventOfTypeAndRound(t, events, EventBeforeModel, 2)
	secondAfterModel := firstEventOfTypeAndRound(t, events, EventAfterModel, 2)
	if secondBeforeModel.RequestID == "" || secondBeforeModel.RequestID != secondAfterModel.RequestID {
		t.Fatalf("second model request IDs = %q/%q, want matching non-empty IDs", secondBeforeModel.RequestID, secondAfterModel.RequestID)
	}
	if secondBeforeModel.ParentRequestID != firstBeforeModel.RequestID || secondAfterModel.ParentRequestID != firstBeforeModel.RequestID {
		t.Fatalf("second model parent request IDs = %q/%q, want first model request ID %q", secondBeforeModel.ParentRequestID, secondAfterModel.ParentRequestID, firstBeforeModel.RequestID)
	}
}

func TestAgentRunEventsShareCustomRunID(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	childModel := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "child done"}},
	}}
	compactor := &recordingCompactor{
		result: []Message{{Role: RoleSystem, Content: "summary"}},
	}
	var events []Event
	var bot *Agent
	var spawned bool
	var err error
	bot, err = New(Config{
		ID:           "run-agent",
		SystemPrompt: "base",
		Compact: CompactConfig{
			MaxTokens: 2,
			Threshold: 1,
		},
	}, model,
		WithSkills(Skill{Name: "planner", Instructions: "Plan carefully."}),
		WithCompactor(compactor),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 1 })),
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				if !spawned {
					spawned = true
					if _, err := bot.SpawnSubagent(ctx, SubagentOptions{
						ID:    "worker-1",
						Model: childModel,
					}); err != nil {
						return ToolResult{}, err
					}
					if _, err := bot.SendMessageToSubagent(ctx, "worker-1", "start"); err != nil {
						return ToolResult{}, err
					}
				}
				return ToolResult{CallID: call.ID, Name: call.Name, Content: call.Arguments["text"].(string)}, nil
			},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	bot.AppendMessage(Message{Role: RoleUser, Content: "before"})

	if _, err := bot.Run(ctx, "use echo", WithRunID("custom-run"), WithRunSkills("planner")); err != nil {
		t.Fatal(err)
	}

	required := []EventType{
		EventSkillActivated,
		EventBeforeCompact,
		EventAfterCompact,
		EventBeforeModel,
		EventAfterModel,
		EventBeforeApproval,
		EventAfterApproval,
		EventBeforeTool,
		EventAfterTool,
		EventSubagentMessage,
	}
	for _, eventType := range required {
		if event := firstEventOfType(t, events, eventType); event.RunID != "custom-run" {
			t.Fatalf("%s run ID = %q, want custom-run", eventType, event.RunID)
		}
	}
	for _, event := range events {
		if event.RunID != "custom-run" {
			t.Fatalf("event = %#v, want every event to share custom run ID", event)
		}
	}
}

func TestAgentGeneratesStableRunIDsPerRun(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "first"}},
		{Message: Message{Role: RoleAssistant, Content: "second"}},
	}}
	var events []Event
	bot, err := New(Config{ID: "generated-agent", SystemPrompt: "base"}, model,
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Run(ctx, "second"); err != nil {
		t.Fatal(err)
	}

	var runIDs []string
	for _, event := range events {
		if event.Type == EventBeforeModel {
			runIDs = append(runIDs, event.RunID)
		}
	}
	want := []string{"generated-agent-run-1", "generated-agent-run-2"}
	if !reflect.DeepEqual(runIDs, want) {
		t.Fatalf("generated run IDs = %#v, want %#v", runIDs, want)
	}
}

func TestAgentTraceContextPropagatesToRunEventsObservationsAndErrors(t *testing.T) {
	type unrelatedContextKey struct{}
	trace := TraceContext{
		TraceID:    "trace-run-123",
		SpanID:     "span-run-456",
		TraceState: "vendor=state",
	}
	ctx := context.WithValue(context.Background(), unrelatedContextKey{}, "context-secret")
	ctx = WithTraceContext(ctx, trace)

	toolErr := errors.New("tool failed")
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "fail", Arguments: map[string]any{"path": "/"}}}},
	}}
	compactor := &recordingCompactor{
		result: []Message{{Role: RoleSystem, Content: "summary"}},
	}
	recorder := &recordingObserver{}
	var events []Event
	bot, err := New(Config{
		ID:           "trace-agent",
		SystemPrompt: "base",
		Compact: CompactConfig{
			MaxTokens: 2,
			Threshold: 1,
		},
	}, model,
		WithSkills(Skill{Name: "planner", Instructions: "Plan carefully."}),
		WithCompactor(compactor),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 1 })),
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName:        "fail",
			ToolDescription: "Fails with a stable error",
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				return ToolResult{}, toolErr
			},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	bot.AppendMessage(Message{Role: RoleUser, Content: "before"})

	_, err = bot.Run(ctx, "use fail", WithRunSkills("planner"))
	if !errors.Is(err, toolErr) {
		t.Fatalf("err = %v, want tool error", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	assertAgentErrorTraceContext(t, agentErr, trace)
	if agentErr.RunID == "" || agentErr.RunID == trace.TraceID {
		t.Fatalf("agent error run ID = %q, want generated ID distinct from trace ID %q", agentErr.RunID, trace.TraceID)
	}

	required := []EventType{
		EventSkillActivated,
		EventBeforeCompact,
		EventAfterCompact,
		EventBeforeModel,
		EventAfterModel,
		EventBeforeApproval,
		EventAfterApproval,
		EventBeforeTool,
		EventAfterTool,
	}
	for _, eventType := range required {
		event := firstEventOfType(t, events, eventType)
		assertEventTraceContext(t, event, trace)
		if event.RunID == "" || event.RunID == trace.TraceID {
			t.Fatalf("%s run ID = %q, want generated ID distinct from trace ID %q", eventType, event.RunID, trace.TraceID)
		}
	}

	observations := recorder.Observations()
	for _, eventType := range required {
		observation := firstObservationOfType(t, observations, eventType)
		assertObservationTraceContext(t, observation, trace)
		if observation.RunID == "" || observation.RunID == trace.TraceID {
			t.Fatalf("%s observation run ID = %q, want generated ID distinct from trace ID %q", eventType, observation.RunID, trace.TraceID)
		}
		assertObservationDoesNotContain(t, observation, "context-secret")
	}

	child, err := bot.SpawnSubagent(ctx, SubagentOptions{
		ID:    "worker-1",
		Model: &recordingModel{responses: []ModelResponse{{Message: Message{Role: RoleAssistant, Content: "child"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.ID() != "worker-1" {
		t.Fatalf("child ID = %q, want worker-1", child.ID())
	}
	subagentEvent := events[len(events)-1]
	if subagentEvent.Type != EventSubagentMessage {
		t.Fatalf("last event = %#v, want subagent message", subagentEvent)
	}
	assertEventTraceContext(t, subagentEvent, trace)
	assertObservationTraceContext(t, recorder.Observations()[len(recorder.Observations())-1], trace)
}

func TestAgentToolPreflightFailuresEmitAfterToolAuditEvent(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		toolCall ToolCall
		options  []Option
		wantErr  error
		wantCat  ErrorCategory
		wantOp   string
	}{
		{
			name:     "tool not found",
			toolCall: ToolCall{ID: "call-1", Name: "missing", Arguments: map[string]any{"text": "hello"}},
			wantErr:  ErrToolNotFound,
			wantCat:  ErrorCategoryTool,
			wantOp:   "tool.lookup",
		},
		{
			name:     "schema validation",
			toolCall: ToolCall{ID: "call-1", Name: "echo", Arguments: map[string]any{}},
			options: []Option{WithTools(ToolFunc{
				ToolName:        "echo",
				ToolDescription: "Echo text",
				Parameters: &ToolParametersSchema{
					Type:     SchemaTypeObject,
					Required: []string{"text"},
					Properties: map[string]ToolParametersSchema{
						"text": {Type: SchemaTypeString},
					},
				},
				Fn: func(context.Context, ToolCall) (ToolResult, error) {
					t.Fatal("tool should not execute when schema validation fails")
					return ToolResult{}, nil
				},
			})},
			wantErr: ErrToolValidation,
			wantCat: ErrorCategorySchema,
			wantOp:  "tool.validate",
		},
		{
			name:     "approval denied",
			toolCall: ToolCall{ID: "call-1", Name: "danger", Arguments: map[string]any{"path": "/"}},
			options: []Option{
				WithTools(ToolFunc{
					ToolName:        "danger",
					ToolDescription: "Dangerous operation",
					Fn: func(context.Context, ToolCall) (ToolResult, error) {
						t.Fatal("tool should not execute when approval is denied")
						return ToolResult{}, nil
					},
				}),
				WithApprovalPolicy(ApprovalFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
					return ApprovalDecision{Approved: false, Reason: "blocked"}, nil
				})),
			},
			wantErr: ErrApprovalDenied,
			wantCat: ErrorCategoryApproval,
			wantOp:  "tool.approval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &recordingModel{responses: []ModelResponse{{ToolCalls: []ToolCall{tt.toolCall}}}}
			var events []Event
			options := append([]Option{}, tt.options...)
			options = append(options, WithHook(func(ctx context.Context, event Event) error {
				events = append(events, event)
				return nil
			}))
			bot, err := New(Config{ID: "tool-preflight-agent"}, model, options...)
			if err != nil {
				t.Fatal(err)
			}

			_, err = bot.Run(ctx, "use tool")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want sentinel %v", err, tt.wantErr)
			}
			var agentErr *AgentError
			if !errors.As(err, &agentErr) {
				t.Fatalf("err = %T, want *AgentError", err)
			}
			if agentErr.Category != tt.wantCat || agentErr.Operation != tt.wantOp {
				t.Fatalf("agent error category/operation = %q/%q, want %q/%q", agentErr.Category, agentErr.Operation, tt.wantCat, tt.wantOp)
			}
			if agentErr.RequestID == "" || agentErr.Round != 1 || agentErr.ToolName != tt.toolCall.Name {
				t.Fatalf("agent error context = %#v, want request ID, round, and tool name", agentErr)
			}

			afterTool := firstEventOfType(t, events, EventAfterTool)
			if afterTool.RequestID == "" || afterTool.RequestID != agentErr.RequestID {
				t.Fatalf("after tool request ID = %q, want agent error request ID %q", afterTool.RequestID, agentErr.RequestID)
			}
			if afterTool.Round != 1 || afterTool.ToolName != tt.toolCall.Name || afterTool.Duration <= 0 {
				t.Fatalf("after tool audit fields = %#v, want round, tool name, and duration", afterTool)
			}
			if afterTool.EstimatedTokens <= 0 || afterTool.ErrorCategory != tt.wantCat || !errors.Is(afterTool.Error, tt.wantErr) {
				t.Fatalf("after tool error fields = tokens %d category %q error %v, want estimate/category/sentinel", afterTool.EstimatedTokens, afterTool.ErrorCategory, afterTool.Error)
			}
		})
	}
}

func TestAgentModelErrorIsStructuredAndEmittedWithCategory(t *testing.T) {
	ctx := context.Background()
	modelErr := errors.New("provider unavailable")
	var events []Event
	bot, err := New(Config{ID: "model-agent"}, failingModel{err: modelErr},
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "hello", WithRunID("model-run"))
	if !errors.Is(err, modelErr) {
		t.Fatalf("err = %v, want wrapped model error", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryModel || agentErr.Operation != "model.generate" {
		t.Fatalf("model error category/operation = %q/%q, want model/model.generate", agentErr.Category, agentErr.Operation)
	}
	if agentErr.RunID != "model-run" {
		t.Fatalf("model error run ID = %q, want model-run", agentErr.RunID)
	}

	afterModel := firstEventOfType(t, events, EventAfterModel)
	if afterModel.RunID != "model-run" {
		t.Fatalf("after model run ID = %q, want model-run", afterModel.RunID)
	}
	if afterModel.RequestID == "" || afterModel.Round != 1 || afterModel.Duration <= 0 {
		t.Fatalf("after model audit fields = %#v, want request ID, round, and duration", afterModel)
	}
	if afterModel.EstimatedTokens <= 0 || afterModel.ErrorCategory != ErrorCategoryModel {
		t.Fatalf("after model tokens/category = %d/%q, want positive tokens and model category", afterModel.EstimatedTokens, afterModel.ErrorCategory)
	}
}

func TestAgentReturnsHookErrorWhenFailureAuditHookRejectsEvent(t *testing.T) {
	ctx := context.Background()
	hookErr := errors.New("audit sink unavailable")
	var captured []Event
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "missing", Arguments: map[string]any{"text": "hello"}}}},
	}}
	bot, err := New(Config{ID: "hook-priority-agent"}, model,
		WithHook(func(ctx context.Context, event Event) error {
			captured = append(captured, event)
			if event.Type == EventAfterTool && event.Error != nil {
				return hookErr
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "use missing tool")
	if !errors.Is(err, hookErr) {
		t.Fatalf("err = %v, want hook error", err)
	}
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want original tool sentinel to remain observable", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryHook || agentErr.Operation != "hook.after_tool" {
		t.Fatalf("agent error category/operation = %q/%q, want hook/hook.after_tool", agentErr.Category, agentErr.Operation)
	}

	afterTool := firstEventOfType(t, captured, EventAfterTool)
	if afterTool.ErrorCategory != ErrorCategoryTool || !errors.Is(afterTool.Error, ErrToolNotFound) {
		t.Fatalf("captured event error = category %q error %v, want original tool error", afterTool.ErrorCategory, afterTool.Error)
	}
	if afterTool.RequestID == "" || agentErr.RequestID != afterTool.RequestID {
		t.Fatalf("request IDs = event %q agent error %q, want matching non-empty IDs", afterTool.RequestID, agentErr.RequestID)
	}
}

func TestAgentReturnsHookErrorWhenModelFailureAuditHookRejectsEvent(t *testing.T) {
	ctx := context.Background()
	modelErr := errors.New("provider unavailable")
	hookErr := errors.New("audit sink unavailable")
	bot, err := New(Config{ID: "model-hook-priority-agent"}, failingModel{err: modelErr},
		WithHook(func(ctx context.Context, event Event) error {
			if event.Type == EventAfterModel && event.Error != nil {
				return hookErr
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "hello")
	if !errors.Is(err, hookErr) {
		t.Fatalf("err = %v, want hook error", err)
	}
	if !errors.Is(err, modelErr) {
		t.Fatalf("err = %v, want original model error to remain observable", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryHook || agentErr.Operation != "hook.after_model" {
		t.Fatalf("agent error category/operation = %q/%q, want hook/hook.after_model", agentErr.Category, agentErr.Operation)
	}
	if agentErr.RequestID == "" || agentErr.Round != 1 {
		t.Fatalf("agent error context = %#v, want request ID and round", agentErr)
	}
}

func TestAgentCompactEventsCarryDurationAndTokenAuditFields(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "after compact"}},
	}}
	compactor := &recordingCompactor{
		result: []Message{{Role: RoleSystem, Content: "summary"}},
	}
	var events []Event
	bot, err := New(Config{
		ID:           "compact-agent",
		SystemPrompt: "base",
		Compact: CompactConfig{
			MaxTokens: 2,
			Threshold: 1,
		},
	}, model,
		WithCompactor(compactor),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 1 })),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	bot.AppendMessage(Message{Role: RoleUser, Content: "before"})

	if _, err := bot.Run(ctx, "trigger compact"); err != nil {
		t.Fatal(err)
	}

	beforeCompact := firstEventOfType(t, events, EventBeforeCompact)
	afterCompact := firstEventOfType(t, events, EventAfterCompact)
	if beforeCompact.RequestID == "" || beforeCompact.RequestID != afterCompact.RequestID {
		t.Fatalf("compact request IDs = %q/%q, want matching non-empty IDs", beforeCompact.RequestID, afterCompact.RequestID)
	}
	if beforeCompact.ParentRequestID != "" || afterCompact.ParentRequestID != "" {
		t.Fatalf("initial compact parent request IDs = %q/%q, want empty roots", beforeCompact.ParentRequestID, afterCompact.ParentRequestID)
	}
	if beforeCompact.EstimatedTokens != 2 || afterCompact.EstimatedTokens != 1 {
		t.Fatalf("compact estimated tokens = %d/%d, want before and after estimates", beforeCompact.EstimatedTokens, afterCompact.EstimatedTokens)
	}
	if afterCompact.Duration <= 0 || afterCompact.ErrorCategory != "" {
		t.Fatalf("after compact duration/category = %s/%q, want positive duration and no category", afterCompact.Duration, afterCompact.ErrorCategory)
	}
}

func TestAgentCompactionAfterToolCycleCarriesParentRequestID(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	compactor := &recordingCompactor{
		result: []Message{{Role: RoleSystem, Content: "summary"}},
	}
	var events []Event
	bot, err := New(Config{
		ID:           "compact-parent-agent",
		SystemPrompt: "base",
		Compact: CompactConfig{
			MaxTokens: 3,
			Threshold: 1,
		},
	}, model,
		WithCompactor(compactor),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 1 })),
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: call.Arguments["text"].(string)}, nil
			},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "use echo"); err != nil {
		t.Fatal(err)
	}

	firstModel := firstEventOfTypeAndRound(t, events, EventBeforeModel, 1)
	beforeCompact := firstEventOfTypeAndRound(t, events, EventBeforeCompact, 1)
	afterCompact := firstEventOfTypeAndRound(t, events, EventAfterCompact, 1)
	if beforeCompact.ParentRequestID != firstModel.RequestID || afterCompact.ParentRequestID != firstModel.RequestID {
		t.Fatalf("post-tool compact parent request IDs = %q/%q, want first model request ID %q", beforeCompact.ParentRequestID, afterCompact.ParentRequestID, firstModel.RequestID)
	}
	secondModel := firstEventOfTypeAndRound(t, events, EventBeforeModel, 2)
	if secondModel.ParentRequestID != firstModel.RequestID {
		t.Fatalf("follow-up model parent request ID = %q, want first model request ID %q", secondModel.ParentRequestID, firstModel.RequestID)
	}
}

func TestAgentWrapsHookErrorWithoutRecursiveHookEvent(t *testing.T) {
	ctx := context.Background()
	hookErr := errors.New("hook rejected")
	var calls int
	bot, err := New(Config{ID: "hook-agent"}, &recordingModel{},
		WithHook(func(ctx context.Context, event Event) error {
			calls++
			return hookErr
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "hello")
	if !errors.Is(err, hookErr) {
		t.Fatalf("err = %v, want wrapped hook error", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryHook || agentErr.Operation != "hook.before_model" {
		t.Fatalf("hook error category/operation = %q/%q, want hook/hook.before_model", agentErr.Category, agentErr.Operation)
	}
	if calls != 1 {
		t.Fatalf("hook calls = %d, want no recursive hook event", calls)
	}
}

func TestAgentLoadsInstructionFilesAndTriggersSkills(t *testing.T) {
	ctx := context.Background()
	instructionFile := filepath.Join(t.TempDir(), "AGENT.md")
	if err := os.WriteFile(instructionFile, []byte("Prefer concise, code-first answers."), 0o600); err != nil {
		t.Fatal(err)
	}

	skillRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(skillRoot, "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	skillBody := `---
name: review
description: Review code changes
triggers:
  - review
---
Inspect patches for correctness, regressions, and missing tests.
`
	if err := os.WriteFile(filepath.Join(skillRoot, "review", "SKILL.md"), []byte(skillBody), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSkills(skillRoot)
	if err != nil {
		t.Fatal(err)
	}

	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "reviewed"}},
	}}
	agent, err := New(Config{SystemPrompt: "You are a senior engineering agent."}, model,
		WithInstructionFiles(instructionFile),
		WithSkills(loaded...),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := agent.Run(ctx, "please review this patch")
	if err != nil {
		t.Fatal(err)
	}

	if response.Content != "reviewed" {
		t.Fatalf("response content = %q, want reviewed", response.Content)
	}
	if len(model.requests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(model.requests))
	}
	request := model.requests[0]
	if !strings.Contains(request.SystemPrompt, "You are a senior engineering agent.") {
		t.Fatalf("system prompt did not include base prompt: %q", request.SystemPrompt)
	}
	if !strings.Contains(request.SystemPrompt, "Prefer concise, code-first answers.") {
		t.Fatalf("system prompt did not include instruction file: %q", request.SystemPrompt)
	}
	if len(request.ActiveSkills) != 1 || request.ActiveSkills[0].Name != "review" {
		t.Fatalf("active skills = %#v, want review", request.ActiveSkills)
	}
	if !strings.Contains(request.SystemPrompt, "Inspect patches for correctness") {
		t.Fatalf("system prompt did not include active skill instructions: %q", request.SystemPrompt)
	}
}

func TestAgentCanTriggerSkillsExplicitlyForSingleRun(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "planned"}},
	}}
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithSkills(Skill{Name: "planner", Description: "Plan work", Instructions: "Break work into testable steps."}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.Run(ctx, "build the feature", WithRunSkills("planner")); err != nil {
		t.Fatal(err)
	}

	request := model.requests[0]
	if len(request.ActiveSkills) != 1 || request.ActiveSkills[0].Name != "planner" {
		t.Fatalf("active skills = %#v, want planner", request.ActiveSkills)
	}
	for _, skill := range agent.ActiveSkills() {
		if skill.Name == "planner" {
			t.Fatal("run-scoped skill leaked into the agent's persistent active skills")
		}
	}
}

func TestAgentEmitsHookWhenSkillActivates(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "reviewed"}},
	}}
	var events []Event
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithSkills(Skill{
			Name:           "review",
			Description:    "Review code",
			Instructions:   "Find bugs.",
			TriggerPhrases: []string{"review"},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.Run(ctx, "review this change"); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, event := range events {
		if event.Type == EventSkillActivated && event.SkillName == "review" {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %#v, want skill activation event for review", events)
	}
}

func TestAgentAutomaticallyCarriesConversationContextBetweenRuns(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "first answer"}},
		{Message: Message{Role: RoleAssistant, Content: "second answer"}},
	}}
	agent, err := New(Config{SystemPrompt: "base"}, model)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.Run(ctx, "first question"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(ctx, "second question"); err != nil {
		t.Fatal(err)
	}

	got := model.requests[1].Messages
	want := []Message{
		{Role: RoleUser, Content: "first question"},
		{Role: RoleAssistant, Content: "first answer"},
		{Role: RoleUser, Content: "second question"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("second request context = %#v, want %#v", got, want)
	}
}

func TestAgentResetClearsCurrentContext(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "fresh answer"}},
	}}
	agent, err := New(Config{SystemPrompt: "base"}, model)
	if err != nil {
		t.Fatal(err)
	}
	agent.AppendMessage(Message{Role: RoleUser, Content: "old question"})
	agent.AppendMessage(Message{Role: RoleAssistant, Content: "old answer"})

	agent.Reset()

	if got := agent.Messages(); len(got) != 0 {
		t.Fatalf("messages after reset = %#v, want empty context", got)
	}
	if _, err := agent.Run(ctx, "fresh question"); err != nil {
		t.Fatal(err)
	}
	want := []Message{{Role: RoleUser, Content: "fresh question"}}
	if got := model.requests[0].Messages; !reflect.DeepEqual(got, want) {
		t.Fatalf("request messages after reset = %#v, want %#v", got, want)
	}
}

func TestAgentSnapshotRestoreDeepCopiesContextAndContinuesRun(t *testing.T) {
	ctx := context.Background()
	source, err := New(Config{ID: "source"}, &recordingModel{})
	if err != nil {
		t.Fatal(err)
	}
	source.AppendMessage(complexMessageForSessionTests())

	isolationSnapshot := source.Snapshot()
	if isolationSnapshot.AgentID != "source" {
		t.Fatalf("snapshot agent ID = %q, want source", isolationSnapshot.AgentID)
	}
	if isolationSnapshot.CreatedAt.IsZero() {
		t.Fatal("snapshot creation time was not recorded")
	}
	isolationSnapshot.messages[0].Content = "changed"
	mutateSessionMessage(isolationSnapshot.messages[0])
	assertComplexSessionMessageUnchanged(t, source.Messages()[0])

	snapshot := source.Snapshot()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SessionSnapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Messages(), snapshot.Messages()) {
		t.Fatalf("decoded snapshot messages = %#v, want %#v", decoded.Messages(), snapshot.Messages())
	}

	targetModel := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "continued"}},
	}}
	target, err := New(Config{ID: "target"}, targetModel)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Restore(decoded); err != nil {
		t.Fatal(err)
	}
	decoded.messages[0].Content = "changed"
	mutateSessionMessage(decoded.messages[0])
	mutateSessionMessage(target.Messages()[0])
	assertComplexSessionMessageUnchanged(t, target.Messages()[0])

	if _, err := target.Run(ctx, "continue"); err != nil {
		t.Fatal(err)
	}
	got := targetModel.requests[0].Messages
	if len(got) != 2 {
		t.Fatalf("restored request messages = %#v, want restored message plus user input", got)
	}
	assertComplexSessionMessageUnchanged(t, got[0])
	if want := (Message{Role: RoleUser, Content: "continue"}); !reflect.DeepEqual(got[1], want) {
		t.Fatalf("continued request tail = %#v, want user continue", got[1])
	}
}

func TestAgentForkCopiesConfigurationAndIsolatesConversation(t *testing.T) {
	ctx := context.Background()
	instructionFile := filepath.Join(t.TempDir(), "AGENT.md")
	if err := os.WriteFile(instructionFile, []byte("Prefer short answers."), 0o600); err != nil {
		t.Fatal(err)
	}

	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "fork answer"}},
		{Message: Message{Role: RoleAssistant, Content: "parent answer"}},
	}}
	compactor := &recordingCompactor{}
	counter := &recordingTokenCounter{}
	var events []Event
	parent, err := New(Config{ID: "parent", SystemPrompt: "base prompt"}, model,
		WithInstructionFiles(instructionFile),
		WithSkills(Skill{Name: "review", Description: "Review", Instructions: "Check for regressions."}),
		WithMCPServers(MCPServerConfig{Name: "fs", Command: "mcp-fs", Args: []string{"--root", "."}, Env: map[string]string{"MODE": "test"}, Transport: MCPTransportStdio}),
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				return ToolResult{Content: "ok"}, nil
			},
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
		WithCompactor(compactor),
		WithTokenCounter(counter),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.ActivateSkill("review"); err != nil {
		t.Fatal(err)
	}
	parent.AppendMessage(Message{Role: RoleUser, Content: "before fork"})

	fork, err := parent.Fork("forked")
	if err != nil {
		t.Fatal(err)
	}
	if fork.ID() != "forked" {
		t.Fatalf("fork ID = %q, want forked", fork.ID())
	}
	if fork.parent != nil {
		t.Fatal("fork unexpectedly has a parent agent")
	}
	if len(fork.subagents) != 0 || len(fork.parentInbox) != 0 {
		t.Fatalf("fork subagent state = %#v/%#v, want empty independent state", fork.subagents, fork.parentInbox)
	}
	if err := fork.SendToParent(ctx, "progress"); err == nil {
		t.Fatal("fork SendToParent returned nil error, want no parent relationship")
	}
	if !fork.HasTool("echo") || !fork.HasSkill("review") {
		t.Fatal("fork did not copy tools and skills")
	}
	if len(fork.ActiveSkills()) != 1 || fork.ActiveSkills()[0].Name != "review" {
		t.Fatalf("fork active skills = %#v, want review", fork.ActiveSkills())
	}
	if !reflect.DeepEqual(fork.MCPServers(), parent.MCPServers()) {
		t.Fatalf("fork MCP servers = %#v, want %#v", fork.MCPServers(), parent.MCPServers())
	}
	if fork.compactor != compactor {
		t.Fatal("fork did not copy compactor")
	}
	if fork.tokenCount != counter {
		t.Fatal("fork did not copy token counter")
	}

	parent.AppendMessage(Message{Role: RoleAssistant, Content: "parent only"})
	fork.AppendMessage(Message{Role: RoleAssistant, Content: "fork only"})

	if containsMessageContent(parent.Messages(), "fork only") {
		t.Fatalf("parent messages = %#v, want no fork-only message", parent.Messages())
	}
	if containsMessageContent(fork.Messages(), "parent only") {
		t.Fatalf("fork messages = %#v, want no parent-only message", fork.Messages())
	}

	if _, err := fork.Run(ctx, "fork turn"); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.Run(ctx, "parent turn"); err != nil {
		t.Fatal(err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(model.requests))
	}

	forkRequest := model.requests[0]
	if forkRequest.AgentID != "forked" {
		t.Fatalf("fork request agent ID = %q, want forked", forkRequest.AgentID)
	}
	if !strings.Contains(forkRequest.SystemPrompt, "base prompt") ||
		!strings.Contains(forkRequest.SystemPrompt, "Prefer short answers.") ||
		!strings.Contains(forkRequest.SystemPrompt, "Check for regressions.") {
		t.Fatalf("fork system prompt = %q, want copied prompt, instructions, and active skill", forkRequest.SystemPrompt)
	}
	if len(forkRequest.Tools) != 1 || forkRequest.Tools[0].Name != "echo" {
		t.Fatalf("fork request tools = %#v, want echo", forkRequest.Tools)
	}
	if len(forkRequest.MCPServers) != 1 || forkRequest.MCPServers[0].Name != "fs" {
		t.Fatalf("fork request MCP servers = %#v, want fs", forkRequest.MCPServers)
	}
	if len(forkRequest.ActiveSkills) != 1 || forkRequest.ActiveSkills[0].Name != "review" {
		t.Fatalf("fork request active skills = %#v, want review", forkRequest.ActiveSkills)
	}
	if containsMessageContent(forkRequest.Messages, "parent only") {
		t.Fatalf("fork request messages = %#v, want no parent-only message", forkRequest.Messages)
	}

	parentRequest := model.requests[1]
	if parentRequest.AgentID != "parent" {
		t.Fatalf("parent request agent ID = %q, want parent", parentRequest.AgentID)
	}
	if containsMessageContent(parentRequest.Messages, "fork only") {
		t.Fatalf("parent request messages = %#v, want no fork-only message", parentRequest.Messages)
	}
	if !hasEventForAgent(events, "forked", EventBeforeModel) || !hasEventForAgent(events, "parent", EventBeforeModel) {
		t.Fatalf("events = %#v, want hooks copied for fork and retained for parent", events)
	}
}

func TestAgentRunStreamEmitsDeltasDoneAndWritesFinalMessage(t *testing.T) {
	ctx := context.Background()
	model := &streamingRecordingModel{streamEvents: []StreamEvent{
		{Type: StreamEventDelta, Delta: "hel"},
		{Type: StreamEventDelta, Delta: "lo"},
		{Type: StreamEventDone},
	}}
	agent, err := New(Config{ID: "stream-agent", SystemPrompt: "base"}, model)
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "say hello")
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)

	if len(got) != 3 {
		t.Fatalf("stream events = %#v, want delta, delta, done", got)
	}
	if got[0].Type != StreamEventDelta || got[0].AgentID != "stream-agent" || got[0].Delta != "hel" {
		t.Fatalf("first event = %#v, want hel delta for stream-agent", got[0])
	}
	if got[1].Type != StreamEventDelta || got[1].AgentID != "stream-agent" || got[1].Delta != "lo" {
		t.Fatalf("second event = %#v, want lo delta for stream-agent", got[1])
	}
	if got[2].Type != StreamEventDone || got[2].AgentID != "stream-agent" {
		t.Fatalf("done event = %#v, want done for stream-agent", got[2])
	}
	if got[2].Message.Role != RoleAssistant || got[2].Message.Content != "hello" {
		t.Fatalf("done message = %#v, want aggregated assistant hello", got[2].Message)
	}

	if len(model.requests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(model.requests))
	}
	if got := model.requests[0].Messages; !reflect.DeepEqual(got, []Message{{Role: RoleUser, Content: "say hello"}}) {
		t.Fatalf("stream request messages = %#v, want user input", got)
	}
	wantMessages := []Message{
		{Role: RoleUser, Content: "say hello"},
		{Role: RoleAssistant, Content: "hello"},
	}
	if got := agent.Messages(); !reflect.DeepEqual(got, wantMessages) {
		t.Fatalf("agent messages = %#v, want final assistant written to context", got)
	}
}

func TestAgentRunStreamReturnsClearErrorWhenModelDoesNotSupportStreaming(t *testing.T) {
	ctx := context.Background()
	agent, err := New(Config{SystemPrompt: "base"}, &recordingModel{})
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "hello")
	if !errors.Is(err, ErrStreamingUnsupported) {
		t.Fatalf("RunStream error = %v, want ErrStreamingUnsupported", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("RunStream error = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryStreaming || agentErr.Operation != "stream.unsupported" {
		t.Fatalf("streaming unsupported category/operation = %q/%q, want streaming/stream.unsupported", agentErr.Category, agentErr.Operation)
	}
	if agentErr.RequestID == "" || agentErr.Round != 1 {
		t.Fatalf("streaming unsupported context = %#v, want request ID and round", agentErr)
	}
	if events != nil {
		t.Fatalf("RunStream events = %#v, want nil channel on immediate unsupported error", events)
	}
	if got := agent.Messages(); len(got) != 0 {
		t.Fatalf("agent messages = %#v, want unsupported streaming to leave context unchanged", got)
	}
}

func TestAgentRunStreamEmitsErrorEventAndSkipsIncompleteAssistantMessage(t *testing.T) {
	ctx := context.Background()
	streamErr := errors.New("stream interrupted")
	model := &streamingRecordingModel{streamEvents: []StreamEvent{
		{Type: StreamEventDelta, Delta: "partial"},
		{Type: StreamEventError, Error: streamErr},
	}}
	agent, err := New(Config{ID: "stream-agent"}, model)
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "start")
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)

	if len(got) != 2 {
		t.Fatalf("stream events = %#v, want delta then error", got)
	}
	if got[0].Type != StreamEventDelta || got[0].Delta != "partial" || got[0].AgentID != "stream-agent" {
		t.Fatalf("delta event = %#v, want partial delta for stream-agent", got[0])
	}
	if got[1].Type != StreamEventError || got[1].AgentID != "stream-agent" || !errors.Is(got[1].Error, streamErr) {
		t.Fatalf("error event = %#v, want stream interrupted error for stream-agent", got[1])
	}

	wantMessages := []Message{{Role: RoleUser, Content: "start"}}
	if got := agent.Messages(); !reflect.DeepEqual(got, wantMessages) {
		t.Fatalf("agent messages = %#v, want only user message after interrupted stream", got)
	}
}

func TestAgentRunStreamCarriesRunIDInObservationsAndErrors(t *testing.T) {
	ctx := context.Background()
	streamErr := errors.New("stream interrupted")
	model := &streamingRecordingModel{streamEvents: []StreamEvent{
		{Type: StreamEventError, Error: streamErr},
	}}
	recorder := &recordingObserver{}
	agent, err := New(Config{ID: "stream-agent"}, model, WithObserver(recorder))
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "start", WithRunID("stream-run"))
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)

	if len(got) != 1 || got[0].Type != StreamEventError {
		t.Fatalf("stream events = %#v, want one error event", got)
	}
	var agentErr *AgentError
	if !errors.As(got[0].Error, &agentErr) {
		t.Fatalf("stream error = %T, want *AgentError", got[0].Error)
	}
	if agentErr.RunID != "stream-run" {
		t.Fatalf("stream error run ID = %q, want stream-run", agentErr.RunID)
	}

	beforeModel := firstObservationOfType(t, recorder.Observations(), EventBeforeModel)
	afterModel := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	if beforeModel.RunID != "stream-run" || afterModel.RunID != "stream-run" {
		t.Fatalf("stream observation run IDs = %q/%q, want stream-run", beforeModel.RunID, afterModel.RunID)
	}
	if beforeModel.RequestID == "" || beforeModel.RequestID != afterModel.RequestID {
		t.Fatalf("stream observation request IDs = %q/%q, want matching non-empty IDs", beforeModel.RequestID, afterModel.RequestID)
	}
}

func TestAgentRunStreamPropagatesTraceContextToObservationsAndStreamErrors(t *testing.T) {
	trace := TraceContext{
		TraceID:    "trace-stream-123",
		SpanID:     "span-stream-456",
		TraceState: "vendor=stream",
	}
	ctx := WithTraceContext(context.Background(), trace)
	streamErr := errors.New("stream interrupted")
	model := &streamingRecordingModel{streamEvents: []StreamEvent{
		{Type: StreamEventError, Error: streamErr},
	}}
	recorder := &recordingObserver{}
	agent, err := New(Config{ID: "stream-trace-agent"}, model, WithObserver(recorder))
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "start")
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)

	if len(got) != 1 || got[0].Type != StreamEventError {
		t.Fatalf("stream events = %#v, want one error event", got)
	}
	var agentErr *AgentError
	if !errors.As(got[0].Error, &agentErr) {
		t.Fatalf("stream error = %T, want *AgentError", got[0].Error)
	}
	assertAgentErrorTraceContext(t, agentErr, trace)
	if agentErr.RunID == "" || agentErr.RunID == trace.TraceID {
		t.Fatalf("stream error run ID = %q, want generated ID distinct from trace ID %q", agentErr.RunID, trace.TraceID)
	}

	beforeModel := firstObservationOfType(t, recorder.Observations(), EventBeforeModel)
	afterModel := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	assertObservationTraceContext(t, beforeModel, trace)
	assertObservationTraceContext(t, afterModel, trace)
	if beforeModel.RunID == "" || beforeModel.RunID != afterModel.RunID || beforeModel.RunID == trace.TraceID {
		t.Fatalf("stream observation run IDs = %q/%q, want matching generated IDs distinct from trace ID %q", beforeModel.RunID, afterModel.RunID, trace.TraceID)
	}
	if beforeModel.RequestID == "" || beforeModel.RequestID != afterModel.RequestID {
		t.Fatalf("stream observation request IDs = %q/%q, want matching non-empty IDs", beforeModel.RequestID, afterModel.RequestID)
	}
}

func TestAgentRunStreamRejectsStreamedToolCalls(t *testing.T) {
	ctx := context.Background()
	model := &streamingRecordingModel{streamEvents: []StreamEvent{
		{
			Type: StreamEventDone,
			Message: Message{
				Role:      RoleAssistant,
				ToolCalls: []ToolCall{{ID: "call-1", Name: "echo"}},
			},
		},
	}}
	agent, err := New(Config{ID: "stream-agent"}, model)
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "use echo")
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)

	if len(got) != 1 {
		t.Fatalf("stream events = %#v, want one error event", got)
	}
	if got[0].Type != StreamEventError || got[0].AgentID != "stream-agent" || !errors.Is(got[0].Error, ErrStreamingToolCallsUnsupported) {
		t.Fatalf("streamed tool-call event = %#v, want ErrStreamingToolCallsUnsupported", got[0])
	}
	var agentErr *AgentError
	if !errors.As(got[0].Error, &agentErr) {
		t.Fatalf("streamed tool-call error = %T, want *AgentError", got[0].Error)
	}
	if agentErr.Category != ErrorCategoryStreaming || agentErr.Operation != "stream.tool_calls" {
		t.Fatalf("stream error category/operation = %q/%q, want streaming/stream.tool_calls", agentErr.Category, agentErr.Operation)
	}
	if agentErr.RequestID == "" || agentErr.Round != 1 {
		t.Fatalf("stream error context = %#v, want request ID and round", agentErr)
	}
	wantMessages := []Message{{Role: RoleUser, Content: "use echo"}}
	if got := agent.Messages(); !reflect.DeepEqual(got, wantMessages) {
		t.Fatalf("agent messages = %#v, want no assistant message for streamed tool call", got)
	}
}

func TestAgentRunStreamHookErrorDoesNotEmitRecursiveHookEvent(t *testing.T) {
	ctx := context.Background()
	hookErr := errors.New("hook rejected")
	model := &streamingRecordingModel{streamEvents: []StreamEvent{{Type: StreamEventDone, Message: Message{Role: RoleAssistant, Content: "done"}}}}
	var hookCalls int
	agent, err := New(Config{ID: "stream-agent"}, model,
		WithHook(func(ctx context.Context, event Event) error {
			hookCalls++
			if event.Type == EventAfterModel {
				return hookErr
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	events, err := agent.RunStream(ctx, "start")
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)

	if len(got) != 1 || got[0].Type != StreamEventError {
		t.Fatalf("stream events = %#v, want one hook error event", got)
	}
	if !errors.Is(got[0].Error, hookErr) {
		t.Fatalf("stream error = %v, want hook error", got[0].Error)
	}
	var agentErr *AgentError
	if !errors.As(got[0].Error, &agentErr) {
		t.Fatalf("stream error = %T, want *AgentError", got[0].Error)
	}
	if agentErr.Category != ErrorCategoryHook || agentErr.Operation != "hook.after_model" {
		t.Fatalf("stream hook error category/operation = %q/%q, want hook/hook.after_model", agentErr.Category, agentErr.Operation)
	}
	if hookCalls != 2 {
		t.Fatalf("hook calls = %d, want before_model plus one after_model hook call", hookCalls)
	}
}

func TestAgentMaxToolRoundsErrorHasStructuredContext(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "first"}}}},
		{ToolCalls: []ToolCall{{ID: "call-2", Name: "echo", Arguments: map[string]any{"text": "second"}}}},
	}}
	bot, err := New(Config{ID: "round-agent", MaxToolRounds: 1}, model,
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: "ok"}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "loop")
	if !errors.Is(err, ErrMaxToolRoundsExceeded) {
		t.Fatalf("err = %v, want ErrMaxToolRoundsExceeded", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryTool || agentErr.Operation != "tool.rounds" {
		t.Fatalf("agent error category/operation = %q/%q, want tool/tool.rounds", agentErr.Category, agentErr.Operation)
	}
	if agentErr.RequestID == "" || agentErr.Round != 2 {
		t.Fatalf("agent error context = %#v, want request ID and second round", agentErr)
	}
}

func TestAgentAutoCompactsContextAtConfiguredThreshold(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "after compact"}},
	}}
	compactor := &recordingCompactor{
		result: []Message{{Role: RoleSystem, Content: "summary of earlier conversation"}},
	}
	var events []Event
	agent, err := New(Config{
		SystemPrompt: "base",
		Compact: CompactConfig{
			MaxTokens: 10,
			Threshold: 0.5,
		},
	}, model,
		WithCompactor(compactor),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 3 })),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	agent.AppendMessage(Message{Role: RoleUser, Content: "one"})
	agent.AppendMessage(Message{Role: RoleAssistant, Content: "two"})

	if _, err := agent.Run(ctx, "three"); err != nil {
		t.Fatal(err)
	}

	if !compactor.called {
		t.Fatal("compactor was not called")
	}
	if got := model.requests[0].Messages; !reflect.DeepEqual(got, compactor.result) {
		t.Fatalf("model messages = %#v, want compacted messages %#v", got, compactor.result)
	}
	if !hasEvent(events, EventBeforeCompact) || !hasEvent(events, EventAfterCompact) {
		t.Fatalf("events = %#v, want before and after compact events", events)
	}
}

func TestModelCompactorSummarizesOlderMessagesAndKeepsRecentContext(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "the user asked about setup"}},
	}}
	compactor := ModelCompactor{
		Model:        model,
		SystemPrompt: "summarize older context",
		KeepLast:     1,
	}
	messages := []Message{
		{Role: RoleUser, Content: "how do I set this up?"},
		{Role: RoleAssistant, Content: "install Go first"},
		{Role: RoleUser, Content: "now continue"},
	}

	compacted, err := compactor.Compact(ctx, messages)
	if err != nil {
		t.Fatal(err)
	}

	if len(model.requests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(model.requests))
	}
	if !reflect.DeepEqual(model.requests[0].Messages, messages[:2]) {
		t.Fatalf("summarized messages = %#v, want older messages", model.requests[0].Messages)
	}
	if len(compacted) != 2 {
		t.Fatalf("compacted messages = %#v, want summary plus latest message", compacted)
	}
	if compacted[0].Role != RoleSystem || !strings.Contains(compacted[0].Content, "the user asked about setup") {
		t.Fatalf("summary message = %#v, want model summary as system message", compacted[0])
	}
	if !reflect.DeepEqual(compacted[1], messages[2]) {
		t.Fatalf("kept message = %#v, want latest message", compacted[1])
	}
}

func TestAgentExposesMCPServersToModelRequests(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "ok"}},
	}}
	server := MCPServerConfig{
		Name:      "filesystem",
		Command:   "mcp-filesystem",
		Args:      []string{"--root", "."},
		Env:       map[string]string{"MODE": "readonly"},
		Transport: MCPTransportStdio,
	}
	agent, err := New(Config{SystemPrompt: "base"}, model, WithMCPServers(server))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.Run(ctx, "list files"); err != nil {
		t.Fatal(err)
	}

	got := model.requests[0].MCPServers
	if !reflect.DeepEqual(got, []MCPServerConfig{server}) {
		t.Fatalf("mcp servers = %#v, want %#v", got, []MCPServerConfig{server})
	}
}

func TestAgentExecutesToolsThroughApprovalAndHooks(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	tool := ToolFunc{
		ToolName:        "echo",
		ToolDescription: "Echo text",
		Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			return ToolResult{CallID: call.ID, Name: call.Name, Content: call.Arguments["text"].(string)}, nil
		},
	}
	var approved []ApprovalRequest
	var events []Event
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(tool),
		WithApprovalPolicy(ApprovalFunc(func(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
			approved = append(approved, request)
			return ApprovalDecision{Approved: true, Reason: "test approval"}, nil
		})),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := agent.Run(ctx, "use echo")
	if err != nil {
		t.Fatal(err)
	}

	if response.Content != "done" {
		t.Fatalf("response content = %q, want done", response.Content)
	}
	if len(approved) != 1 || approved[0].ToolCall.Name != "echo" {
		t.Fatalf("approval requests = %#v, want one echo approval", approved)
	}
	secondRequest := model.requests[1]
	last := secondRequest.Messages[len(secondRequest.Messages)-1]
	if last.Role != RoleTool || last.ToolCallID != "call-1" || last.Content != "hello" {
		t.Fatalf("last message after tool = %#v, want tool result", last)
	}
	if !hasEvent(events, EventBeforeTool) || !hasEvent(events, EventAfterTool) {
		t.Fatalf("events = %#v, want before and after tool events", events)
	}
	if !hasEvent(events, EventBeforeModel) || !hasEvent(events, EventAfterModel) {
		t.Fatalf("events = %#v, want before and after model events", events)
	}
}

func TestAgentExposesToolParameterSchemaToModelRequests(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "ok"}},
	}}
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "search",
			ToolDescription: "Search documents",
			Parameters: &ToolParametersSchema{
				Type:     SchemaTypeObject,
				Required: []string{"query"},
				Properties: map[string]ToolParametersSchema{
					"query": {Type: SchemaTypeString, Description: "Search query"},
					"limit": {Type: SchemaTypeInteger},
				},
			},
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				return ToolResult{}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.Run(ctx, "search docs"); err != nil {
		t.Fatal(err)
	}

	tools := model.requests[0].Tools
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", tools)
	}
	if tools[0].Parameters == nil {
		t.Fatal("tool parameter schema was not exposed to the model request")
	}
	got := tools[0].Parameters.JSONSchema()
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer"},
		},
		"required": []string{"query"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool schema = %#v, want %#v", got, want)
	}
}

func TestAgentExecutesToolWhenSchemaArgumentsAreValid(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello", "count": 2}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	called := false
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Parameters: &ToolParametersSchema{
				Type:     SchemaTypeObject,
				Required: []string{"text", "count"},
				Properties: map[string]ToolParametersSchema{
					"text":  {Type: SchemaTypeString},
					"count": {Type: SchemaTypeInteger},
				},
			},
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				called = true
				return ToolResult{CallID: call.ID, Name: call.Name, Content: call.Arguments["text"].(string)}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := agent.Run(ctx, "use echo")
	if err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("tool was not called for valid schema arguments")
	}
	if response.Content != "done" {
		t.Fatalf("response content = %q, want done", response.Content)
	}
}

func TestAgentRejectsToolCallMissingRequiredSchemaArgument(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{}}}},
	}}
	called := false
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Parameters: &ToolParametersSchema{
				Type:     SchemaTypeObject,
				Required: []string{"text"},
				Properties: map[string]ToolParametersSchema{
					"text": {Type: SchemaTypeString},
				},
			},
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				called = true
				return ToolResult{}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(ctx, "use echo")
	if err == nil {
		t.Fatal("Run returned nil error, want validation error")
	}
	if !errors.Is(err, ErrToolValidation) {
		t.Fatalf("err = %v, want ErrToolValidation", err)
	}
	var validationErr *ToolValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("err = %T, want *ToolValidationError", err)
	}
	if validationErr.ToolName != "echo" || validationErr.Parameter != "text" {
		t.Fatalf("validation error = %#v, want echo text", validationErr)
	}
	if called {
		t.Fatal("tool was called after schema validation failed")
	}
}

func TestAgentRejectsToolCallWithSchemaTypeMismatch(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": 42}}}},
	}}
	called := false
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Parameters: &ToolParametersSchema{
				Type:     SchemaTypeObject,
				Required: []string{"text"},
				Properties: map[string]ToolParametersSchema{
					"text": {Type: SchemaTypeString},
				},
			},
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				called = true
				return ToolResult{}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(ctx, "use echo")
	if !errors.Is(err, ErrToolValidation) {
		t.Fatalf("err = %v, want ErrToolValidation", err)
	}
	var validationErr *ToolValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("err = %T, want *ToolValidationError", err)
	}
	if validationErr.ToolName != "echo" || validationErr.Parameter != "text" {
		t.Fatalf("validation error = %#v, want echo text", validationErr)
	}
	if called {
		t.Fatal("tool was called after schema validation failed")
	}
}

func TestAgentKeepsNoSchemaToolsCompatible(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "legacy", Arguments: map[string]any{"text": 42}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	called := false
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "legacy",
			ToolDescription: "Legacy tool without a parameter schema",
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				called = true
				return ToolResult{CallID: call.ID, Name: call.Name, Content: "legacy ok"}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := agent.Run(ctx, "use legacy")
	if err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("legacy tool was not called")
	}
	if response.Content != "done" {
		t.Fatalf("response content = %q, want done", response.Content)
	}
}

func TestAgentReturnsApprovalDeniedWhenPolicyRejectsTool(t *testing.T) {
	ctx := context.Background()
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "danger", Arguments: map[string]any{"path": "/"}}}},
	}}
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "danger",
			ToolDescription: "Dangerous operation",
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				t.Fatal("tool should not execute when approval is denied")
				return ToolResult{}, nil
			},
		}),
		WithApprovalPolicy(ApprovalFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
			return ApprovalDecision{Approved: false, Reason: "blocked"}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(ctx, "run danger")
	if !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("err = %v, want ErrApprovalDenied", err)
	}
}

func TestSubagentsCanInheritSelectedCapabilities(t *testing.T) {
	ctx := context.Background()
	master, err := New(Config{SystemPrompt: "master"}, &recordingModel{},
		WithSkills(
			Skill{Name: "review", Instructions: "review"},
			Skill{Name: "plan", Instructions: "plan"},
		),
		WithMCPServers(
			MCPServerConfig{Name: "fs", Command: "mcp-fs", Transport: MCPTransportStdio},
			MCPServerConfig{Name: "db", Command: "mcp-db", Transport: MCPTransportStdio},
		),
		WithTools(
			ToolFunc{ToolName: "echo", ToolDescription: "Echo", Fn: func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{}, nil }},
			ToolFunc{ToolName: "write", ToolDescription: "Write", Fn: func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{}, nil }},
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	child, err := master.SpawnSubagent(ctx, SubagentOptions{
		ID:                "selected-worker",
		Model:             &recordingModel{},
		InheritToolNames:  []string{"echo"},
		InheritSkillNames: []string{"review"},
		InheritMCPNames:   []string{"fs"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !child.HasTool("echo") || child.HasTool("write") {
		t.Fatalf("child tool inheritance did not match selected tool names")
	}
	if !child.HasSkill("review") || child.HasSkill("plan") {
		t.Fatalf("child skill inheritance did not match selected skill names")
	}
	servers := child.MCPServers()
	if len(servers) != 1 || servers[0].Name != "fs" {
		t.Fatalf("child MCP inheritance = %#v, want only fs", servers)
	}
}

func TestSendMessageToMissingSubagentReturnsStructuredSentinelAndEvent(t *testing.T) {
	ctx := context.Background()
	var events []Event
	master, err := New(Config{ID: "master"}, &recordingModel{},
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = master.SendMessageToSubagent(ctx, "missing-worker", "start")
	if !errors.Is(err, ErrSubagentNotFound) {
		t.Fatalf("err = %v, want ErrSubagentNotFound", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategorySubagent || agentErr.Operation != "subagent.lookup" {
		t.Fatalf("agent error category/operation = %q/%q, want subagent/subagent.lookup", agentErr.Category, agentErr.Operation)
	}
	if agentErr.RequestID == "" || agentErr.SubagentID != "missing-worker" {
		t.Fatalf("agent error context = %#v, want request ID and subagent ID", agentErr)
	}

	event := firstEventOfType(t, events, EventSubagentMessage)
	if event.RequestID != agentErr.RequestID || event.SubagentID != "missing-worker" || event.ErrorCategory != ErrorCategorySubagent {
		t.Fatalf("subagent event = %#v, want matching request ID, subagent ID, and category", event)
	}
	if !errors.Is(event.Error, ErrSubagentNotFound) {
		t.Fatalf("subagent event error = %v, want ErrSubagentNotFound", event.Error)
	}
}

func TestSubagentsCanInheritCapabilitiesAndExchangeMessages(t *testing.T) {
	ctx := context.Background()
	childModel := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "child response"}},
	}}
	var events []Event
	master, err := New(Config{SystemPrompt: "master"}, &recordingModel{},
		WithSkills(Skill{Name: "review", Instructions: "review carefully"}),
		WithMCPServers(MCPServerConfig{Name: "fs", Command: "mcp-fs", Transport: MCPTransportStdio}),
		WithTools(ToolFunc{
			ToolName:        "echo",
			ToolDescription: "Echo text",
			Fn:              func(context.Context, ToolCall) (ToolResult, error) { return ToolResult{}, nil },
		}),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	child, err := master.SpawnSubagent(ctx, SubagentOptions{
		ID:            "worker-1",
		SystemPrompt:  "worker",
		Model:         childModel,
		InheritTools:  true,
		InheritMCP:    true,
		InheritSkills: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !child.HasTool("echo") || !child.HasSkill("review") || len(child.MCPServers()) != 1 {
		t.Fatalf("child did not inherit requested capabilities")
	}
	response, err := master.SendMessageToSubagent(ctx, "worker-1", "start")
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "child response" {
		t.Fatalf("subagent response = %q, want child response", response.Content)
	}
	if err := child.SendToParent(ctx, "progress update"); err != nil {
		t.Fatal(err)
	}
	messages := master.DrainSubagentMessages("worker-1")
	if len(messages) != 1 || messages[0].Message.Content != "progress update" {
		t.Fatalf("parent inbox = %#v, want progress update", messages)
	}
	if !hasEvent(events, EventSubagentMessage) {
		t.Fatalf("events = %#v, want subagent message event", events)
	}
}

type recordingModel struct {
	requests  []ModelRequest
	responses []ModelResponse
}

func (m *recordingModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	m.requests = append(m.requests, request)
	if len(m.responses) == 0 {
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: ""}}, nil
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

type failingModel struct {
	err error
}

func (m failingModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	return ModelResponse{}, m.err
}

type streamingRecordingModel struct {
	requests     []ModelRequest
	streamEvents []StreamEvent
	streamErr    error
}

func (m *streamingRecordingModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	return ModelResponse{Message: Message{Role: RoleAssistant, Content: ""}}, nil
}

func (m *streamingRecordingModel) Stream(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
	m.requests = append(m.requests, request)
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		for _, event := range m.streamEvents {
			select {
			case <-ctx.Done():
				events <- StreamEvent{Type: StreamEventError, Error: ctx.Err()}
				return
			case events <- event:
			}
		}
	}()
	return events, nil
}

func collectStreamEvents(t *testing.T, events <-chan StreamEvent) []StreamEvent {
	t.Helper()
	var got []StreamEvent
	for event := range events {
		got = append(got, event)
	}
	return got
}

type recordingCompactor struct {
	called bool
	got    []Message
	result []Message
}

func (c *recordingCompactor) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	c.called = true
	c.got = append([]Message(nil), messages...)
	return append([]Message(nil), c.result...), nil
}

type recordingTokenCounter struct{}

func (recordingTokenCounter) Count(Message) int {
	return 1
}

func complexMessageForSessionTests() Message {
	return Message{
		Role:    RoleAssistant,
		Content: "tool ready",
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "lookup",
			Arguments: map[string]any{
				"query":  "sdk",
				"nested": map[string]any{"limit": "2"},
				"items":  []any{map[string]any{"id": "a"}},
			},
		}},
		Metadata: map[string]any{
			"trace": map[string]any{"id": "source"},
			"tags":  []any{"first"},
		},
	}
}

func mutateSessionMessage(message Message) {
	message.Content = "changed"
	message.ToolCalls[0].Arguments["query"] = "changed"
	message.ToolCalls[0].Arguments["nested"].(map[string]any)["limit"] = "99"
	message.ToolCalls[0].Arguments["items"].([]any)[0].(map[string]any)["id"] = "changed"
	message.Metadata["trace"].(map[string]any)["id"] = "changed"
	message.Metadata["tags"].([]any)[0] = "changed"
}

func assertComplexSessionMessageUnchanged(t *testing.T, message Message) {
	t.Helper()
	if message.Content != "tool ready" {
		t.Fatalf("message content = %q, want tool ready", message.Content)
	}
	if got := message.ToolCalls[0].Arguments["query"]; got != "sdk" {
		t.Fatalf("tool argument query = %#v, want sdk", got)
	}
	if got := message.ToolCalls[0].Arguments["nested"].(map[string]any)["limit"]; got != "2" {
		t.Fatalf("nested tool argument limit = %#v, want 2", got)
	}
	if got := message.ToolCalls[0].Arguments["items"].([]any)[0].(map[string]any)["id"]; got != "a" {
		t.Fatalf("nested tool argument item ID = %#v, want a", got)
	}
	if got := message.Metadata["trace"].(map[string]any)["id"]; got != "source" {
		t.Fatalf("metadata trace ID = %#v, want source", got)
	}
	if got := message.Metadata["tags"].([]any)[0]; got != "first" {
		t.Fatalf("metadata tag = %#v, want first", got)
	}
}

func containsMessageContent(messages []Message, content string) bool {
	for _, message := range messages {
		if message.Content == content {
			return true
		}
	}
	return false
}

func hasEvent(events []Event, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func firstEventOfType(t *testing.T, events []Event, eventType EventType) Event {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("events = %#v, want %s", events, eventType)
	return Event{}
}

func firstEventOfTypeAndRound(t *testing.T, events []Event, eventType EventType, round int) Event {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType && event.Round == round {
			return event
		}
	}
	t.Fatalf("events = %#v, want %s in round %d", events, eventType, round)
	return Event{}
}

func assertEventTraceContext(t *testing.T, event Event, trace TraceContext) {
	t.Helper()
	if event.TraceID != trace.TraceID || event.SpanID != trace.SpanID || event.TraceState != trace.TraceState {
		t.Fatalf("event trace context = %q/%q/%q, want %q/%q/%q",
			event.TraceID,
			event.SpanID,
			event.TraceState,
			trace.TraceID,
			trace.SpanID,
			trace.TraceState,
		)
	}
}

func assertObservationTraceContext(t *testing.T, observation Observation, trace TraceContext) {
	t.Helper()
	if observation.TraceID != trace.TraceID || observation.SpanID != trace.SpanID || observation.TraceState != trace.TraceState {
		t.Fatalf("observation trace context = %q/%q/%q, want %q/%q/%q",
			observation.TraceID,
			observation.SpanID,
			observation.TraceState,
			trace.TraceID,
			trace.SpanID,
			trace.TraceState,
		)
	}
}

func assertAgentErrorTraceContext(t *testing.T, agentErr *AgentError, trace TraceContext) {
	t.Helper()
	if agentErr.TraceID != trace.TraceID || agentErr.SpanID != trace.SpanID || agentErr.TraceState != trace.TraceState {
		t.Fatalf("agent error trace context = %q/%q/%q, want %q/%q/%q",
			agentErr.TraceID,
			agentErr.SpanID,
			agentErr.TraceState,
			trace.TraceID,
			trace.SpanID,
			trace.TraceState,
		)
	}
}

func hasEventForAgent(events []Event, agentID string, eventType EventType) bool {
	for _, event := range events {
		if event.AgentID == agentID && event.Type == eventType {
			return true
		}
	}
	return false
}
