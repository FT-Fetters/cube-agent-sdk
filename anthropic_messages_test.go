package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicMessagesModelSendsMessagesRequest(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got != defaultAnthropicVersion {
			t.Fatalf("anthropic-version = %q, want %s", got, defaultAnthropicVersion)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}

		var payload anthropicMessagesRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "claude-test-model" || payload.MaxTokens != defaultAnthropicMaxTokens {
			t.Fatalf("model/max_tokens = %q/%d, want claude-test-model/%d", payload.Model, payload.MaxTokens, defaultAnthropicMaxTokens)
		}
		if payload.System != "You are concise." {
			t.Fatalf("system = %q, want system prompt", payload.System)
		}
		if len(payload.Messages) != 3 {
			t.Fatalf("messages = %#v, want user, assistant tool_use, user tool_result", payload.Messages)
		}
		if payload.Messages[0].Role != "user" || payload.Messages[0].Content != "Find docs" {
			t.Fatalf("first message = %#v, want user text", payload.Messages[0])
		}

		assistantBlocks, ok := payload.Messages[1].Content.([]anthropicContentBlockForTest)
		if !ok {
			t.Fatalf("assistant content = %#v, want block array", payload.Messages[1].Content)
		}
		if len(assistantBlocks) != 2 || assistantBlocks[0].Type != "text" || assistantBlocks[0].Text != "Searching" {
			t.Fatalf("assistant text blocks = %#v, want text block", assistantBlocks)
		}
		if assistantBlocks[1].Type != "tool_use" || assistantBlocks[1].ID != "toolu-prev" || assistantBlocks[1].Name != "search" {
			t.Fatalf("assistant tool_use block = %#v, want search tool_use", assistantBlocks[1])
		}
		if assistantBlocks[1].Input["query"] != "previous" {
			t.Fatalf("assistant tool input = %#v, want previous query", assistantBlocks[1].Input)
		}

		toolResultBlocks, ok := payload.Messages[2].Content.([]anthropicContentBlockForTest)
		if !ok {
			t.Fatalf("tool result content = %#v, want block array", payload.Messages[2].Content)
		}
		if len(toolResultBlocks) != 1 || toolResultBlocks[0].Type != "tool_result" || toolResultBlocks[0].ToolUseID != "toolu-prev" || toolResultBlocks[0].Content != "Previous result" {
			t.Fatalf("tool result blocks = %#v, want matching tool_result", toolResultBlocks)
		}

		if len(payload.Tools) != 1 || payload.Tools[0].Name != "search" || payload.Tools[0].Description != "Search documents" {
			t.Fatalf("tools = %#v, want search tool", payload.Tools)
		}
		wantSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query"},
				"limit": map[string]any{"type": "integer"},
			},
			"required": []any{"query"},
		}
		if !mapsEqual(payload.Tools[0].InputSchema, wantSchema) {
			t.Fatalf("input schema = %#v, want %#v", payload.Tools[0].InputSchema, wantSchema)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Done"}],"stop_reason":"end_turn"}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := model.Generate(ctx, ModelRequest{
		SystemPrompt: "You are concise.",
		Messages: []Message{
			{Role: RoleUser, Content: "Find docs"},
			{
				Role:    RoleAssistant,
				Content: "Searching",
				ToolCalls: []ToolCall{{
					ID:        "toolu-prev",
					Name:      "search",
					Arguments: map[string]any{"query": "previous"},
				}},
			},
			{Role: RoleTool, Name: "search", ToolCallID: "toolu-prev", Content: "Previous result"},
		},
		Tools: []ToolDescriptor{{
			Name:        "search",
			Description: "Search documents",
			Parameters: &ToolParametersSchema{
				Type:     SchemaTypeObject,
				Required: []string{"query"},
				Properties: map[string]ToolParametersSchema{
					"query": {Type: SchemaTypeString, Description: "Search query"},
					"limit": {Type: SchemaTypeInteger},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Role != RoleAssistant || response.Message.Content != "Done" {
		t.Fatalf("response message = %#v, want assistant Done", response.Message)
	}
}

func TestAnthropicMessagesModelSendsThinkingConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload anthropicMessagesRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Thinking == nil {
			t.Fatal("thinking config is nil, want request thinking config")
		}
		if payload.Thinking.Type != "adaptive" || payload.Thinking.Display != "summarized" {
			t.Fatalf("thinking config = %#v, want adaptive summarized", payload.Thinking)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
		Thinking: &AnthropicThinkingConfig{
			Type:    "adaptive",
			Display: "summarized",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := model.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicMessagesModelUsesFullMessagesURLAndCustomVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/v1/messages" {
			t.Fatalf("path = %q, want /custom/v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-version"); got != "2025-01-01" {
			t.Fatalf("anthropic-version = %q, want custom version", got)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:          server.URL + "/custom/v1/messages",
		Model:            "claude-test-model",
		AnthropicVersion: "2025-01-01",
		HTTPClient:       server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "ok" {
		t.Fatalf("content = %q, want ok", response.Message.Content)
	}
}

func TestAnthropicMessagesModelParsesToolUse(t *testing.T) {
	server := httptest.NewServer(newAnthropicMessagesTestHandler(t))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "I will search." {
		t.Fatalf("content = %q, want text block", response.Message.Content)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one", response.ToolCalls)
	}
	call := response.ToolCalls[0]
	if call.ID != "toolu-1" || call.Name != "search" {
		t.Fatalf("tool call = %#v, want search toolu-1", call)
	}
	if call.Arguments["query"] != "docs" || call.Arguments["limit"] != float64(3) {
		t.Fatalf("tool arguments = %#v, want docs limit 3", call.Arguments)
	}
}

func TestAnthropicMessagesModelPreservesThinkingBlocksAndReplaysRawContent(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			_, _ = io.WriteString(w, `{
				"id": "msg_1",
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "Need to search first.", "signature": "thinking-signature-1"},
					{"type": "redacted_thinking", "data": "encrypted-redacted-thinking-1"},
					{"type": "text", "text": "I will search."},
					{"type": "tool_use", "id": "toolu-1", "name": "search", "input": {"query": "docs", "limit": 3}}
				],
				"stop_reason": "tool_use"
			}`)
		case 2:
			var payload anthropicMessagesRequestForTest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Messages) != 2 {
				t.Fatalf("messages = %#v, want assistant raw content and user tool_result", payload.Messages)
			}
			assistantBlocks, ok := payload.Messages[0].Content.([]anthropicContentBlockForTest)
			if !ok {
				t.Fatalf("assistant content = %#v, want raw block array", payload.Messages[0].Content)
			}
			if len(assistantBlocks) != 4 {
				t.Fatalf("assistant blocks = %#v, want preserved thinking, redacted_thinking, text, and tool_use", assistantBlocks)
			}
			if assistantBlocks[0].Type != "thinking" || assistantBlocks[0].Thinking != "Need to search first." || assistantBlocks[0].Signature != "thinking-signature-1" {
				t.Fatalf("thinking block = %#v, want unmodified thinking block", assistantBlocks[0])
			}
			if assistantBlocks[1].Type != "redacted_thinking" || assistantBlocks[1].Data != "encrypted-redacted-thinking-1" {
				t.Fatalf("redacted thinking block = %#v, want unmodified redacted_thinking block", assistantBlocks[1])
			}
			if assistantBlocks[2].Type != "text" || assistantBlocks[2].Text != "I will search." {
				t.Fatalf("text block = %#v, want preserved text block", assistantBlocks[2])
			}
			if assistantBlocks[3].Type != "tool_use" || assistantBlocks[3].ID != "toolu-1" || assistantBlocks[3].Name != "search" {
				t.Fatalf("tool_use block = %#v, want preserved tool_use block", assistantBlocks[3])
			}
			if assistantBlocks[3].Input["query"] != "docs" || assistantBlocks[3].Input["limit"] != float64(3) {
				t.Fatalf("tool_use input = %#v, want original input", assistantBlocks[3].Input)
			}
			toolResultBlocks, ok := payload.Messages[1].Content.([]anthropicContentBlockForTest)
			if !ok || len(toolResultBlocks) != 1 || toolResultBlocks[0].Type != "tool_result" {
				t.Fatalf("tool result content = %#v, want tool_result block", payload.Messages[1].Content)
			}
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"Final"}]}`)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Message.Content != "I will search." {
		t.Fatalf("content = %q, want text block", first.Message.Content)
	}
	if len(first.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one", first.ToolCalls)
	}
	rawContent, ok := first.Message.Metadata["anthropic_messages_content"].([]map[string]any)
	if !ok || len(rawContent) != 4 {
		t.Fatalf("metadata raw content = %#v, want four preserved blocks", first.Message.Metadata["anthropic_messages_content"])
	}

	second, err := model.Generate(context.Background(), ModelRequest{
		Messages: []Message{
			first.Message,
			{Role: RoleTool, Name: "search", ToolCallID: "toolu-1", Content: "Search result"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Message.Content != "Final" {
		t.Fatalf("second content = %q, want Final", second.Message.Content)
	}
}

func TestAnthropicMessagesModelParsesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Done"}],
			"usage": {
				"input_tokens": 17,
				"output_tokens": 9
			}
		}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "Done" {
		t.Fatalf("content = %q, want Done", response.Message.Content)
	}
	want := TokenUsage{InputTokens: 17, OutputTokens: 9, TotalTokens: 26}
	if response.Usage != want {
		t.Fatalf("usage = %#v, want %#v", response.Usage, want)
	}
}

func TestAnthropicMessagesModelStreamsDeltasDoneAndUsage(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		var payload anthropicMessagesRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !payload.Stream {
			t.Fatalf("stream = false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "message_start", `{"type":"message_start","message":{"usage":{"input_tokens":17}}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`)
		writeSSEEvent(t, w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`)
		writeSSEEvent(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := model.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)
	if !sawRequest {
		t.Fatal("server did not receive a request")
	}
	assertProviderStreamSuccess(t, got, "Hel", "lo", "Hello", TokenUsage{InputTokens: 17, OutputTokens: 9, TotalTokens: 26})
}

func TestAnthropicMessagesModelStreamPreservesThinkingMetadataOnDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "message_start", `{"type":"message_start","message":{"usage":{"input_tokens":17}}}`)
		writeSSEEvent(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need to answer carefully."}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"stream-signature-1"}}`)
		writeSSEEvent(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeSSEEvent(t, w, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hel"}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"lo"}}`)
		writeSSEEvent(t, w, "message_delta", `{"type":"message_delta","usage":{"output_tokens":9}}`)
		writeSSEEvent(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := model.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)
	if len(got) != 4 {
		t.Fatalf("stream events = %#v, want thinking, delta, delta, done", got)
	}
	if got[0].Type != StreamEventThinkingDelta || got[0].Delta != "Need to answer carefully." {
		t.Fatalf("thinking stream event = %#v, want thinking delta", got[0])
	}
	if got[1].Type != StreamEventDelta || got[1].Delta != "Hel" {
		t.Fatalf("first text stream event = %#v, want Hel delta", got[1])
	}
	if got[2].Type != StreamEventDelta || got[2].Delta != "lo" {
		t.Fatalf("second text stream event = %#v, want lo delta", got[2])
	}
	if got[3].Type != StreamEventDone || got[3].Message.Content != "Hello" || got[3].Usage != (TokenUsage{InputTokens: 17, OutputTokens: 9, TotalTokens: 26}) {
		t.Fatalf("done stream event = %#v, want final Hello with usage", got[3])
	}
	rawContent, ok := got[3].Message.Metadata["anthropic_messages_content"].([]map[string]any)
	if !ok || len(rawContent) != 2 {
		t.Fatalf("metadata raw content = %#v, want thinking and text blocks", got[3].Message.Metadata["anthropic_messages_content"])
	}
	if rawContent[0]["type"] != "thinking" || rawContent[0]["thinking"] != "Need to answer carefully." || rawContent[0]["signature"] != "stream-signature-1" {
		t.Fatalf("thinking metadata = %#v, want reconstructed thinking block", rawContent[0])
	}
	if rawContent[1]["type"] != "text" || rawContent[1]["text"] != "Hello" {
		t.Fatalf("text metadata = %#v, want reconstructed text block", rawContent[1])
	}
}

func TestAnthropicMessagesModelStreamEmitsThinkingDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need to "}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"answer carefully."}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"stream-signature-1"}}`)
		writeSSEEvent(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeSSEEvent(t, w, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello"}}`)
		writeSSEEvent(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := model.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)
	if len(got) != 4 {
		t.Fatalf("stream events = %#v, want thinking, thinking, delta, done", got)
	}
	if got[0].Type != StreamEventThinkingDelta || got[0].Delta != "Need to " {
		t.Fatalf("first stream event = %#v, want thinking delta", got[0])
	}
	if got[1].Type != StreamEventThinkingDelta || got[1].Delta != "answer carefully." {
		t.Fatalf("second stream event = %#v, want thinking delta", got[1])
	}
	if got[2].Type != StreamEventDelta || got[2].Delta != "Hello" {
		t.Fatalf("third stream event = %#v, want text delta", got[2])
	}
	if got[3].Type != StreamEventDone || got[3].Message.Content != "Hello" {
		t.Fatalf("done stream event = %#v, want final text only", got[3])
	}
	rawContent, ok := got[3].Message.Metadata["anthropic_messages_content"].([]map[string]any)
	if !ok || len(rawContent) != 2 {
		t.Fatalf("metadata raw content = %#v, want thinking and text blocks", got[3].Message.Metadata["anthropic_messages_content"])
	}
	if rawContent[0]["thinking"] != "Need to answer carefully." || rawContent[0]["signature"] != "stream-signature-1" {
		t.Fatalf("thinking metadata = %#v, want reconstructed thinking block", rawContent[0])
	}
}

func TestAnthropicMessagesModelStreamMapsToolUseBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "message_start", `{"type":"message_start","message":{"usage":{"input_tokens":17}}}`)
		writeSSEEvent(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"search","input":{}}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":`+strconvQuote(`{"query"`)+`}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":`+strconvQuote(`:"docs","limit":3}`)+`}}`)
		writeSSEEvent(t, w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeSSEEvent(t, w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`)
		writeSSEEvent(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := model.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)
	if len(got) != 3 {
		t.Fatalf("stream events = %#v, want tool start, tool done, done", got)
	}
	assertStreamToolCallBoundary(t, got[0], StreamEventToolCallStart, 0, "toolu-1", "search")
	assertStreamToolCallBoundary(t, got[1], StreamEventToolCallDone, 0, "toolu-1", "search")
	assertStreamDoneToolCall(t, got[2], "", "toolu-1", "search", map[string]any{"query": "docs", "limit": float64(3)}, TokenUsage{InputTokens: 17, OutputTokens: 9, TotalTokens: 26})
	if got[2].Finish.Reason != "tool_use" {
		t.Fatalf("done finish metadata = %#v, want tool_use", got[2].Finish)
	}
	rawContent, ok := got[2].Message.Metadata["anthropic_messages_content"].([]map[string]any)
	if !ok || len(rawContent) != 1 {
		t.Fatalf("metadata raw content = %#v, want tool_use block", got[2].Message.Metadata["anthropic_messages_content"])
	}
	input, ok := rawContent[0]["input"].(map[string]any)
	if rawContent[0]["type"] != "tool_use" || rawContent[0]["id"] != "toolu-1" || !ok || input["query"] != "docs" || input["limit"] != float64(3) {
		t.Fatalf("tool_use metadata = %#v, want reconstructed input", rawContent[0])
	}
}

