package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cubence/cube-agent-sdk/internal/core"
	providerdiagnostics "github.com/cubence/cube-agent-sdk/internal/providers/diagnostics"
	providersse "github.com/cubence/cube-agent-sdk/internal/providers/sse"
)

var errAnthropicStreamStop = errors.New("anthropic stream stopped")

// Stream sends an Anthropic Messages streaming request and emits text deltas
// from content_block_delta events.
func (m *AnthropicMessagesModel) Stream(ctx context.Context, request ModelRequest) (<-chan core.StreamEvent, error) {
	if m == nil {
		return nil, errors.New("agent: anthropic messages model is nil")
	}

	diagnostics := providerdiagnostics.New(providerAnthropicMessages, m.endpoint)
	payload, err := newAnthropicMessagesRequest(m.model, m.maxTokens, request)
	if err != nil {
		return nil, err
	}
	payload.Stream = true

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, core.NewProviderError("encode anthropic messages stream request", diagnostics, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewProviderError("create anthropic messages stream request", diagnostics, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpRequest.Header.Set("anthropic-version", m.anthropicVersion)
	if m.apiKey != "" {
		httpRequest.Header.Set("x-api-key", m.apiKey)
	}

	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return nil, core.NewProviderTransportError("call anthropic messages stream", diagnostics, err)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		defer httpResponse.Body.Close()
		diagnostics := providerdiagnostics.FromResponse(providerAnthropicMessages, m.endpoint, httpResponse)
		return nil, core.NewProviderError(fmt.Sprintf("anthropic messages stream returned status %d", httpResponse.StatusCode), diagnostics, nil)
	}

	events := make(chan core.StreamEvent)
	go func() {
		defer close(events)
		defer httpResponse.Body.Close()
		streamAnthropicMessagesEvents(ctx, httpResponse.Body, events, diagnostics)
	}()
	return events, nil
}

type anthropicMessagesStreamEvent struct {
	Type         string                         `json:"type"`
	Message      anthropicMessagesStreamMessage `json:"message"`
	ContentBlock anthropicContentBlock          `json:"content_block"`
	Delta        anthropicMessagesStreamDelta   `json:"delta"`
	Usage        anthropicMessagesUsage         `json:"usage"`
	Error        *anthropicMessagesStreamError  `json:"error"`
}

type anthropicMessagesStreamMessage struct {
	Usage anthropicMessagesUsage `json:"usage"`
}

type anthropicMessagesStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	PartialJSON string `json:"partial_json"`
}

type anthropicMessagesStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func streamAnthropicMessagesEvents(ctx context.Context, body io.Reader, events chan<- core.StreamEvent, diagnostics core.ProviderDiagnostics) {
	var content strings.Builder
	var usage anthropicMessagesUsage
	var done bool

	err := providersse.Read(ctx, body, func(event providersse.Event) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		var decoded anthropicMessagesStreamEvent
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			return sendAnthropicStreamError(ctx, events, core.NewProviderDecodeError("decode anthropic messages stream event", diagnostics, err))
		}
		eventType := decoded.Type
		if eventType == "" {
			eventType = event.Name
		}
		switch eventType {
		case "message_start":
			mergeAnthropicStreamUsage(&usage, decoded.Message.Usage)
		case "content_block_start":
			if decoded.ContentBlock.Type == "tool_use" {
				return sendAnthropicStreamError(ctx, events, core.ErrStreamingToolCallsUnsupported)
			}
			if decoded.ContentBlock.Type == "text" && decoded.ContentBlock.Text != "" {
				return sendAnthropicTextDelta(ctx, events, &content, decoded.ContentBlock.Text)
			}
		case "content_block_delta":
			switch decoded.Delta.Type {
			case "text_delta":
				return sendAnthropicTextDelta(ctx, events, &content, decoded.Delta.Text)
			case "input_json_delta":
				return sendAnthropicStreamError(ctx, events, core.ErrStreamingToolCallsUnsupported)
			}
		case "message_delta":
			mergeAnthropicStreamUsage(&usage, decoded.Usage)
		case "message_stop":
			done = true
			return sendAnthropicStreamEvent(ctx, events, core.StreamEvent{
				Type:    core.StreamEventDone,
				Message: core.Message{Role: RoleAssistant, Content: content.String()},
				Usage:   usage.tokenUsage(),
			})
		case "error":
			return sendAnthropicStreamError(ctx, events, core.NewProviderError("anthropic messages stream returned provider error", diagnostics, nil))
		}
		return nil
	})
	if shouldStopAnthropicStream(ctx, err) {
		return
	}
	if err != nil {
		_ = sendAnthropicStreamError(ctx, events, core.NewProviderTransportError("read anthropic messages stream", diagnostics, err))
		return
	}
	if !done {
		_ = sendAnthropicStreamError(ctx, events, core.NewProviderDecodeError("decode anthropic messages stream", diagnostics, errors.New("stream ended before message_stop")))
	}
}

func mergeAnthropicStreamUsage(target *anthropicMessagesUsage, update anthropicMessagesUsage) {
	if update.InputTokens > 0 {
		target.InputTokens = update.InputTokens
	}
	if update.OutputTokens > 0 {
		target.OutputTokens = update.OutputTokens
	}
	if update.TotalTokens > 0 {
		target.TotalTokens = update.TotalTokens
	}
}

func sendAnthropicTextDelta(ctx context.Context, events chan<- core.StreamEvent, content *strings.Builder, delta string) error {
	if delta == "" {
		return nil
	}
	content.WriteString(delta)
	return sendAnthropicStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDelta, Delta: delta})
}

func sendAnthropicStreamEvent(ctx context.Context, events chan<- core.StreamEvent, event core.StreamEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case events <- event:
		if event.Type == core.StreamEventDone {
			return errAnthropicStreamStop
		}
		return nil
	}
}

func sendAnthropicStreamError(ctx context.Context, events chan<- core.StreamEvent, err error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case events <- core.StreamEvent{Type: core.StreamEventError, Error: err}:
		return errAnthropicStreamStop
	}
}

func shouldStopAnthropicStream(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errAnthropicStreamStop) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), err)
}
