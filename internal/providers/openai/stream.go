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
	Type        string                    `json:"type"`
	Delta       string                    `json:"delta"`
	Text        string                    `json:"text"`
	OutputIndex int                       `json:"output_index"`
	ItemID      string                    `json:"item_id"`
	Arguments   string                    `json:"arguments"`
	Item        openAIResponsesOutputItem `json:"item"`
	Response    openAIResponsesResponse   `json:"response"`
	Error       *openAIStreamError        `json:"error"`
}

type openAIStreamError struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

func streamOpenAICompatibleEvents(ctx context.Context, body io.Reader, events chan<- core.StreamEvent, diagnostics core.ProviderDiagnostics) {
	var content strings.Builder
	var toolCalls openAICompatibleStreamToolCalls
	var usage core.TokenUsage
	var finish core.StreamFinishMetadata
	var boundaryEvents []core.StreamEvent
	var done bool

	err := providersse.Read(ctx, body, func(event providersse.Event) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			boundaryEvents = append(boundaryEvents, toolCalls.doneEvents()...)
			calls, err := toolCalls.toolCalls()
			if err != nil {
				return sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai-compatible streamed tool call arguments", diagnostics, err))
			}
			boundaryEvents = toolCalls.finalBoundaryEvents(boundaryEvents)
			if err := sendOpenAIStreamEvents(ctx, events, boundaryEvents); err != nil {
				return err
			}
			done = true
			return sendOpenAIStreamEvent(ctx, events, core.StreamEvent{
				Type: core.StreamEventDone,
				Message: core.Message{
					Role:      RoleAssistant,
					Content:   content.String(),
					ToolCalls: core.CloneToolCalls(calls),
				},
				Usage:  usage,
				Finish: finish,
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
			if len(choice.Delta.ToolCalls) > 0 {
				boundaryEvents = append(boundaryEvents, toolCalls.add(choice.Delta.ToolCalls)...)
			}
			if choice.FinishReason != "" {
				finish.Reason = choice.FinishReason
				boundaryEvents = append(boundaryEvents, toolCalls.doneEvents()...)
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

// openAICompatibleStreamToolCalls reconstructs tool call deltas by index. Some
// OpenAI-compatible providers stream IDs and names before argument fragments.
type openAICompatibleStreamToolCalls struct {
	calls   map[int]*openAICompatibleStreamToolCall
	order   []int
	started map[int]bool
	done    map[int]bool
}

type openAICompatibleStreamToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func (c *openAICompatibleStreamToolCalls) add(deltas []openAIChatToolCall) []core.StreamEvent {
	var events []core.StreamEvent
	for _, delta := range deltas {
		call := c.ensure(delta.Index)
		call.id = appendOpenAIStreamValue(call.id, delta.ID)
		call.name = appendOpenAIStreamValue(call.name, delta.Function.Name)
		call.arguments.WriteString(delta.Function.Arguments)
		if !c.started[delta.Index] {
			c.started[delta.Index] = true
			events = append(events, openAIStreamToolCallBoundary(core.StreamEventToolCallStart, delta.Index, call.id, call.name))
		}
	}
	return events
}

func (c *openAICompatibleStreamToolCalls) ensure(index int) *openAICompatibleStreamToolCall {
	if c.calls == nil {
		c.calls = make(map[int]*openAICompatibleStreamToolCall)
		c.started = make(map[int]bool)
		c.done = make(map[int]bool)
	}
	call := c.calls[index]
	if call == nil {
		call = &openAICompatibleStreamToolCall{}
		c.calls[index] = call
		c.order = append(c.order, index)
	}
	return call
}

func (c *openAICompatibleStreamToolCalls) doneEvents() []core.StreamEvent {
	if len(c.order) == 0 {
		return nil
	}
	events := make([]core.StreamEvent, 0, len(c.order))
	for _, index := range c.order {
		if c.done[index] {
			continue
		}
		c.done[index] = true
		call := c.calls[index]
		events = append(events, openAIStreamToolCallBoundary(core.StreamEventToolCallDone, index, call.id, call.name))
	}
	return events
}

func (c *openAICompatibleStreamToolCalls) finalBoundaryEvents(events []core.StreamEvent) []core.StreamEvent {
	if len(events) == 0 {
		return nil
	}
	finalEvents := make([]core.StreamEvent, len(events))
	for i, event := range events {
		index := event.ToolCall.Index
		if call := c.calls[index]; call != nil {
			event.ToolCall.ID = call.id
			event.ToolCall.Name = call.name
		}
		finalEvents[i] = event
	}
	return finalEvents
}

func (c *openAICompatibleStreamToolCalls) toolCalls() ([]core.ToolCall, error) {
	if len(c.order) == 0 {
		return nil, nil
	}
	calls := make([]core.ToolCall, 0, len(c.order))
	for _, index := range c.order {
		call := c.calls[index]
		arguments, err := openAIParseToolCallArguments(call.arguments.String(), call.name, call.id)
		if err != nil {
			return nil, err
		}
		calls = append(calls, core.ToolCall{
			ID:        call.id,
			Name:      call.name,
			Arguments: arguments,
		})
	}
	return calls, nil
}

func openAIStreamToolCallBoundary(eventType core.StreamEventType, index int, id string, name string) core.StreamEvent {
	return core.StreamEvent{
		Type: eventType,
		ToolCall: core.StreamToolCall{
			ID:    id,
			Name:  name,
			Index: index,
		},
	}
}

func appendOpenAIStreamValue(existing string, fragment string) string {
	if fragment == "" {
		return existing
	}
	if existing == "" || existing == fragment {
		return fragment
	}
	if strings.HasPrefix(fragment, existing) {
		return fragment
	}
	if strings.HasPrefix(existing, fragment) {
		return existing
	}
	return existing + fragment
}

func streamOpenAIResponseEvents(ctx context.Context, body io.Reader, events chan<- core.StreamEvent, diagnostics core.ProviderDiagnostics) {
	var content strings.Builder
	var finalText string
	var toolCalls openAIResponsesStreamToolCalls
	var finish core.StreamFinishMetadata
	var boundaryEvents []core.StreamEvent
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
		case "response.output_item.added", "response.output_item.done":
			boundaryEvents = append(boundaryEvents, toolCalls.addItem(decoded.OutputIndex, decoded.ItemID, decoded.Item)...)
		case "response.function_call_arguments.delta":
			toolCalls.appendArguments(decoded.OutputIndex, decoded.ItemID, decoded.Delta)
		case "response.function_call_arguments.done":
			boundaryEvents = append(boundaryEvents, toolCalls.setArguments(decoded.OutputIndex, decoded.ItemID, decoded.Arguments)...)
		case "response.completed":
			finish.Reason = decoded.Response.finishReason("completed")
			boundaryEvents = append(boundaryEvents, toolCalls.doneEvents()...)
			response, err := decoded.Response.modelResponse()
			streamCalls, streamCallErr := toolCalls.toolCalls()
			if streamCallErr != nil && (err != nil || len(response.Message.ToolCalls) == 0) {
				return sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai responses streamed function call arguments", diagnostics, streamCallErr))
			}
			if err != nil {
				text := finalText
				if text == "" {
					text = content.String()
				}
				if text == "" && len(streamCalls) == 0 {
					return sendOpenAIStreamError(ctx, events, core.NewProviderDecodeError("decode openai responses completed event", diagnostics, err))
				}
				response = ModelResponse{
					Message: Message{
						Role:      RoleAssistant,
						Content:   text,
						ToolCalls: core.CloneToolCalls(streamCalls),
					},
					ToolCalls: streamCalls,
					Usage:     decoded.Response.Usage.tokenUsage(),
				}
			} else if len(streamCalls) > 0 && len(response.Message.ToolCalls) == 0 {
				response.ToolCalls = streamCalls
				response.Message.ToolCalls = core.CloneToolCalls(streamCalls)
			}
			if response.Message.Content == "" {
				response.Message.Content = content.String()
			}
			if err := sendOpenAIStreamEvents(ctx, events, boundaryEvents); err != nil {
				return err
			}
			done = true
			return sendOpenAIStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDone, Message: response.Message, Usage: response.Usage, Finish: finish})
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

// openAIResponsesStreamToolCalls reconstructs Responses API function-call
// output items from semantic streaming events when the completed response omits them.
type openAIResponsesStreamToolCalls struct {
	calls     map[string]*openAIResponsesStreamToolCall
	order     []string
	indexKeys map[int]string
	started   map[string]bool
	done      map[string]bool
}

type openAIResponsesStreamToolCall struct {
	outputIndex int
	itemID      string
	callID      string
	name        string
	arguments   strings.Builder
}

func (c *openAIResponsesStreamToolCalls) addItem(outputIndex int, itemID string, item openAIResponsesOutputItem) []core.StreamEvent {
	if item.Type != "function_call" {
		return nil
	}
	if itemID == "" {
		itemID = item.ID
	}
	call := c.ensure(outputIndex, itemID)
	call.itemID = appendOpenAIStreamValue(call.itemID, item.ID)
	call.callID = appendOpenAIStreamValue(call.callID, item.CallID)
	call.name = appendOpenAIStreamValue(call.name, item.Name)
	if item.Arguments != "" {
		call.arguments.Reset()
		call.arguments.WriteString(item.Arguments)
	}
	return c.startEvent(call)
}

func (c *openAIResponsesStreamToolCalls) appendArguments(outputIndex int, itemID string, delta string) {
	if delta == "" {
		return
	}
	call := c.ensure(outputIndex, itemID)
	call.arguments.WriteString(delta)
}

func (c *openAIResponsesStreamToolCalls) setArguments(outputIndex int, itemID string, arguments string) []core.StreamEvent {
	call := c.ensure(outputIndex, itemID)
	call.arguments.Reset()
	call.arguments.WriteString(arguments)
	events := c.startEvent(call)
	return append(events, c.doneEvent(call)...)
}

func (c *openAIResponsesStreamToolCalls) ensure(outputIndex int, itemID string) *openAIResponsesStreamToolCall {
	if c.calls == nil {
		c.calls = make(map[string]*openAIResponsesStreamToolCall)
		c.indexKeys = make(map[int]string)
		c.started = make(map[string]bool)
		c.done = make(map[string]bool)
	}
	key := itemID
	if key == "" {
		key = c.indexKeys[outputIndex]
	}
	if key == "" {
		key = fmt.Sprintf("index:%d", outputIndex)
	}
	if itemID != "" {
		if oldKey := c.indexKeys[outputIndex]; oldKey != "" && oldKey != key {
			if call := c.calls[oldKey]; call != nil {
				delete(c.calls, oldKey)
				c.calls[key] = call
				if c.started[oldKey] {
					c.started[key] = true
					delete(c.started, oldKey)
				}
				if c.done[oldKey] {
					c.done[key] = true
					delete(c.done, oldKey)
				}
				for i, ordered := range c.order {
					if ordered == oldKey {
						c.order[i] = key
						break
					}
				}
			}
		}
		c.indexKeys[outputIndex] = key
	}
	call := c.calls[key]
	if call == nil {
		call = &openAIResponsesStreamToolCall{outputIndex: outputIndex, itemID: itemID}
		c.calls[key] = call
		c.order = append(c.order, key)
	}
	if itemID != "" {
		call.itemID = appendOpenAIStreamValue(call.itemID, itemID)
	}
	return call
}

func (c *openAIResponsesStreamToolCalls) startEvent(call *openAIResponsesStreamToolCall) []core.StreamEvent {
	if call == nil || c.started[call.key()] {
		return nil
	}
	c.started[call.key()] = true
	return []core.StreamEvent{openAIStreamToolCallBoundary(core.StreamEventToolCallStart, call.outputIndex, call.callID, call.name)}
}

func (c *openAIResponsesStreamToolCalls) doneEvent(call *openAIResponsesStreamToolCall) []core.StreamEvent {
	if call == nil || c.done[call.key()] {
		return nil
	}
	c.done[call.key()] = true
	return []core.StreamEvent{openAIStreamToolCallBoundary(core.StreamEventToolCallDone, call.outputIndex, call.callID, call.name)}
}

func (c *openAIResponsesStreamToolCalls) doneEvents() []core.StreamEvent {
	if len(c.order) == 0 {
		return nil
	}
	events := make([]core.StreamEvent, 0, len(c.order))
	for _, key := range c.order {
		call := c.calls[key]
		events = append(events, c.startEvent(call)...)
		events = append(events, c.doneEvent(call)...)
	}
	return events
}

func (c *openAIResponsesStreamToolCall) key() string {
	if c.itemID != "" {
		return c.itemID
	}
	return fmt.Sprintf("index:%d", c.outputIndex)
}

func (c *openAIResponsesStreamToolCalls) toolCalls() ([]core.ToolCall, error) {
	if len(c.order) == 0 {
		return nil, nil
	}
	calls := make([]core.ToolCall, 0, len(c.order))
	for _, key := range c.order {
		call := c.calls[key]
		arguments, err := openAIParseToolCallArguments(call.arguments.String(), call.name, call.callID)
		if err != nil {
			return nil, err
		}
		calls = append(calls, core.ToolCall{
			ID:        call.callID,
			Name:      call.name,
			Arguments: arguments,
		})
	}
	return calls, nil
}

func (r openAIResponsesResponse) finishReason(defaultReason string) string {
	if status := strings.TrimSpace(r.Status); status != "" {
		return status
	}
	return defaultReason
}

func sendOpenAIStreamEvents(ctx context.Context, events chan<- core.StreamEvent, streamEvents []core.StreamEvent) error {
	for _, event := range streamEvents {
		if err := sendOpenAIStreamEvent(ctx, events, event); err != nil {
			return err
		}
	}
	return nil
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
