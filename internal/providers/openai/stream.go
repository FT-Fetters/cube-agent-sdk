package openai

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

var errOpenAIStreamStop = errors.New("openai stream stopped")

// Stream sends a chat completions streaming request and emits text deltas as
// provider SSE chunks arrive. Usage is requested with the final usage chunk.
func (m *OpenAICompatibleModel) Stream(ctx context.Context, request ModelRequest) (<-chan core.StreamEvent, error) {
	if m == nil {
		return nil, errors.New("agent: openai-compatible model is nil")
	}

	diagnostics := providerdiagnostics.New(providerOpenAICompatible, m.endpoint)
	payload, err := newOpenAIChatCompletionRequest(m.model, request)
	if err != nil {
		return nil, err
	}
	payload.Stream = true
	payload.StreamOptions = &openAIChatCompletionStreamOptions{IncludeUsage: true}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, core.NewProviderError("encode openai-compatible stream request", diagnostics, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewProviderError("create openai-compatible stream request", diagnostics, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if m.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return nil, core.NewProviderTransportError("call openai-compatible chat completions stream", diagnostics, err)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		defer httpResponse.Body.Close()
		diagnostics := providerdiagnostics.FromResponse(providerOpenAICompatible, m.endpoint, httpResponse)
		return nil, core.NewProviderError(fmt.Sprintf("openai-compatible chat completions stream returned status %d", httpResponse.StatusCode), diagnostics, nil)
	}

	events := make(chan core.StreamEvent)
	go func() {
		defer close(events)
		defer httpResponse.Body.Close()
		streamOpenAICompatibleEvents(ctx, httpResponse.Body, events, diagnostics)
	}()
	return events, nil
}

// Stream sends a Responses API streaming request and emits text deltas from
// semantic response.output_text.delta events.
func (m *OpenAIResponsesModel) Stream(ctx context.Context, request ModelRequest) (<-chan core.StreamEvent, error) {
	if m == nil {
		return nil, errors.New("agent: openai responses model is nil")
	}

	diagnostics := providerdiagnostics.New(providerOpenAIResponses, m.endpoint)
	payload, err := newOpenAIResponsesRequest(m.model, m.maxTokens, m.store, request)
	if err != nil {
		return nil, err
	}
	payload.Stream = true

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, core.NewProviderError("encode openai responses stream request", diagnostics, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewProviderError("create openai responses stream request", diagnostics, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if m.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return nil, core.NewProviderTransportError("call openai responses stream", diagnostics, err)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		defer httpResponse.Body.Close()
		diagnostics := providerdiagnostics.FromResponse(providerOpenAIResponses, m.endpoint, httpResponse)
		return nil, core.NewProviderError(fmt.Sprintf("openai responses stream returned status %d", httpResponse.StatusCode), diagnostics, nil)
	}

	events := make(chan core.StreamEvent)
	go func() {
		defer close(events)
		defer httpResponse.Body.Close()
		streamOpenAIResponseEvents(ctx, httpResponse.Body, events, diagnostics)
	}()
	return events, nil
}

type openAIChatCompletionStreamChunk struct {
	Choices []openAIChatCompletionStreamChoice `json:"choices"`
	Usage   *openAIChatCompletionUsage         `json:"usage"`
	Error   *openAIStreamError                 `json:"error"`
}

type openAIChatCompletionStreamChoice struct {
	Delta        openAIChatCompletionStreamDelta `json:"delta"`
	FinishReason string                          `json:"finish_reason"`
}

type openAIChatCompletionStreamDelta struct {
	Role      string               `json:"role"`
	Content   *string              `json:"content"`
	ToolCalls []openAIChatToolCall `json:"tool_calls"`
}

type openAIResponsesStreamEvent struct {
	Type     string                  `json:"type"`
	Delta    string                  `json:"delta"`
	Text     string                  `json:"text"`
	Response openAIResponsesResponse `json:"response"`
	Error    *openAIStreamError      `json:"error"`
}

type openAIStreamError struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

func streamOpenAICompatibleEvents(ctx context.Context, body io.Reader, events chan<- core.StreamEvent, diagnostics core.ProviderDiagnostics) {
	var content strings.Builder
	var usage core.TokenUsage
	var done bool

	err := providersse.Read(ctx, body, func(event providersse.Event) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			done = true
			return sendOpenAIStreamEvent(ctx, events, core.StreamEvent{
				Type:    core.StreamEventDone,
				Message: core.Message{Role: RoleAssistant, Content: content.String()},
				Usage:   usage,
			})
		}

		var chunk openAIChatCompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai-compatible stream event", diagnostics, err))
		}
		if chunk.Error != nil {
			return sendOpenAIStreamError(ctx, events, core.NewProviderError("openai-compatible stream returned provider error", diagnostics, nil))
		}
		if chunk.Usage != nil {
			usage = chunk.Usage.tokenUsage()
		}
		for _, choice := range chunk.Choices {
			if len(choice.Delta.ToolCalls) > 0 || choice.FinishReason == "tool_calls" {
				return sendOpenAIStreamError(ctx, events, core.ErrStreamingToolCallsUnsupported)
			}
			if choice.Delta.Content == nil || *choice.Delta.Content == "" {
				continue
			}
			delta := *choice.Delta.Content
			content.WriteString(delta)
			if err := sendOpenAIStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDelta, Delta: delta}); err != nil {
				return err
			}
		}
		return nil
	})
	if shouldStopOpenAIStream(ctx, err) {
		return
	}
	if err != nil {
		_ = sendOpenAIStreamError(ctx, events, core.NewProviderTransportError("read openai-compatible stream", diagnostics, err))
		return
	}
	if !done {
		_ = sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai-compatible stream", diagnostics, errors.New("stream ended before done marker")))
	}
}

