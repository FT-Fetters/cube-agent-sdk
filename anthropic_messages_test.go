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

func TestAnthropicMessagesModelReturnsNon2xxErrorWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
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
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "provider rejected") {
		t.Fatalf("error = %v, want status and response body summary", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatalf("error exposed API key: %v", err)
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
	Model     string                    `json:"model"`
	MaxTokens int                       `json:"max_tokens"`
	System    string                    `json:"system"`
	Messages  []anthropicMessageForTest `json:"messages"`
	Tools     []anthropicToolDefForTest `json:"tools"`
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
}

type anthropicToolDefForTest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}
