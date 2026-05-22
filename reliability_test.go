package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReliableModelRetriesRetryableFailuresAndSucceeds(t *testing.T) {
	model := &scriptedReliableModel{}
	model.generate = func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
		if request.AgentID != "agent-1" {
			t.Fatalf("request agent ID = %q, want agent-1", request.AgentID)
		}
		if requestCallCount := len(request.Messages); requestCallCount != 1 {
			t.Fatalf("request messages = %d, want 1", requestCallCount)
		}
		if model.currentGenerateCall() < 3 {
			return ModelResponse{}, NewProviderError("temporarily unavailable", ProviderDiagnostics{HTTPStatus: 500}, errors.New("raw provider text"))
		}
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(3),
		WithReliableBackoff(func(int) time.Duration { return 0 }),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	response, err := wrapped.Generate(context.Background(), ModelRequest{
		AgentID:  "agent-1",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "ok" {
		t.Fatalf("response = %#v, want ok", response)
	}
	if model.generateCalls != 3 {
		t.Fatalf("attempts = %d, want 3", model.generateCalls)
	}
	requireReliabilityEventTypes(t, events,
		ReliabilityEventAttemptStart,
		ReliabilityEventAttemptFailure,
		ReliabilityEventRetryScheduled,
		ReliabilityEventAttemptStart,
		ReliabilityEventAttemptFailure,
		ReliabilityEventRetryScheduled,
		ReliabilityEventAttemptStart,
		ReliabilityEventSuccess,
	)
}

func TestReliableModelDoesNotRetryNonRetryableFailures(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{}, NewProviderError("bad request", ProviderDiagnostics{HTTPStatus: 400}, errors.New("raw validation details"))
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(3),
		WithReliableBackoff(func(int) time.Duration { return 0 }),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	_, err := wrapped.Generate(context.Background(), ModelRequest{})
	if err == nil {
		t.Fatal("Generate error = nil, want non-retryable error")
	}
	if model.generateCalls != 1 {
		t.Fatalf("attempts = %d, want 1", model.generateCalls)
	}
	requireReliabilityEventTypes(t, events,
		ReliabilityEventAttemptStart,
		ReliabilityEventAttemptFailure,
		ReliabilityEventFinalFailure,
	)
	if events[1].Retryable {
		t.Fatalf("attempt failure retryable = true, want false")
	}
}

func TestReliableModelUsesBackoffHook(t *testing.T) {
	model := &scriptedReliableModel{}
	model.generate = func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
		if model.currentGenerateCall() == 1 {
			return ModelResponse{}, NewProviderError("rate limited", ProviderDiagnostics{HTTPStatus: 429}, nil)
		}
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
	}
	var backoffAttempts []int
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(2),
		WithReliableBackoff(func(attempt int) time.Duration {
			backoffAttempts = append(backoffAttempts, attempt)
			return 0
		}),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	if _, err := wrapped.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(backoffAttempts); got != "[1]" {
		t.Fatalf("backoff attempts = %s, want [1]", got)
	}
	var scheduled ReliabilityEvent
	for _, event := range events {
		if event.Type == ReliabilityEventRetryScheduled {
			scheduled = event
			break
		}
	}
	if scheduled.Type == "" || scheduled.Attempt != 1 {
		t.Fatalf("retry event = %#v, want attempt 1", scheduled)
	}
}

func TestReliableModelPerAttemptTimeoutRetries(t *testing.T) {
	model := &scriptedReliableModel{}
	model.generate = func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
		if model.currentGenerateCall() == 1 {
			<-ctx.Done()
			return ModelResponse{}, ctx.Err()
		}
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: "after-timeout"}}, nil
	}
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(2),
		WithReliablePerAttemptTimeout(5*time.Millisecond),
		WithReliableBackoff(func(int) time.Duration { return 0 }),
	)

	response, err := wrapped.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "after-timeout" {
		t.Fatalf("response = %q, want after-timeout", response.Message.Content)
	}
	if model.generateCalls != 2 {
		t.Fatalf("attempts = %d, want 2", model.generateCalls)
	}
}

func TestReliableModelTotalTimeoutStopsRetries(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{}, NewProviderError("server error", ProviderDiagnostics{HTTPStatus: 500}, nil)
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(3),
		WithReliableTotalTimeout(5*time.Millisecond),
		WithReliableBackoff(func(int) time.Duration { return 20 * time.Millisecond }),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	_, err := wrapped.Generate(context.Background(), ModelRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Generate error = %v, want context deadline exceeded", err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("attempts = %d, want 1", model.generateCalls)
	}
	requireLastReliabilityEventType(t, events, ReliabilityEventFinalFailure)
}

func TestReliableModelRejectsWhenRateLimited(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableRateLimit(1, time.Minute),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	if _, err := wrapped.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatal(err)
	}
	_, err := wrapped.Generate(context.Background(), ModelRequest{})
	if !errors.Is(err, ErrReliableRateLimited) {
		t.Fatalf("second Generate error = %v, want ErrReliableRateLimited", err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("attempts = %d, want 1", model.generateCalls)
	}
	requireLastReliabilityEventType(t, events, ReliabilityEventRateRejected)
}

