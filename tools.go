package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cubence/cube-agent-sdk/internal/core"
)

var (
	ErrApprovalDenied               = errors.New("agent: approval denied")
	ErrMaxToolRoundsExceeded        = errors.New("agent: max tool rounds exceeded")
	ErrToolNotFound                 = errors.New("agent: tool not found")
	ErrToolConcurrencyLimitExceeded = errors.New("agent: tool concurrency limit exceeded")
	ErrToolResultTooLarge           = errors.New("agent: tool result too large")
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
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, ToolRiskUnspecified, "", ToolSafetyMetadata{}, timing, wrapped)
	}
	safety := toolSafety(tool)
	risk := safety.Risk
	safetyMetadata := toolSafetyMetadata(safety)
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
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, risk, toolSchemaHash, safetyMetadata, timing, wrapped)
	}
	timing.Validation = eventDurationSince(validationStarted)

	if err := a.emit(ctx, Event{
		Type:            EventBeforeApproval,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		ToolSafety:      safetyMetadata,
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
		AgentID:    agentID,
		ToolName:   call.Name,
		Risk:       risk,
		ToolSafety: cloneToolSafety(safety),
		ToolCall:   cloneToolCall(call),
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
			ToolSafety:      safetyMetadata,
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
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, risk, toolSchemaHash, safetyMetadata, timing, wrapped)
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
			ToolSafety:      safetyMetadata,
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
		return a.failTool(ctx, call, requestID, parentRequestID, round, started, estimatedTokens, risk, toolSchemaHash, safetyMetadata, timing, wrapped)
	}
	if err := a.emit(ctx, Event{
		Type:            EventAfterApproval,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		ToolSafety:      safetyMetadata,
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
		ToolSafety:      safetyMetadata,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		EstimatedTokens: estimatedTokens,
		ToolCall:        cloneToolCall(call),
	}); err != nil {
		return ToolResult{}, err
	}
	executionStarted := time.Now()
	result, err := a.callTool(ctx, tool, cloneToolCall(call), safety)
	timing.Execution = eventDurationSince(executionStarted)
	if result.CallID == "" {
		result.CallID = call.ID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	if err == nil {
		err = validateToolResultSize(result, safety)
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
		ToolSafety:      safetyMetadata,
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

func (a *Agent) failTool(ctx context.Context, call ToolCall, requestID string, parentRequestID string, round int, started time.Time, estimatedTokens int, risk ToolRisk, toolSchemaHash string, safetyMetadata ToolSafetyMetadata, timing ToolLifecycleTiming, err error) (ToolResult, error) {
	if emitErr := a.emit(ctx, Event{
		Type:            EventAfterTool,
		ToolName:        call.Name,
		ToolRisk:        risk,
		ToolSchemaHash:  toolSchemaHash,
		ToolSafety:      safetyMetadata,
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

// toolErrorFeedbackResult turns model-correctable tool failures into a normal
// tool message. Approval and hook failures remain terminal security/runtime errors.
func toolErrorFeedbackResult(call ToolCall, err error) (ToolResult, bool) {
	if err == nil {
		return ToolResult{}, false
	}
	category := classifyError(err)
	switch category {
	case ErrorCategoryApproval, ErrorCategoryHook:
		return ToolResult{}, false
	}
	return ToolResult{
		CallID:  call.ID,
		Name:    call.Name,
		Content: toolErrorFeedbackContent(err),
	}, true
}

func toolErrorFeedbackContent(err error) string {
	return fmt.Sprintf("Tool call failed: %v. Fix the arguments or choose another approach.", err)
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

type toolCallOutcome struct {
	result ToolResult
	err    error
}

func (a *Agent) callTool(ctx context.Context, tool Tool, call ToolCall, safety ToolSafety) (ToolResult, error) {
	release, err := a.acquireToolSlot(call.Name, safety.MaxConcurrency)
	if err != nil {
		return ToolResult{}, err
	}
	if safety.Timeout <= 0 {
		defer release()
		return tool.Call(ctx, call)
	}

	toolCtx, cancel := context.WithTimeout(ctx, safety.Timeout)
	defer cancel()
	done := make(chan toolCallOutcome, 1)
	go func() {
		defer release()
		result, err := tool.Call(toolCtx, call)
		done <- toolCallOutcome{result: result, err: err}
	}()

	select {
	case outcome := <-done:
		return outcome.result, outcome.err
	case <-toolCtx.Done():
		return ToolResult{}, toolCtx.Err()
	}
}

func (a *Agent) acquireToolSlot(name string, maxConcurrency int) (func(), error) {
	if maxConcurrency <= 0 {
		return func() {}, nil
	}
	a.mu.Lock()
	if a.toolConcurrency == nil {
		a.toolConcurrency = make(map[string]int)
	}
	if a.toolConcurrency[name] >= maxConcurrency {
		a.mu.Unlock()
		return nil, fmt.Errorf("%w: max concurrency %d", ErrToolConcurrencyLimitExceeded, maxConcurrency)
	}
	a.toolConcurrency[name]++
	a.mu.Unlock()

	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.toolConcurrency[name] <= 1 {
			delete(a.toolConcurrency, name)
			return
		}
		a.toolConcurrency[name]--
	}, nil
}

func validateToolResultSize(result ToolResult, safety ToolSafety) error {
	if safety.MaxResultBytes <= 0 {
		return nil
	}
	contentBytes := len(result.Content)
	if contentBytes <= safety.MaxResultBytes {
		return nil
	}
	return fmt.Errorf("%w: content bytes %d exceeds max %d", ErrToolResultTooLarge, contentBytes, safety.MaxResultBytes)
}

func toolSafety(tool Tool) ToolSafety {
	if tool == nil {
		return ToolSafety{}
	}
	safety := ToolSafety{}
	if provider, ok := tool.(ToolSafetyProvider); ok {
		safety = provider.ToolSafety()
	}
	if safety.Risk == "" {
		if provider, ok := tool.(ToolRiskProvider); ok {
			safety.Risk = provider.Risk()
		}
	}
	return cloneToolSafety(safety)
}

func toolRisk(tool Tool) ToolRisk {
	return toolSafety(tool).Risk
}

func cloneToolSafety(safety ToolSafety) ToolSafety {
	return core.CloneToolSafety(safety)
}

func toolSafetyMetadata(safety ToolSafety) ToolSafetyMetadata {
	return core.ToolSafetyMetadataFromSafety(safety)
}

func cloneToolResult(result ToolResult) ToolResult {
	result.Metadata = cloneAnyMap(result.Metadata)
	return result
}
