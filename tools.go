package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrApprovalDenied        = errors.New("agent: approval denied")
	ErrMaxToolRoundsExceeded = errors.New("agent: max tool rounds exceeded")
	ErrToolNotFound          = errors.New("agent: tool not found")
)

func (a *Agent) executeTool(ctx context.Context, call ToolCall, round int, parentRequestID string) (ToolResult, error) {
	requestID := a.nextRequestID(ctx, RequestIDContext{
		EventType:       EventBeforeApproval,
		Operation:       requestOperationToolCall,
		Round:           round,
		ParentRequestID: parentRequestID,
		ToolName:        call.Name,
	})
	started := time.Now()
	timing := ToolLifecycleTiming{}
	estimatedTokens := a.estimatedToolCallTokens(call, ToolResult{})

	a.mu.Lock()
	tool := a.tools[call.Name]
	approval := a.approval
	agentID := a.id
	a.mu.Unlock()
	if tool == nil {
		cause := fmt.Errorf("%w: %s", ErrToolNotFound, call.Name)
		wrapped := agentError(ErrorCategoryTool, "tool.lookup", cause)
		wrapped.AgentID = agentID
		wrapped.ToolName = call.Name
		wrapped.RequestID = requestID
		wrapped.ParentRequestID = parentRequestID
		wrapped.Round = round
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, ToolRiskUnspecified, "", timing, wrapped)
	}
	risk := toolRisk(tool)
	parameters := toolParametersSchema(tool)
	toolSchemaHash := toolSchemaHashForTool(tool, parameters, risk)

	validationStarted := time.Now()
	if err := validateToolCallArguments(call.Name, call.Arguments, parameters); err != nil {
		timing.Validation = eventDurationSince(validationStarted)
		wrapped := agentError(ErrorCategorySchema, "tool.validate", err)
		wrapped.AgentID = agentID
		wrapped.ToolName = call.Name
		wrapped.RequestID = requestID
		wrapped.ParentRequestID = parentRequestID
		wrapped.Round = round
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, risk, toolSchemaHash, timing, wrapped)
	}
	timing.Validation = eventDurationSince(validationStarted)

	if err := a.emit(ctx, Event{
		Type:            EventBeforeApproval,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		EstimatedTokens: estimatedTokens,
		ToolCall:        approvalEventToolCall(call),
	}); err != nil {
		return ToolResult{}, err
	}
	approvalStarted := time.Now()
	decision, err := approval.ApproveTool(ctx, ApprovalRequest{
		AgentID:  agentID,
		ToolName: call.Name,
		Risk:     risk,
		ToolCall: cloneToolCall(call),
	})
	timing.Approval = eventDurationSince(approvalStarted)
	if err != nil {
		wrapped := agentError(ErrorCategoryApproval, "tool.approval", err)
		wrapped.AgentID = agentID
		wrapped.ToolName = call.Name
		wrapped.RequestID = requestID
		wrapped.ParentRequestID = parentRequestID
		wrapped.Round = round
		if emitErr := a.emit(ctx, Event{
			Type:            EventAfterApproval,
			ToolName:        call.Name,
			ToolRisk:        risk,
			ToolSchemaHash:  toolSchemaHash,
			RequestID:       requestID,
			ParentRequestID: parentRequestID,
			Round:           round,
			Duration:        timing.Approval,
			EstimatedTokens: estimatedTokens,
			Approved:        decision.Approved,
			ApprovalReason:  decision.Reason,
			ToolCall:        approvalEventToolCall(call),
			Error:           wrapped,
		}); emitErr != nil {
			return ToolResult{}, emitErr
		}
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, risk, toolSchemaHash, timing, wrapped)
	}
	decision = normalizeApprovalDecision(decision)
	if !decision.Approved {
		cause := fmt.Errorf("%w: %s", ErrApprovalDenied, decision.Reason)
		wrapped := agentError(ErrorCategoryApproval, "tool.approval", cause)
		wrapped.AgentID = agentID
		wrapped.ToolName = call.Name
		wrapped.RequestID = requestID
		wrapped.ParentRequestID = parentRequestID
		wrapped.Round = round
		if emitErr := a.emit(ctx, Event{
			Type:            EventAfterApproval,
			ToolName:        call.Name,
			ToolRisk:        risk,
			ToolSchemaHash:  toolSchemaHash,
			RequestID:       requestID,
			ParentRequestID: parentRequestID,
			Round:           round,
			Duration:        timing.Approval,
			EstimatedTokens: estimatedTokens,
			Approved:        false,
			ApprovalReason:  decision.Reason,
			ToolCall:        approvalEventToolCall(call),
			Error:           wrapped,
		}); emitErr != nil {
			return ToolResult{}, emitErr
		}
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, risk, toolSchemaHash, timing, wrapped)
	}
	if err := a.emit(ctx, Event{
		Type:            EventAfterApproval,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		Duration:        timing.Approval,
		EstimatedTokens: estimatedTokens,
		Approved:        true,
		ApprovalReason:  decision.Reason,
		ToolCall:        approvalEventToolCall(call),
	}); err != nil {
		return ToolResult{}, err
	}

	if err := a.emit(ctx, Event{
		Type:            EventBeforeTool,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		EstimatedTokens: estimatedTokens,
		ToolCall:        cloneToolCall(call),
	}); err != nil {
		return ToolResult{}, err
	}
	executionStarted := time.Now()
	result, err := tool.Call(ctx, cloneToolCall(call))
	timing.Execution = eventDurationSince(executionStarted)
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	wrappedErr := err
	if err != nil {
		category := classifyError(err)
		if category == "" {
			category = ErrorCategoryTool
		}
		wrapped := agentError(category, "tool.call", err)
		wrapped.AgentID = agentID
		wrapped.ToolName = call.Name
		wrapped.RequestID = requestID
		wrapped.ParentRequestID = parentRequestID
		wrapped.Round = round
		wrappedErr = wrapped
	}
	afterTokens := a.estimatedToolCallTokens(call, result)
	if emitErr := a.emit(ctx, Event{
		Type:            EventAfterTool,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		Duration:        eventDurationSince(started),
		EstimatedTokens: afterTokens,
		ToolTiming:      timing,
		ToolCall:        cloneToolCall(call),
		ToolResult:      cloneToolResult(result),
		Error:           wrappedErr,
	}); emitErr != nil {
		return ToolResult{}, emitErr
	}
	if err != nil {
		return ToolResult{}, wrappedErr
	}
	return cloneToolResult(result), nil
}

