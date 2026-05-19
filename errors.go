package agent

import (
	"errors"
)

func classifyError(err error) ErrorCategory {
	if err == nil {
		return ""
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr.Category != "" {
		return agentErr.Category
	}
	switch {
	case errors.Is(err, ErrApprovalDenied):
		return ErrorCategoryApproval
	case errors.Is(err, ErrToolValidation):
		return ErrorCategorySchema
	case errors.Is(err, ErrMCPRPC), errors.Is(err, ErrMCPProcessExited), errors.Is(err, ErrMCPToolNotFound):
		return ErrorCategoryMCP
	case errors.Is(err, ErrSubagentNotFound):
		return ErrorCategorySubagent
	case errors.Is(err, ErrStreamingUnsupported), errors.Is(err, ErrStreamingToolCallsUnsupported):
		return ErrorCategoryStreaming
	case errors.Is(err, ErrToolNotFound), errors.Is(err, ErrMaxToolRoundsExceeded):
		return ErrorCategoryTool
	default:
		return ""
	}
}

func agentError(category ErrorCategory, operation string, cause error) *AgentError {
	if cause == nil {
		return nil
	}
	return &AgentError{
		Category:  category,
		Operation: operation,
		Cause:     cause,
	}
}