func TestReliableModelRejectsWhenCircuitOpen(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{}, NewProviderError("server error", ProviderDiagnostics{HTTPStatus: 500}, nil)
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(1),
		WithReliableCircuitBreaker(1, time.Hour),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	_, err := wrapped.Generate(context.Background(), ModelRequest{})
	if err == nil {
		t.Fatal("first Generate error = nil, want model error")
	}
	_, err = wrapped.Generate(context.Background(), ModelRequest{})
	if !errors.Is(err, ErrReliableCircuitOpen) {
		t.Fatalf("second Generate error = %v, want ErrReliableCircuitOpen", err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("attempts = %d, want 1", model.generateCalls)
	}
	requireLastReliabilityEventType(t, events, ReliabilityEventCircuitRejected)
}

func TestReliableModelExpiredCallerDeadlineDoesNotOpenCircuit(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
		},
	}
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(1),
		WithReliableCircuitBreaker(1, time.Hour),
	)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := wrapped.Generate(expired, ModelRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired Generate error = %v, want context deadline exceeded", err)
	}
	if model.generateCalls != 0 {
		t.Fatalf("attempts after expired context = %d, want 0", model.generateCalls)
	}

	response, err := wrapped.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatalf("healthy Generate after expired context error = %v, want nil", err)
	}
	if response.Message.Content != "ok" {
		t.Fatalf("healthy response = %q, want ok", response.Message.Content)
	}
}

func TestReliableModelCallerDeadlineDuringBackoffDoesNotOpenCircuit(t *testing.T) {
	model := &scriptedReliableModel{}
	model.generate = func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
		if model.currentGenerateCall() == 1 {
			return ModelResponse{}, NewProviderError("server error", ProviderDiagnostics{HTTPStatus: 500}, nil)
		}
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
	}
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(2),
		WithReliableBackoff(func(int) time.Duration { return 50 * time.Millisecond }),
		WithReliableCircuitBreaker(2, time.Hour),
	)
	deadlineCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := wrapped.Generate(deadlineCtx, ModelRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("backoff-interrupted Generate error = %v, want context deadline exceeded", err)
	}
	if model.generateCalls != 1 {
		t.Fatalf("attempts before interrupted backoff = %d, want 1", model.generateCalls)
	}

	response, err := wrapped.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatalf("healthy Generate after interrupted backoff error = %v, want nil", err)
	}
	if response.Message.Content != "ok" {
		t.Fatalf("healthy response = %q, want ok", response.Message.Content)
	}
}

func TestReliableModelRejectsWhenTokenBudgetExceeded(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableTokenBudget(3),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	_, err := wrapped.Generate(context.Background(), ModelRequest{
		Messages: []Message{{Role: RoleUser, Content: "one two three four"}},
	})
	if !errors.Is(err, ErrReliableBudgetExceeded) {
		t.Fatalf("Generate error = %v, want ErrReliableBudgetExceeded", err)
	}
	if model.generateCalls != 0 {
		t.Fatalf("attempts = %d, want 0", model.generateCalls)
	}
	requireLastReliabilityEventType(t, events, ReliabilityEventBudgetRejected)
	if events[len(events)-1].BudgetKind != "token" {
		t.Fatalf("budget kind = %q, want token", events[len(events)-1].BudgetKind)
	}
}

func TestReliableModelRejectsWhenCostBudgetExceeded(t *testing.T) {
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableCostBudget(0.001, 1, 1),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	_, err := wrapped.Generate(context.Background(), ModelRequest{
		Messages: []Message{{Role: RoleUser, Content: "one two"}},
	})
	if !errors.Is(err, ErrReliableBudgetExceeded) {
		t.Fatalf("Generate error = %v, want ErrReliableBudgetExceeded", err)
	}
	if model.generateCalls != 0 {
		t.Fatalf("attempts = %d, want 0", model.generateCalls)
	}
	requireLastReliabilityEventType(t, events, ReliabilityEventBudgetRejected)
	if events[len(events)-1].BudgetKind != "cost" {
		t.Fatalf("budget kind = %q, want cost", events[len(events)-1].BudgetKind)
	}
}

func TestReliableModelWrapsStreamModelAndDoesNotRetryAfterStreamStarts(t *testing.T) {
	model := &scriptedReliableStreamModel{}
	model.stream = func(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
		model.mu.Lock()
		model.streamCalls++
		model.mu.Unlock()
		events := make(chan StreamEvent, 2)
		events <- StreamEvent{Type: StreamEventDelta, Delta: "partial"}
		events <- StreamEvent{Type: StreamEventError, Error: NewProviderError("server error", ProviderDiagnostics{HTTPStatus: 500}, nil)}
		close(events)
		return events, nil
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(3),
		WithReliableBackoff(func(int) time.Duration { return 0 }),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)
	streamModel, ok := wrapped.(StreamModel)
	if !ok {
		t.Fatalf("wrapped model = %T, want StreamModel", wrapped)
	}

	stream, err := streamModel.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var got []StreamEvent
	for event := range stream {
		got = append(got, event)
	}
	if model.streamCalls != 1 {
		t.Fatalf("stream calls = %d, want 1", model.streamCalls)
	}
	if len(got) != 2 || got[0].Type != StreamEventDelta || got[1].Type != StreamEventError {
		t.Fatalf("stream events = %#v, want delta then error", got)
	}
	requireReliabilityEventTypes(t, events,
		ReliabilityEventAttemptStart,
		ReliabilityEventSuccess,
	)
}