func (a *Agent) failTool(ctx context.Context, call ToolCall, requestID string, parentRequestID string, round int, started time.Time, estimatedTokens int, risk ToolRisk, toolSchemaHash string, timing ToolLifecycleTiming, err error) (ToolResult, error) {
	if emitErr := a.emit(ctx, Event{
		Type:            EventAfterTool,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		Duration:        eventDurationSince(started),
		EstimatedTokens: estimatedTokens,
		ToolTiming:      timing,
		ToolCall:        cloneToolCall(call),
		Error:           err,
	}); emitErr != nil {
		return ToolResult{}, emitErr
	}
	return ToolResult{}, err
}

func (a *Agent) estimatedToolCallTokens(call ToolCall, result ToolResult) int {
	content := call.Name
	if len(call.Arguments) > 0 {
		content += " " + fmt.Sprint(call.Arguments)
	}
	if result.Content != "" {
		content += " " + result.Content
	}
	return a.estimatedTokens([]Message{{Role: RoleAssistant, Content: content}})
}

func cloneToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	cloned := make([]ToolCall, len(calls))
	for i, call := range calls {
		cloned[i] = cloneToolCall(call)
	}
	return cloned
}

func cloneToolCall(call ToolCall) ToolCall {
	call.Arguments = cloneAnyMap(call.Arguments)
	return call
}

func approvalEventToolCall(call ToolCall) ToolCall {
	return ToolCall{ID: call.ID, Name: call.Name}
}

func toolRisk(tool Tool) ToolRisk {
	provider, ok := tool.(ToolRiskProvider)
	if !ok {
		return ToolRiskUnspecified
	}
	return provider.Risk()
}

func cloneToolResult(result ToolResult) ToolResult {
	result.Metadata = cloneAnyMap(result.Metadata)
	return result
}
