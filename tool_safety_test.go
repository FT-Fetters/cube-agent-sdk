package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestToolSafetyTimeoutResultLimitAndAuditMetadata(t *testing.T) {
	ctx := context.Background()
	const resultSecret = "tool-safety-result-secret"
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "slow_lookup"}}},
	}}
	bot, err := New(Config{ID: "tool-safety-agent", SystemPrompt: "base"}, model,
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName: "slow_lookup",
			Safety: ToolSafety{
				Timeout:        5 * time.Millisecond,
				MaxResultBytes: len("small"),
				Risk:           ToolRiskRead,
			},
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				<-ctx.Done()
				return ToolResult{
					CallID:  call.ID,
					Name:    call.Name,
					Content: resultSecret,
				}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "run slow lookup")
	if err != nil {
		t.Fatalf("Run error = %v, want timeout feedback to continue", err)
	}

	afterTool := firstObservationOfType(t, recorder.Observations(), EventAfterTool)
	if !afterTool.Failed || afterTool.ErrorCategory != ErrorCategoryTool {
		t.Fatalf("after tool observation = %#v, want failed tool category", afterTool)
	}
	if afterTool.ToolRisk != ToolRiskRead {
		t.Fatalf("after tool risk = %q, want read", afterTool.ToolRisk)
	}
	if !afterTool.ToolSafety.TimeoutConfigured || afterTool.ToolSafety.Timeout != 5*time.Millisecond {
		t.Fatalf("tool safety timeout metadata = %#v, want configured 5ms timeout", afterTool.ToolSafety)
	}
	if afterTool.ToolSafety.MaxResultBytes != len("small") {
		t.Fatalf("tool safety max result bytes = %d, want %d", afterTool.ToolSafety.MaxResultBytes, len("small"))
	}
	assertObservationDoesNotContain(t, afterTool, resultSecret)
}

func TestToolSafetyRejectsOversizedResultsBeforeAppendingToContext(t *testing.T) {
	ctx := context.Background()
	const resultSecret = "oversized-tool-result-secret"
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "lookup"}}},
	}}
	bot, err := New(Config{ID: "tool-result-limit-agent", SystemPrompt: "base"}, model,
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName: "lookup",
			Safety: ToolSafety{
				Risk:           ToolRiskRead,
				MaxResultBytes: len("short"),
			},
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: resultSecret}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "run lookup")
	if err != nil {
		t.Fatalf("Run error = %v, want result-size feedback to continue", err)
	}
	for _, message := range bot.Messages() {
		if strings.Contains(message.Content, resultSecret) {
			t.Fatalf("agent context leaked oversized tool result: %#v", bot.Messages())
		}
	}
	afterTool := firstObservationOfType(t, recorder.Observations(), EventAfterTool)
	if !afterTool.Failed || afterTool.ErrorCategory != ErrorCategoryTool {
		t.Fatalf("after tool observation = %#v, want failed tool category", afterTool)
	}
	metadata := toolResultMetadataFromObservation(t, afterTool)
	if metadata.contentBytes != len(resultSecret) {
		t.Fatalf("result metadata content bytes = %d, want %d", metadata.contentBytes, len(resultSecret))
	}
	assertObservationDoesNotContain(t, afterTool, resultSecret)
}

