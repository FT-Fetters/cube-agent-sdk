package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
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
	if afterModel.Duration <= 0 || afterModel.EstimatedTokens <= 0 || afterModel.Failed {
		t.Fatalf("after model observation = %#v, want duration, tokens, and success", afterModel)
	}

	afterTool := firstObservationOfType(t, observations, EventAfterTool)
	if afterTool.ToolName != "echo" || afterTool.RequestID == "" || afterTool.Duration <= 0 || afterTool.EstimatedTokens <= 0 {
		t.Fatalf("after tool observation = %#v, want safe tool metadata", afterTool)
	}

	afterApproval := firstObservationOfType(t, observations, EventAfterApproval)
	if afterApproval.ToolName != "echo" || afterApproval.RequestID == "" || afterApproval.Duration <= 0 || afterApproval.Failed {
		t.Fatalf("after approval observation = %#v, want successful approval metadata", afterApproval)
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
	value := reflect.ValueOf(observation)
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Kind() == reflect.String && strings.Contains(field.String(), secret) {
			t.Fatalf("observation leaked sensitive content in field %s", value.Type().Field(i).Name)
		}
	}
}
