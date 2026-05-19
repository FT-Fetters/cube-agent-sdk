package agent

import (
	"context"
	"time"
)

// EventType identifies a lifecycle hook event.
type EventType string

const (
	EventBeforeModel     EventType = "before_model"
	EventAfterModel      EventType = "after_model"
	EventBeforeApproval  EventType = "before_approval"
	EventAfterApproval   EventType = "after_approval"
	EventBeforeTool      EventType = "before_tool"
	EventAfterTool       EventType = "after_tool"
	EventBeforeCompact   EventType = "before_compact"
	EventAfterCompact    EventType = "after_compact"
	EventSkillActivated  EventType = "skill_activated"
	EventSubagentMessage EventType = "subagent_message"
)

// Event carries lifecycle data to hooks.
type Event struct {
	Type            EventType
	AgentID         string
	SubagentID      string
	ToolName        string
	ToolRisk        ToolRisk
	SkillName       string
	RequestID       string
	Round           int
	Duration        time.Duration
	EstimatedTokens int
	Approved        bool
	ApprovalReason  string
	ErrorCategory   ErrorCategory
	Message         Message
	ToolCall        ToolCall
	ToolResult      ToolResult
	Error           error
}

// Hook observes or rejects lifecycle events.
type Hook func(context.Context, Event) error
