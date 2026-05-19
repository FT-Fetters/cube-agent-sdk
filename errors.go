package agent

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCategory groups operational failures so callers can audit and branch
// without relying on provider-specific error text.
type ErrorCategory string

const (
	ErrorCategoryModel     ErrorCategory = "model"
	ErrorCategoryTool      ErrorCategory = "tool"
	ErrorCategoryApproval  ErrorCategory = "approval"
	ErrorCategorySchema    ErrorCategory = "schema"
	ErrorCategoryMCP       ErrorCategory = "mcp"
	ErrorCategoryCompact   ErrorCategory = "compact"
	ErrorCategorySubagent  ErrorCategory = "subagent"
	ErrorCategoryStreaming ErrorCategory = "streaming"
	ErrorCategoryHook      ErrorCategory = "hook"
	ErrorCategoryConfig    ErrorCategory = "config"
)

// AgentError adds stable SDK context around an underlying error. Unwrap keeps
// existing sentinel checks such as errors.Is(err, ErrApprovalDenied) working.
type AgentError struct {
	Category   ErrorCategory
	Operation  string
	AgentID    string
	RequestID  string
	ToolName   string
	SubagentID string
	Round      int
	Cause      error
}

func (e *AgentError) Error() string {
	if e == nil {
		return ""
	}
	var parts []string
	if e.Category != "" {
		parts = append(parts, string(e.Category))
	}
	if e.Operation != "" {
		parts = append(parts, e.Operation)
	}
	if len(parts) == 0 {
		parts = append(parts, "operation")
	}
	message := "agent: " + strings.Join(parts, " ")
	if e.Cause == nil {
		return message
	}
	return fmt.Sprintf("%s: %v", message, e.Cause)
}

func (e *AgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

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
