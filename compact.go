package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// CompactConfig controls when and how the agent compacts conversation context.
type CompactConfig struct {
	MaxTokens int
	Threshold float64
	KeepLast  int
}

// TokenCounter estimates token usage for threshold checks.
type TokenCounter interface {
	Count(Message) int
}

// TokenCounterFunc adapts a function into a TokenCounter.
type TokenCounterFunc func(Message) int

func (f TokenCounterFunc) Count(message Message) int {
	return f(message)
}

// ApproxTokenCounter is a dependency-free token estimator suitable for threshold triggers.
type ApproxTokenCounter struct{}

func (ApproxTokenCounter) Count(message Message) int {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return 0
	}
	words := len(strings.Fields(content))
	chars := int(math.Ceil(float64(len(content)) / 4.0))
	if chars > words {
		return chars
	}
	return words
}

// Compactor replaces a long message history with a shorter one.
type Compactor interface {
	Compact(context.Context, []Message) ([]Message, error)
}

// SummaryCompactor creates a deterministic local summary when no model-backed compactor is provided.
type SummaryCompactor struct {
	KeepLast int
}

func (c SummaryCompactor) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	keep := c.KeepLast
	if keep <= 0 {
		keep = 4
	}
	if len(messages) <= keep {
		return cloneMessages(messages), nil
	}
	summary := Message{
		Role:    RoleSystem,
		Content: fmt.Sprintf("Conversation compacted: %d earlier messages were summarized by the SDK.", len(messages)-keep),
	}
	result := []Message{summary}
	result = append(result, cloneMessages(messages[len(messages)-keep:])...)
	return result, nil
}

// ModelCompactor asks a model to summarize older context and keeps recent messages intact.
type ModelCompactor struct {
	Model        Model
	SystemPrompt string
	KeepLast     int
}

func (c ModelCompactor) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	if c.Model == nil {
		return nil, errors.New("agent: compactor model is required")
	}
	keep := c.KeepLast
	if keep <= 0 {
		keep = 4
	}
	if len(messages) <= keep {
		return cloneMessages(messages), nil
	}

	older := cloneMessages(messages[:len(messages)-keep])
	recent := cloneMessages(messages[len(messages)-keep:])
	prompt := strings.TrimSpace(c.SystemPrompt)
	if prompt == "" {
		prompt = "Summarize the conversation context for a future agent turn. Preserve decisions, constraints, open tasks, and user preferences."
	}
	response, err := c.Model.Generate(ctx, ModelRequest{
		SystemPrompt: prompt,
		Messages:     older,
	})
	if err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(response.Message.Content)
	if summary == "" {
		summary = "Earlier context was compacted, but the summarizer returned an empty summary."
	}
	result := []Message{{
		Role:    RoleSystem,
		Content: "Context summary:\n" + summary,
	}}
	result = append(result, recent...)
	return result, nil
}

func (a *Agent) maybeCompact(ctx context.Context, round int) error {
	a.mu.Lock()
	config := a.config.Compact
	if config.MaxTokens <= 0 {
		a.mu.Unlock()
		return nil
	}
	threshold := config.Threshold
	if threshold <= 0 {
		threshold = 0.8
	}
	limit := config.MaxTokens
	if threshold <= 1 {
		limit = int(math.Ceil(float64(config.MaxTokens) * threshold))
	} else {
		limit = int(threshold)
	}
	counter := a.tokenCount
	total := estimateMessagesTokens(counter, a.messages)
	if total < limit {
		a.mu.Unlock()
		return nil
	}
	messages := cloneMessages(a.messages)
	compactor := a.compactor
	a.mu.Unlock()

	if compactor == nil {
		return nil
	}
	requestID := a.nextRequestID()
	if err := a.emit(ctx, Event{
		Type:            EventBeforeCompact,
		RequestID:       requestID,
		Round:           round,
		EstimatedTokens: total,
	}); err != nil {
		return err
	}
	started := time.Now()
	compacted, err := compactor.Compact(ctx, messages)
	duration := eventDurationSince(started)
	if err != nil {
		wrapped := agentError(ErrorCategoryCompact, "compact.context", err)
		wrapped.AgentID = a.agentID()
		wrapped.RequestID = requestID
		wrapped.Round = round
		if emitErr := a.emit(ctx, Event{
			Type:            EventAfterCompact,
			RequestID:       requestID,
			Round:           round,
			Duration:        duration,
			EstimatedTokens: total,
			Error:           wrapped,
		}); emitErr != nil {
			return emitErr
		}
		return wrapped
	}
	a.mu.Lock()
	a.messages = cloneMessages(compacted)
	a.mu.Unlock()
	return a.emit(ctx, Event{
		Type:            EventAfterCompact,
		RequestID:       requestID,
		Round:           round,
		Duration:        duration,
		EstimatedTokens: estimateMessagesTokens(counter, compacted),
	})
}

func estimateMessagesTokens(counter TokenCounter, messages []Message) int {
	if counter == nil {
		return 0
	}
	total := 0
	for _, message := range messages {
		total += counter.Count(message)
	}
	return total
}