func TestAnthropicMessagesModelStreamRejectsInvalidToolUseInputSafely(t *testing.T) {
	const rawProviderPayload = "secret-raw-provider-payload"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"search","input":{}}}`)
		writeSSEEvent(t, w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":`+strconvQuote(rawProviderPayload)+`}}`)
		writeSSEEvent(t, w, "message_stop", `{"type":"message_stop"}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := model.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)
	assertProviderStreamError(t, got, ProviderDiagnostics{
		Provider:     "anthropic-messages",
		EndpointHost: server.Listener.Addr().String(),
	}, ModelErrorSubcategoryDecodeError)
	if strings.Contains(got[0].Error.Error(), rawProviderPayload) {
		t.Fatalf("stream error exposed unsafe provider detail: %v", got[0].Error)
	}
}

func TestAnthropicMessagesModelStreamEmitsProviderErrorWithDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "error", `{"type":"error","error":{"type":"overloaded_error","message":"provider secret payload"}}`)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := model.Stream(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := collectStreamEvents(t, events)
	assertProviderStreamError(t, got, ProviderDiagnostics{
		Provider:     "anthropic-messages",
		EndpointHost: server.Listener.Addr().String(),
	}, ModelErrorSubcategoryUnknown)
	if strings.Contains(got[0].Error.Error(), "provider secret payload") {
		t.Fatalf("stream error exposed unsafe provider detail: %v", got[0].Error)
	}
}

func TestAnthropicMessagesModelReturnsNon2xxErrorWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		w.Header().Set("Request-Id", "anthropic-request-1")
		http.Error(w, "provider rejected test-key", http.StatusUnauthorized)
	}))
	defer server.Close()

	model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		Model:      "claude-test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = model.Generate(context.Background(), ModelRequest{})
	if err == nil {
		t.Fatal("Generate returned nil error, want non-2xx error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want status", err)
	}
	if strings.Contains(err.Error(), "provider rejected") || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("error exposed unsafe provider detail: %v", err)
	}
	want := ProviderDiagnostics{
		Provider:     "anthropic-messages",
		HTTPStatus:   http.StatusUnauthorized,
		EndpointHost: server.Listener.Addr().String(),
		RequestID:    "anthropic-request-1",
	}
	got, ok := ProviderDiagnosticsFromError(err)
	if !ok || got != want {
		t.Fatalf("provider diagnostics = %#v/%t, want %#v/true", got, ok, want)
	}
}

func TestNewAnthropicMessagesModelValidatesRequiredConfig(t *testing.T) {
	tests := []struct {
		name   string
		config AnthropicMessagesConfig
	}{
		{name: "base URL", config: AnthropicMessagesConfig{Model: "claude-test-model"}},
		{name: "model", config: AnthropicMessagesConfig{BaseURL: "https://example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAnthropicMessagesModel(tt.config)
			if err == nil {
				t.Fatal("NewAnthropicMessagesModel returned nil error, want validation error")
			}
		})
	}
}

func newAnthropicMessagesTestHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"content": [
				{"type": "text", "text": "I will search."},
				{"type": "tool_use", "id": "toolu-1", "name": "search", "input": {"query": "docs", "limit": 3}}
			],
			"stop_reason": "tool_use"
		}`)
	}
}

func newOpenAICompatibleTestHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}
}

type anthropicMessagesRequestForTest struct {
	Model     string                          `json:"model"`
	MaxTokens int                             `json:"max_tokens"`
	System    string                          `json:"system"`
	Messages  []anthropicMessageForTest       `json:"messages"`
	Tools     []anthropicToolDefForTest       `json:"tools"`
	Thinking  *anthropicThinkingConfigForTest `json:"thinking"`
	Stream    bool                            `json:"stream"`
}

type anthropicMessageForTest struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func (m *anthropicMessageForTest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	if len(raw.Content) > 0 && raw.Content[0] == '"' {
		var text string
		if err := json.Unmarshal(raw.Content, &text); err != nil {
			return err
		}
		m.Content = text
		return nil
	}
	var blocks []anthropicContentBlockForTest
	if err := json.Unmarshal(raw.Content, &blocks); err != nil {
		return err
	}
	m.Content = blocks
	return nil
}

type anthropicContentBlockForTest struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
	Content   string         `json:"content"`
	Thinking  string         `json:"thinking"`
	Signature string         `json:"signature"`
	Data      string         `json:"data"`
}

type anthropicThinkingConfigForTest struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
	Display      string `json:"display"`
}

type anthropicToolDefForTest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}
