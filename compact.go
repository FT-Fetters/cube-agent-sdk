package agent

import (
	"context"
	"math"
	"time"
)

func (a *Agent) maybeCompact(ctx context.Context, round int, parentRequestID string) error {
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
		ParentRequestID: parentRequestID,
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
		wrapped.ParentRequestID = parentRequestID
		wrapped.Round = round
		if emitErr := a.emit(ctx, Event{
			Type:            EventAfterCompact,
			RequestID:       requestID,
			ParentRequestID: parentRequestID,
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
		ParentRequestID: parentRequestID,
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
