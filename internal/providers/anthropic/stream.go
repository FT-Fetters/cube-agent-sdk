package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
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
	payload, err := newAnthropicMessagesRequest(m.model, m.maxTokens, m.thinking, request)
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
	Index        int                            `json:"index"`
	Message      anthropicMessagesStreamMessage `json:"message"`
	ContentBlock json.RawMessage                `json:"content_block"`
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
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}

type anthropicMessagesStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func streamAnthropicMessagesEvents(ctx context.Context, body io.Reader, events chan<- core.StreamEvent, diagnostics core.ProviderDiagnostics) {
	var content strings.Builder
	var usage anthropicMessagesUsage
	var blocks anthropicStreamContentBlocks
	var finish core.StreamFinishMetadata
	var boundaryEvents []core.StreamEvent
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
			block, err := blocks.start(decoded.Index, decoded.ContentBlock)
			if err != nil {
				return sendAnthropicStreamError(ctx, events, core.NewProviderDecodeError("decode anthropic messages stream content block", diagnostics, err))
			}
			if block.Type == "tool_use" {
				boundaryEvents = append(boundaryEvents, anthropicStreamToolCallBoundary(core.StreamEventToolCallStart, decoded.Index, block.ID, block.Name))
			}
			if block.Type == "text" && block.Text != "" {
				content.WriteString(block.Text)
				return sendAnthropicStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDelta, Delta: block.Text})
			}
		case "content_block_delta":
			switch decoded.Delta.Type {
			case "text_delta":
				return sendAnthropicTextDelta(ctx, events, &content, &blocks, decoded.Index, decoded.Delta.Text)
			case "thinking_delta":
				blocks.appendString(decoded.Index, "thinking", "thinking", decoded.Delta.Thinking)
			case "signature_delta":
				blocks.appendString(decoded.Index, "thinking", "signature", decoded.Delta.Signature)
			case "input_json_delta":
				blocks.appendPartialJSON(decoded.Index, decoded.Delta.PartialJSON)
			}
		case "content_block_stop":
			if toolCall, ok := blocks.toolCallMetadata(decoded.Index); ok {
				boundaryEvents = append(boundaryEvents, core.StreamEvent{Type: core.StreamEventToolCallDone, ToolCall: toolCall})
			}
		case "message_delta":
			mergeAnthropicStreamUsage(&usage, decoded.Usage)
			if decoded.Delta.StopReason != "" {
				finish.Reason = decoded.Delta.StopReason
			}
		case "message_stop":
			toolCalls, err := blocks.toolCalls()
			if err != nil {
				return sendAnthropicStreamError(ctx, events, core.NewProviderDecodeError("decode anthropic messages streamed tool_use input", diagnostics, err))
			}
			if err := sendAnthropicStreamEvents(ctx, events, boundaryEvents); err != nil {
				return err
			}
			done = true
			return sendAnthropicStreamEvent(ctx, events, core.StreamEvent{
				Type: core.StreamEventDone,
				Message: core.Message{
					Role:      RoleAssistant,
					Content:   content.String(),
					ToolCalls: core.CloneToolCalls(toolCalls),
					Metadata:  blocks.metadata(),
				},
				Usage:  usage.tokenUsage(),
				Finish: finish,
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

// anthropicStreamContentBlocks reconstructs provider content blocks so
// thinking signatures can be replayed on later Messages API calls.
type anthropicStreamContentBlocks struct {
	blocks     map[int]map[string]any
	toolInputs map[int]*strings.Builder
}

func (b *anthropicStreamContentBlocks) start(index int, raw json.RawMessage) (anthropicContentBlock, error) {
	if len(raw) == 0 {
		return anthropicContentBlock{}, nil
	}
	var block anthropicContentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return anthropicContentBlock{}, err
	}
	var rawBlock map[string]any
	if err := json.Unmarshal(raw, &rawBlock); err != nil {
		return anthropicContentBlock{}, err
	}
	b.set(index, rawBlock)
	return block, nil
}

func (b *anthropicStreamContentBlocks) set(index int, block map[string]any) {
	if len(block) == 0 {
		return
	}
	if b.blocks == nil {
		b.blocks = make(map[int]map[string]any)
	}
	b.blocks[index] = block
}

func (b *anthropicStreamContentBlocks) appendString(index int, blockType string, key string, value string) {
	if value == "" {
		return
	}
	block := b.ensure(index, blockType)
	existing, _ := block[key].(string)
	block[key] = existing + value
}

func (b *anthropicStreamContentBlocks) ensure(index int, blockType string) map[string]any {
	if b.blocks == nil {
		b.blocks = make(map[int]map[string]any)
	}
	block := b.blocks[index]
	if block == nil {
		block = map[string]any{"type": blockType}
		b.blocks[index] = block
	}
	if block["type"] == nil {
		block["type"] = blockType
	}
	return block
}

func (b *anthropicStreamContentBlocks) appendPartialJSON(index int, partial string) {
	if partial == "" {
		return
	}
	if b.toolInputs == nil {
		b.toolInputs = make(map[int]*strings.Builder)
	}
	input := b.toolInputs[index]
	if input == nil {
		input = &strings.Builder{}
		b.toolInputs[index] = input
	}
	input.WriteString(partial)
}

func (b *anthropicStreamContentBlocks) toolCalls() ([]core.ToolCall, error) {
	if len(b.blocks) == 0 {
		return nil, nil
	}
	indexes := make([]int, 0, len(b.blocks))
	for index := range b.blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	calls := make([]core.ToolCall, 0)
	for _, index := range indexes {
		block := b.blocks[index]
		if block["type"] != "tool_use" {
			continue
		}
		id, _ := block["id"].(string)
		name, _ := block["name"].(string)
		input, err := b.toolInput(index, block, id, name)
		if err != nil {
			return nil, err
		}
		block["input"] = cloneAnyMap(input)
		calls = append(calls, core.ToolCall{
			ID:        id,
			Name:      name,
			Arguments: cloneAnyMap(input),
		})
	}
	return calls, nil
}

func (b *anthropicStreamContentBlocks) toolCallMetadata(index int) (core.StreamToolCall, bool) {
	if b.blocks == nil {
		return core.StreamToolCall{}, false
	}
	block := b.blocks[index]
	if block == nil || block["type"] != "tool_use" {
		return core.StreamToolCall{}, false
	}
	id, _ := block["id"].(string)
	name, _ := block["name"].(string)
	return core.StreamToolCall{ID: id, Name: name, Index: index}, true
}

func (b *anthropicStreamContentBlocks) toolInput(index int, block map[string]any, id string, name string) (map[string]any, error) {
	if partial := b.toolInputs[index]; partial != nil && partial.Len() > 0 {
		return parseAnthropicStreamToolInput(partial.String(), id, name)
	}
	if input, ok := block["input"].(map[string]any); ok {
		return cloneAnyMap(input), nil
	}
	if block["input"] == nil {
		return map[string]any{}, nil
	}
	data, err := json.Marshal(block["input"])
	if err != nil {
		return nil, fmt.Errorf("agent: encode anthropic messages tool_use input for %s: %w", anthropicToolUseLabel(name, id), err)
	}
	return parseAnthropicStreamToolInput(string(data), id, name)
}

func parseAnthropicStreamToolInput(raw string, id string, name string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, fmt.Errorf("agent: anthropic messages tool_use input for %s contains invalid JSON: %w", anthropicToolUseLabel(name, id), err)
	}
	if input == nil {
		return nil, fmt.Errorf("agent: anthropic messages tool_use input for %s must be a JSON object", anthropicToolUseLabel(name, id))
	}
	return input, nil
}

