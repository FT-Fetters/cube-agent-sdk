package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestApprovalPoliciesAllowDenyAndCompose(t *testing.T) {
	ctx := context.Background()
	readRequest := ApprovalRequest{
		ToolName: "lookup",
		ToolCall: ToolCall{Name: "lookup"},
		Risk:     ToolRiskRead,
	}
	writeRequest := ApprovalRequest{
		ToolName: "write_file",
		ToolCall: ToolCall{Name: "write_file"},
		Risk:     ToolRiskWrite,
	}
	deleteRequest := ApprovalRequest{
		ToolName: "delete_file",
		ToolCall: ToolCall{Name: "delete_file"},
		Risk:     ToolRiskDestructive,
	}

	assertApprovalDecision(t, DenyAllApproval{}, ctx, readRequest, false, "default")
	assertApprovalDecision(t, AllowToolsApproval("lookup"), ctx, readRequest, true, "allowlist")
	assertApprovalDecision(t, AllowToolsApproval("lookup"), ctx, writeRequest, false, "allowlist")
	assertApprovalDecision(t, DenyToolsApproval("delete_file"), ctx, deleteRequest, false, "denylist")
	assertApprovalDecision(t, DenyToolsApproval("delete_file"), ctx, readRequest, true, "denylist")

	readOnlyAllowlist := RequireAllApprovals(
		AllowToolsApproval("lookup", "write_file"),
		AllowRisksApproval(ToolRiskRead),
	)
	assertApprovalDecision(t, readOnlyAllowlist, ctx, readRequest, true, "allowed")
	assertApprovalDecision(t, readOnlyAllowlist, ctx, writeRequest, false, "risk")

	reasonedDeny := RequireAllApprovals(
		AllowToolsApproval("lookup"),
		ApprovalFunc(func(context.Context, ApprovalRequest) (ApprovalDecision, error) {
			return ApprovalDecision{Approved: false, Reason: "business-hours-only"}, nil
		}),
	)
	decision, err := reasonedDeny.ApproveTool(ctx, readRequest)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Approved || decision.Reason != "business-hours-only" {
		t.Fatalf("decision = %#v, want custom denial reason to pass through", decision)
	}
}

func TestAgentApprovalMetadataAndRequestRisk(t *testing.T) {
	ctx := context.Background()
	const secret = "do-not-record-this-argument"
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "write_file", Arguments: map[string]any{"content": secret}}}},
	}}
	var called bool
	var gotRequest ApprovalRequest
	var events []Event
	recorder := &recordingObserver{}
	bot, err := New(Config{ID: "approval-agent", SystemPrompt: "base"}, model,
		WithObserver(recorder),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
		WithTools(ToolFunc{
			ToolName:        "write_file",
			ToolDescription: "Write a file",
			ToolRisk:        ToolRiskWrite,
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				called = true
				return ToolResult{}, nil
			},
		}),
		WithApprovalPolicy(RequireAllApprovals(
			ApprovalFunc(func(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
				gotRequest = request
				return ApprovalDecision{Approved: true, Reason: "captured"}, nil
			}),
			AllowToolsApproval("read_file"),
		)),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(ctx, "write file")
	if !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("err = %v, want ErrApprovalDenied", err)
	}
	if called {
		t.Fatal("tool executed after approval denial")
	}
	if gotRequest.ToolName != "write_file" || gotRequest.Risk != ToolRiskWrite {
		t.Fatalf("approval request = %#v, want tool name and write risk", gotRequest)
	}
	if len(model.requests) == 0 || len(model.requests[0].Tools) != 1 || model.requests[0].Tools[0].Risk != ToolRiskWrite {
		t.Fatalf("model tools = %#v, want descriptor with write risk", model.requests)
	}

	afterApproval := firstEventOfType(t, events, EventAfterApproval)
	if afterApproval.Approved || afterApproval.ApprovalReason == "" || !strings.Contains(afterApproval.ApprovalReason, "allowlist") {
		t.Fatalf("after approval event = %#v, want denied approval reason", afterApproval)
	}
	if afterApproval.ToolRisk != ToolRiskWrite {
		t.Fatalf("after approval risk = %q, want write", afterApproval.ToolRisk)
	}
	if afterApproval.ToolCall.Arguments != nil || strings.Contains(afterApproval.Error.Error(), secret) {
		t.Fatalf("after approval event leaked sensitive approval arguments: %#v", afterApproval)
	}

	afterApprovalObservation := firstObservationOfType(t, recorder.Observations(), EventAfterApproval)
	if afterApprovalObservation.Approved || afterApprovalObservation.ApprovalReason != afterApproval.ApprovalReason {
		t.Fatalf("after approval observation = %#v, want denied approval reason", afterApprovalObservation)
	}
	if afterApprovalObservation.ToolRisk != ToolRiskWrite || !afterApprovalObservation.Failed || afterApprovalObservation.ErrorCategory != ErrorCategoryApproval {
		t.Fatalf("after approval observation = %#v, want write risk and approval failure", afterApprovalObservation)
	}
	assertObservationDoesNotContain(t, afterApprovalObservation, secret)
}

func assertApprovalDecision(t *testing.T, policy ApprovalPolicy, ctx context.Context, request ApprovalRequest, wantApproved bool, wantReason string) {
	t.Helper()
	decision, err := policy.ApproveTool(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Approved != wantApproved || !strings.Contains(decision.Reason, wantReason) {
		t.Fatalf("decision = %#v, want approved %v and reason containing %q", decision, wantApproved, wantReason)
	}
}