func TestToolSafetyRejectsConcurrentExecutionsAboveLimit(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	bot, err := New(Config{ID: "tool-concurrency-agent", SystemPrompt: "base"}, succeedingModel{},
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName: "limited_lookup",
			Safety: ToolSafety{
				Risk:           ToolRiskRead,
				MaxConcurrency: 1,
			},
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				close(started)
				<-release
				return ToolResult{CallID: call.ID, Name: call.Name, Content: "ok"}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_, err := bot.executeTool(ctx, ToolCall{ID: "call-1", Name: "limited_lookup"}, 1, "model-request")
		firstDone <- err
	}()
	<-started

	_, err = bot.executeTool(ctx, ToolCall{ID: "call-2", Name: "limited_lookup"}, 1, "model-request")
	if !errors.Is(err, ErrToolConcurrencyLimitExceeded) {
		t.Fatalf("second executeTool error = %v, want ErrToolConcurrencyLimitExceeded", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first executeTool error = %v, want nil", err)
	}

	var rejected Observation
	for _, observation := range recorder.Observations() {
		if observation.Type == EventAfterTool && observation.Failed {
			rejected = observation
			break
		}
	}
	if rejected.ToolSafety.MaxConcurrency != 1 {
		t.Fatalf("rejected tool safety metadata = %#v, want max concurrency 1", rejected.ToolSafety)
	}
}

func TestToolSafetyScopesAndBusinessReasonReachApprovalWithoutTelemetryLeak(t *testing.T) {
	ctx := context.Background()
	const (
		scopeSecret          = "/tenant/acme/private-ledger"
		businessReasonSecret = "customer-change-ticket-secret"
	)
	recorder := &recordingObserver{}
	var gotRequest ApprovalRequest
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "write_ledger"}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	bot, err := New(Config{ID: "tool-scope-agent", SystemPrompt: "base"}, model,
		WithObserver(recorder),
		WithTools(ToolFunc{
			ToolName: "write_ledger",
			Safety: ToolSafety{
				Risk: ToolRiskWrite,
				Scopes: []ToolScope{{
					Kind:  "filesystem",
					Value: scopeSecret,
				}},
				BusinessReason: businessReasonSecret,
			},
			Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
				return ToolResult{CallID: call.ID, Name: call.Name, Content: "ok"}, nil
			},
		}),
		WithApprovalPolicy(ApprovalFunc(func(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
			gotRequest = request
			if len(request.ToolSafety.Scopes) != 1 || request.ToolSafety.Scopes[0].Value != scopeSecret {
				return ApprovalDecision{Approved: false, Reason: "scope mismatch"}, nil
			}
			if request.ToolSafety.BusinessReason != businessReasonSecret {
				return ApprovalDecision{Approved: false, Reason: "business reason mismatch"}, nil
			}
			return ApprovalDecision{Approved: true, Reason: "scoped approval granted"}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, "write ledger"); err != nil {
		t.Fatal(err)
	}
	if gotRequest.ToolName != "write_ledger" || gotRequest.Risk != ToolRiskWrite {
		t.Fatalf("approval request = %#v, want write ledger request with write risk", gotRequest)
	}

	for _, observation := range recorder.Observations() {
		assertObservationDoesNotContain(t, observation, scopeSecret)
		assertObservationDoesNotContain(t, observation, businessReasonSecret)
	}
	beforeApproval := firstObservationOfType(t, recorder.Observations(), EventBeforeApproval)
	if beforeApproval.ToolSafety.ScopeCount != 1 || beforeApproval.ToolSafety.ScopeHash == "" {
		t.Fatalf("before approval tool safety = %#v, want scope count and hash", beforeApproval.ToolSafety)
	}
	if beforeApproval.ToolSafety.BusinessReasonHash == "" {
		t.Fatalf("before approval business reason hash = empty, want hash")
	}
}

func TestToolWithSafetyWrapsMCPAndOtherTools(t *testing.T) {
	wrapped := ToolWithSafety(ToolFunc{
		ToolName:        "remote_delete",
		ToolDescription: "Delete through a remote bridge",
		Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			return ToolResult{CallID: call.ID, Name: call.Name, Content: "ok"}, nil
		},
	}, ToolSafety{
		Risk:           ToolRiskDestructive,
		Timeout:        time.Second,
		MaxConcurrency: 2,
		MaxResultBytes: 64,
		Scopes:         []ToolScope{{Kind: "mcp-server", Value: "prod-filesystem"}},
	})

	if wrapped.Name() != "remote_delete" || wrapped.Description() != "Delete through a remote bridge" {
		t.Fatalf("wrapped tool metadata = %q/%q, want underlying metadata", wrapped.Name(), wrapped.Description())
	}
	if toolRisk(wrapped) != ToolRiskDestructive {
		t.Fatalf("wrapped risk = %q, want destructive", toolRisk(wrapped))
	}
	safety := wrapped.(ToolSafetyProvider).ToolSafety()
	if safety.Timeout != time.Second || safety.MaxConcurrency != 2 || safety.MaxResultBytes != 64 || len(safety.Scopes) != 1 {
		t.Fatalf("wrapped safety = %#v, want configured safety", safety)
	}
}

type succeedingModel struct{}

func (succeedingModel) Generate(context.Context, ModelRequest) (ModelResponse, error) {
	return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
}
