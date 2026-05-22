package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestScriptedModelRecordsRequestsAndFinalTranscript(t *testing.T) {
	ctx := context.Background()
	recorder := NewEvalRecorder()
	model := NewScriptedModel(ScriptedResponse(ModelResponse{
		Message: Message{Role: RoleAssistant, Content: "ready"},
		Usage:   TokenUsage{InputTokens: 3, OutputTokens: 1, TotalTokens: 4},
	})).RecordWith(recorder)

	bot, err := New(Config{ID: "eval-agent", SystemPrompt: "base"}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, runErr := bot.Run(ctx, "hello")
	transcript := recorder.RecordRun("hello", reply, runErr)

	if runErr != nil {
		t.Fatal(runErr)
	}
	AssertFinalMessage(t, transcript, "ready")
	AssertEventOrder(t, transcript, EventBeforeModel, EventAfterModel)
	AssertObservation(t, transcript, ObservationExpectation{
		Type: EventAfterModel,
	})

	requests := model.Requests()
	if len(requests) != 1 {
		t.Fatalf("scripted model requests = %d, want 1", len(requests))
	}
	if got := requests[0].Messages; len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("recorded request messages = %#v, want user input", got)
	}

	if len(transcript.ModelExchanges) != 1 {
		t.Fatalf("transcript model exchanges = %d, want 1", len(transcript.ModelExchanges))
	}
	exchange := transcript.ModelExchanges[0]
	if exchange.Request.SystemPrompt != "base" || exchange.Response == nil || exchange.Response.Message.Content != "ready" {
		t.Fatalf("model exchange = %#v, want request and scripted response", exchange)
	}
}