func streamOpenAIResponseEvents(ctx context.Context, body io.Reader, events chan<- core.StreamEvent, diagnostics core.ProviderDiagnostics) {
	var content strings.Builder
	var finalText string
	var done bool

	err := providersse.Read(ctx, body, func(event providersse.Event) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		var decoded openAIResponsesStreamEvent
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			return sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai responses stream event", diagnostics, err))
		}
		eventType := decoded.Type
		if eventType == "" {
			eventType = event.Name
		}
		switch eventType {
		case "response.output_text.delta":
			if decoded.Delta == "" {
				return nil
			}
			content.WriteString(decoded.Delta)
			return sendOpenAIStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDelta, Delta: decoded.Delta})
		case "response.output_text.done":
			finalText = decoded.Text
		case "response.completed":
			response, err := decoded.Response.modelResponse()
			if err != nil {
				text := finalText
				if text == "" {
					text = content.String()
				}
				if text == "" {
					return sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai responses completed event", diagnostics, err))
				}
				response = ModelResponse{
					Message: Message{Role: RoleAssistant, Content: text},
					Usage:   decoded.Response.Usage.tokenUsage(),
				}
			}
			if response.Message.Content == "" {
				response.Message.Content = content.String()
			}
			done = true
			return sendOpenAIStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDone, Message: response.Message, Usage: response.Usage})
		case "error", "response.failed", "response.incomplete":
			return sendOpenAIStreamError(ctx, events, core.NewProviderError("openai responses stream returned provider error", diagnostics, nil))
		}
		return nil
	})
	if shouldStopOpenAIStream(ctx, err) {
		return
	}
	if err != nil {
		_ = sendOpenAIStreamError(ctx, events, core.NewProviderTransportError("read openai responses stream", diagnostics, err))
		return
	}
	if !done {
		_ = sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai responses stream", diagnostics, errors.New("stream ended before response.completed")))
	}
}

func sendOpenAIStreamEvent(ctx context.Context, events chan<- core.StreamEvent, event core.StreamEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case events <- event:
		if event.Type == core.StreamEventDone {
			return errOpenAIStreamStop
		}
		return nil
	}
}

func sendOpenAIStreamError(ctx context.Context, events chan<- core.StreamEvent, err error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case events <- core.StreamEvent{Type: core.StreamEventError, Error: err}:
		return errOpenAIStreamStop
	}
}

func shouldStopOpenAIStream(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errOpenAIStreamStop) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), err)
}
