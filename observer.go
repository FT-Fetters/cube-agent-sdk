package agent

import (
	"context"
	"time"
)

// Observer receives sanitized lifecycle metadata for production telemetry.
type Observer interface {
	Observe(context.Context, Observation)
}

// ObserverFunc adapts a function into an Observer.
type ObserverFunc func(context.Context, Observation)

func (f ObserverFunc) Observe(ctx context.Context, observation Observation) {
	if f != nil {
		f(ctx, observation)
	}
}

// NoopObserver drops every observation without output or dependencies.
type NoopObserver struct{}

func (NoopObserver) Observe(context.Context, Observation) {}

// Observation is a safe telemetry view of an Event. It intentionally omits
// message content, tool arguments, tool results, raw errors, and MCP settings.
type Observation struct {
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
	Failed          bool
}

// ObservationFromEvent converts a lifecycle event into safe telemetry metadata.
func ObservationFromEvent(event Event) Observation {
	return Observation{
		Type:            event.Type,
		AgentID:         event.AgentID,
		SubagentID:      event.SubagentID,
		ToolName:        event.ToolName,
		ToolRisk:        event.ToolRisk,
		SkillName:       event.SkillName,
		RequestID:       event.RequestID,
		Round:           event.Round,
		Duration:        event.Duration,
		EstimatedTokens: event.EstimatedTokens,
		Approved:        event.Approved,
		ApprovalReason:  event.ApprovalReason,
		ErrorCategory:   event.ErrorCategory,
		Failed:          event.Error != nil || event.ErrorCategory != "",
	}
}

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	if observer == nil {
		return
	}
	observation := ObservationFromEvent(event)
	// Telemetry is best-effort and must not change agent behavior.
	defer func() {
		_ = recover()
	}()
	observer.Observe(ctx, observation)
}