func TestReliableModelRetriesRetryableStreamStartFailure(t *testing.T) {
	model := &scriptedReliableStreamModel{}
	model.stream = func(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
		model.mu.Lock()
		defer model.mu.Unlock()
		model.streamCalls++
		if model.streamCalls == 1 {
			return nil, NewProviderError("rate limited", ProviderDiagnostics{HTTPStatus: 429}, nil)
		}
		events := make(chan StreamEvent, 1)
		events <- StreamEvent{Type: StreamEventDone, Message: Message{Role: RoleAssistant, Content: "ok"}}
		close(events)
		return events, nil
	}
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(2),
		WithReliableBackoff(func(int) time.Duration { return 0 }),
	)
	streamModel, ok := wrapped.(StreamModel)
	if !ok {
		t.Fatalf("wrapped model = %T, want StreamModel", wrapped)
	}

	stream, err := streamModel.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if model.streamCalls != 2 {
		t.Fatalf("stream calls = %d, want 2", model.streamCalls)
	}
}

func TestReliabilityEventsDoNotExposeSensitivePayloads(t *testing.T) {
	const (
		promptSecret = "prompt-secret"
		rawSecret    = "raw-provider-secret"
	)
	model := &scriptedReliableModel{
		generate: func(ctx context.Context, request ModelRequest) (ModelResponse, error) {
			return ModelResponse{}, NewProviderError("safe provider message", ProviderDiagnostics{
				Provider:     "provider",
				HTTPStatus:   500,
				EndpointHost: "https://api.example.test/v1/chat/completions?token=hidden",
				RequestID:    "request-1",
			}, errors.New(rawSecret))
		},
	}
	var events []ReliabilityEvent
	wrapped := NewReliableModel(model,
		WithReliableMaxAttempts(1),
		WithReliabilityObserver(func(ctx context.Context, event ReliabilityEvent) {
			events = append(events, event)
		}),
	)

	_, err := wrapped.Generate(context.Background(), ModelRequest{
		SystemPrompt: promptSecret,
		Messages:     []Message{{Role: RoleUser, Content: promptSecret}},
		Tools: []ToolDescriptor{{
			Name:        "lookup",
			Description: promptSecret,
		}},
	})
	if err == nil {
		t.Fatal("Generate error = nil, want provider error")
	}
	rendered := fmt.Sprintf("%#v", events)
	for _, unsafe := range []string{promptSecret, rawSecret, "hidden"} {
		if strings.Contains(rendered, unsafe) {
			t.Fatalf("reliability events expose %q: %s", unsafe, rendered)
		}
	}
	if !strings.Contains(rendered, "api.example.test") {
		t.Fatalf("reliability events = %s, want safe endpoint host", rendered)
	}
}

type scriptedReliableModel struct {
	mu            sync.Mutex
	generateCalls int
	generate      func(context.Context, ModelRequest) (ModelResponse, error)
}

func (m *scriptedReliableModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	m.mu.Lock()
	m.generateCalls++
	if m.generate == nil {
		m.mu.Unlock()
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
	}
	generate := m.generate
	m.mu.Unlock()
	return generate(ctx, request)
}

func (m *scriptedReliableModel) currentGenerateCall() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generateCalls
}

type scriptedReliableStreamModel struct {
	scriptedReliableModel
	streamCalls int
	stream      func(context.Context, ModelRequest) (<-chan StreamEvent, error)
}

func (m *scriptedReliableStreamModel) Stream(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
	if m.stream == nil {
		m.mu.Lock()
		m.streamCalls++
		m.mu.Unlock()
		events := make(chan StreamEvent, 1)
		events <- StreamEvent{Type: StreamEventDone, Message: Message{Role: RoleAssistant, Content: "ok"}}
		close(events)
		return events, nil
	}
	return m.stream(ctx, request)
}

func requireReliabilityEventTypes(t *testing.T, events []ReliabilityEvent, want ...ReliabilityEventType) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("event count = %d (%#v), want %d (%#v)", len(events), events, len(want), want)
	}
	for i, event := range events {
		if event.Type != want[i] {
			t.Fatalf("event %d = %q, want %q; all events %#v", i, event.Type, want[i], events)
		}
	}
}

func requireLastReliabilityEventType(t *testing.T, events []ReliabilityEvent, want ReliabilityEventType) {
	t.Helper()
	if len(events) == 0 {
		t.Fatalf("events are empty, want last event %q", want)
	}
	if got := events[len(events)-1].Type; got != want {
		t.Fatalf("last event = %q, want %q; all events %#v", got, want, events)
	}
}