func anthropicToolUseLabel(name string, id string) string {
	switch {
	case name != "" && id != "":
		return fmt.Sprintf("%s (%s)", name, id)
	case name != "":
		return name
	case id != "":
		return id
	default:
		return "unknown"
	}
}

func (b *anthropicStreamContentBlocks) metadata() map[string]any {
	content := b.content()
	if len(content) == 0 {
		return nil
	}
	return map[string]any{anthropicMessagesContentMetadataKey: content}
}

func (b *anthropicStreamContentBlocks) content() []map[string]any {
	if len(b.blocks) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(b.blocks))
	for index := range b.blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	content := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		content = append(content, cloneAnyMap(b.blocks[index]))
	}
	return content
}

func anthropicStreamToolCallBoundary(eventType core.StreamEventType, index int, id string, name string) core.StreamEvent {
	return core.StreamEvent{
		Type: eventType,
		ToolCall: core.StreamToolCall{
			ID:    id,
			Name:  name,
			Index: index,
		},
	}
}

func sendAnthropicTextDelta(ctx context.Context, events chan<- core.StreamEvent, content *strings.Builder, blocks *anthropicStreamContentBlocks, index int, delta string) error {
	if delta == "" {
		return nil
	}
	content.WriteString(delta)
	if blocks != nil {
		blocks.appendString(index, "text", "text", delta)
	}
	return sendAnthropicStreamEvent(ctx, events, core.StreamEvent{Type: core.StreamEventDelta, Delta: delta})
}

func sendAnthropicStreamEvents(ctx context.Context, events chan<- core.StreamEvent, streamEvents []core.StreamEvent) error {
	for _, event := range streamEvents {
		if err := sendAnthropicStreamEvent(ctx, events, event); err != nil {
			return err
		}
	}
	return nil
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