func TestEvalAssertionsCoverToolChoiceAndObservations(t *testing.T) {
	ctx := context.Background()
	recorder := NewEvalRecorder()
	model := NewScriptedModel(
		ScriptedResponse(ModelResponse{
			ToolCalls: []ToolCall{{
				ID:        "call-1",
				Name:      "lookup_account",
				Arguments: map[string]any{"account_id": "acct_123"},
			}},
		}),
		ScriptedResponse(ModelResponse{
			Message: Message{Role: RoleAssistant, Content: "account is active"},
		}),
	).RecordWith(recorder)

	bot, err := New(Config{ID: "tool-eval-agent"}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName:        "lookup_account",
			ToolDescription: "Read account status",
			ToolRisk:        ToolRiskRead,
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{
					CallID:  call.ID,
					Name:    call.Name,
					Content: "active",
					Metadata: map[string]any{
						"source": "fixture",
					},
				}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, runErr := bot.Run(ctx, "check account")
	transcript := recorder.RecordRun("check account", reply, runErr)
	if runErr != nil {
		t.Fatal(runErr)
	}

	AssertToolCall(t, transcript, ToolCallExpectation{
		Name:          "lookup_account",
		Arguments:     map[string]any{"account_id": "acct_123"},
		ResultContent: "active",
	})
	AssertObservation(t, transcript, ObservationExpectation{
		Type:     EventAfterTool,
		ToolName: "lookup_account",
	})
	AssertEventOrder(t, transcript,
		EventBeforeModel,
		EventAfterModel,
		EventBeforeApproval,
		EventAfterApproval,
		EventBeforeTool,
		EventAfterTool,
		EventBeforeModel,
		EventAfterModel,
	)
	AssertFinalMessage(t, transcript, "account is active")
}

func TestEvalApprovalDenialAssertion(t *testing.T) {
	ctx := context.Background()
	recorder := NewEvalRecorder()
	model := NewScriptedModel(ScriptedResponse(ModelResponse{
		ToolCalls: []ToolCall{{
			ID:        "call-1",
			Name:      "delete_account",
			Arguments: map[string]any{"account_id": "acct_123"},
		}},
	})).RecordWith(recorder)

	bot, err := New(Config{ID: "approval-eval-agent"}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
		WithApprovalPolicy(DenyAllApproval{}),
		WithTools(ToolFunc{
			ToolName:        "delete_account",
			ToolDescription: "Delete account",
			ToolRisk:        ToolRiskDestructive,
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				t.Fatal("denied tool should not run")
				return ToolResult{}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, runErr := bot.Run(ctx, "delete it")
	transcript := recorder.RecordRun("delete it", reply, runErr)

	if !errors.Is(runErr, ErrApprovalDenied) {
		t.Fatalf("run error = %v, want ErrApprovalDenied", runErr)
	}
	AssertApprovalDenied(t, transcript, "delete_account")
	AssertFinalError(t, transcript, "approval denied")
}

func TestScriptedModelErrorAssertion(t *testing.T) {
	ctx := context.Background()
	recorder := NewEvalRecorder()
	model := NewScriptedModel(ScriptedError(errors.New("scripted model unavailable"))).RecordWith(recorder)
	bot, err := New(Config{ID: "model-error-eval-agent"}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, runErr := bot.Run(ctx, "hello")
	transcript := recorder.RecordRun("hello", reply, runErr)

	if runErr == nil {
		t.Fatal("run error = nil, want scripted model error")
	}
	AssertFinalError(t, transcript, "scripted model unavailable")
	AssertObservation(t, transcript, ObservationExpectation{
		Type:          EventAfterModel,
		Failed:        true,
		ErrorCategory: ErrorCategoryModel,
	})
}

func TestEvalRecorderCapturesCompaction(t *testing.T) {
	ctx := context.Background()
	recorder := NewEvalRecorder()
	model := NewScriptedModel(ScriptedResponse(ModelResponse{
		Message: Message{Role: RoleAssistant, Content: "compacted"},
	})).RecordWith(recorder)
	bot, err := New(Config{
		ID: "compact-eval-agent",
		Compact: CompactConfig{
			MaxTokens: 2,
			Threshold: 1,
			KeepLast:  1,
		},
	}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
		WithTokenCounter(TokenCounterFunc(func(Message) int { return 1 })),
		WithCompactor(SummaryCompactor{KeepLast: 1}),
	)
	if err != nil {
		t.Fatal(err)
	}
	bot.AppendMessage(Message{Role: RoleUser, Content: "older"})

	reply, runErr := bot.Run(ctx, "new")
	transcript := recorder.RecordRun("new", reply, runErr)
	if runErr != nil {
		t.Fatal(runErr)
	}

	AssertObservation(t, transcript, ObservationExpectation{Type: EventBeforeCompact})
	AssertObservation(t, transcript, ObservationExpectation{Type: EventAfterCompact})
	AssertFinalMessage(t, transcript, "compacted")
}

func TestScriptedStreamModelFailureAssertion(t *testing.T) {
	ctx := context.Background()
	recorder := NewEvalRecorder()
	model := NewScriptedStreamModel(ScriptedStreamEvents(
		StreamEvent{Type: StreamEventDelta, Delta: "partial"},
		StreamEvent{Type: StreamEventError, Error: errors.New("scripted stream failed")},
	)).RecordWith(recorder)
	bot, err := New(Config{ID: "stream-eval-agent"}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	events, runErr := bot.RunStream(ctx, "stream please", WithStreamObservations())
	if runErr != nil {
		t.Fatal(runErr)
	}
	var streamErr error
	for event := range events {
		if event.Type == StreamEventError {
			streamErr = event.Error
		}
	}
	transcript := recorder.RecordRun("stream please", Message{}, streamErr)

	if streamErr == nil {
		t.Fatal("stream error = nil, want scripted stream failure")
	}
	AssertFinalError(t, transcript, "scripted stream failed")
	AssertObservation(t, transcript, ObservationExpectation{
		Type:          EventStreamError,
		Failed:        true,
		ErrorCategory: ErrorCategoryModel,
	})
	if len(transcript.StreamExchanges) != 1 || len(transcript.StreamExchanges[0].Events) != 2 {
		t.Fatalf("stream exchanges = %#v, want scripted stream events", transcript.StreamExchanges)
	}
}

func TestRunTranscriptStableJSONAndSafetyBoundaries(t *testing.T) {
	ctx := context.Background()
	const secret = "secret-runtime-token"
	recorder := NewEvalRecorder()
	model := NewScriptedModel(ScriptedResponse(ModelResponse{
		Message: Message{Role: RoleAssistant, Content: "safe"},
	})).RecordWith(recorder)
	bot, err := New(Config{ID: "safe-eval-agent", SystemPrompt: "base"}, model,
		WithHook(recorder.Hook()),
		WithObserver(recorder),
		WithMCPServers(MCPServerConfig{
			Name:      "files",
			Command:   "/bin/sdk-" + secret,
			Args:      []string{"--token", secret},
			Env:       map[string]string{"API_KEY": secret},
			URL:       "https://example.test/rpc?token=" + secret,
			Transport: MCPTransportHTTP,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, runErr := bot.Run(ctx, "hello")
	transcript := recorder.RecordRun("hello", reply, runErr)
	if runErr != nil {
		t.Fatal(runErr)
	}

	first, err := transcript.StableJSON()
	if err != nil {
		t.Fatal(err)
	}
	second, err := transcript.StableJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("stable JSON mismatch:\nfirst=%s\nsecond=%s", first, second)
	}
	if strings.Contains(string(first), secret) {
		t.Fatalf("transcript leaked runtime configuration secret: %s", first)
	}
	if !strings.Contains(string(first), `"mcp_servers"`) {
		t.Fatalf("transcript JSON = %s, want sanitized MCP server metadata", first)
	}

	var decoded RunTranscript
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, transcript) {
		t.Fatalf("decoded transcript = %#v, want %#v", decoded, transcript)
	}
}

func TestReplayRunTranscriptAndSessionEvents(t *testing.T) {
	transcript := RunTranscript{
		SchemaVersion: EvalTranscriptSchemaVersion,
		Inputs:        []string{"hello"},
		Final: &TranscriptOutcome{
			Message: &Message{Role: RoleAssistant, Content: "ok"},
		},
	}
	payload, err := transcript.StableJSON()
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := ReplayRunTranscript(payload)
	if err != nil {
		t.Fatal(err)
	}
	AssertFinalMessage(t, replayed, "ok")

	events := []SessionEvent{
		{
			ID:            "session-1:1",
			SessionID:     "session-1",
			SchemaVersion: CurrentSessionSchemaVersion,
			Sequence:      1,
			Type:          SessionEventRunStarted,
			RunID:         "run-1",
			Metadata:      map[string]string{"agent_id": "eval-agent"},
		},
		{
			ID:            "session-1:2",
			SessionID:     "session-1",
			SchemaVersion: CurrentSessionSchemaVersion,
			Sequence:      2,
			Type:          SessionEventRunCompleted,
			RunID:         "run-1",
		},
	}
	eventTranscript, err := ReplaySessionEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	AssertSessionEventOrder(t, eventTranscript, SessionEventRunStarted, SessionEventRunCompleted)

	if _, err := ReplaySessionEvents([]SessionEvent{events[1], events[0]}); err == nil {
		t.Fatal("replay unordered session events error = nil, want error")
	}
}
