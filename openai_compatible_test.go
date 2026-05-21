package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleModelSendsChatCompletionRequest(t *testing.T) {
	ctx := context.Background()
	var sawRequest bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}

		var payload openAIChatCompletionRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "test-model" {
			t.Fatalf("model = %q, want test-model", payload.Model)
		}
		if len(payload.Messages) != 4 {
			t.Fatalf("messages = %#v, want system plus three history messages", payload.Messages)
		}
		if payload.Messages[0].Role != "system" || payload.Messages[0].Content != "You are concise." {
			t.Fatalf("system message = %#v", payload.Messages[0])
		}
		if payload.Messages[1].Role != "user" || payload.Messages[1].Content != "Find docs" {
			t.Fatalf("user message = %#v", payload.Messages[1])
		}
		if len(payload.Messages[2].ToolCalls) != 1 {
			t.Fatalf("assistant tool calls = %#v, want one", payload.Messages[2].ToolCalls)
		}
		call := payload.Messages[2].ToolCalls[0]
		if call.ID != "call-prev" || call.Type != "function" || call.Function.Name != "search" {
			t.Fatalf("assistant tool call = %#v", call)
		}
		var callArgs map[string]any
		if err := json.Unmarshal([]byte(call.Function.Arguments), &callArgs); err != nil {
			t.Fatalf("assistant tool call arguments are not JSON: %v", err)
		}
		if callArgs["query"] != "previous" {
			t.Fatalf("assistant tool call arguments = %#v, want previous query", callArgs)
		}
		if payload.Messages[3].Role != "tool" || payload.Messages[3].ToolCallID != "call-prev" || payload.Messages[3].Name != "search" {
			t.Fatalf("tool result message = %#v", payload.Messages[3])
		}
		if len(payload.Tools) != 1 {
			t.Fatalf("tools = %#v, want one", payload.Tools)
		}
		tool := payload.Tools[0]
		if tool.Type != "function" || tool.Function.Name != "search" || tool.Function.Description != "Search documents" {
			t.Fatalf("tool definition = %#v", tool)
		}
		wantParameters := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query"},
				"limit": map[string]any{"type": "integer"},
			},
			"required": []any{"query"},
		}
		if !mapsEqual(tool.Function.Parameters, wantParameters) {
			t.Fatalf("tool parameters = %#v, want %#v", tool.Function.Parameters, wantParameters)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Done"}}]}`)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "test-model",
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
				Content: "",
				ToolCalls: []ToolCall{{
					ID:        "call-prev",
					Name:      "search",
					Arguments: map[string]any{"query": "previous"},
				}},
			},
			{Role: RoleTool, Name: "search", ToolCallID: "call-prev", Content: "Previous result"},
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
	if !sawRequest {
		t.Fatal("server did not receive a request")
	}
	if response.Message.Role != RoleAssistant || response.Message.Content != "Done" {
		t.Fatalf("response message = %#v, want assistant Done", response.Message)
	}
}

func TestOpenAICompatibleModelUsesFullChatCompletionsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/chat/completions" {
			t.Fatalf("path = %q, want /custom/chat/completions", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
		BaseURL: server.URL + "/custom/chat/completions",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := model.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompatibleModelParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call-1",
						"type": "function",
						"function": {
							"name": "search",
							"arguments": "{\"query\":\"docs\",\"limit\":3}"
						}
					}]
				}
			}]
		}`)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
		BaseURL: server.URL,
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Role != RoleAssistant {
		t.Fatalf("message role = %q, want assistant", response.Message.Role)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one", response.ToolCalls)
	}
	call := response.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "search" {
		t.Fatalf("tool call = %#v, want search call-1", call)
	}
	if call.Arguments["query"] != "docs" || call.Arguments["limit"] != float64(3) {
		t.Fatalf("tool call arguments = %#v, want query docs and limit 3", call.Arguments)
	}
	if len(response.Message.ToolCalls) != 1 {
		t.Fatalf("message tool calls = %#v, want mirrored tool call", response.Message.ToolCalls)
	}
}

func TestOpenAICompatibleModelParsesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"choices": [{"message": {"role": "assistant", "content": "Done"}}],
			"usage": {
				"prompt_tokens": 11,
				"completion_tokens": 7,
				"total_tokens": 18
			}
		}`)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
		BaseURL: server.URL,
		Model:   "test-model",
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
	want := TokenUsage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18}
	if response.Usage != want {
		t.Fatalf("usage = %#v, want %#v", response.Usage, want)
	}
}

func TestOpenAICompatibleModelRejectsEmptyOrInvalidToolCallArguments(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		want      string
	}{
		{name: "empty", arguments: "", want: "empty"},
		{name: "invalid", arguments: "{not-json", want: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"search","arguments":` + strconvQuote(tt.arguments) + `}}]}}]}`
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()

			model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
				BaseURL: server.URL,
				Model:   "test-model",
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = model.Generate(context.Background(), ModelRequest{})
			if err == nil {
				t.Fatal("Generate returned nil error, want tool call argument error")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "tool call arguments") {
				t.Fatalf("error = %v, want %q tool call arguments error", err, tt.want)
			}
		})
	}
}

func TestOpenAICompatibleModelReturnsNon2xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q, want Bearer test-key", got)
		}
		http.Error(w, "provider rejected request", http.StatusUnauthorized)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = model.Generate(context.Background(), ModelRequest{})
	if err == nil {
		t.Fatal("Generate returned nil error, want non-2xx error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "provider rejected request") {
		t.Fatalf("error = %v, want status and response body summary", err)
	}
	if strings.Contains(err.Error(), "Bearer test-key") {
		t.Fatalf("error exposed authorization header: %v", err)
	}
}

func TestNewOpenAICompatibleModelValidatesRequiredConfig(t *testing.T) {
	tests := []struct {
		name   string
		config OpenAICompatibleConfig
	}{
		{name: "base URL", config: OpenAICompatibleConfig{Model: "test-model"}},
		{name: "model", config: OpenAICompatibleConfig{BaseURL: "https://example.com/v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOpenAICompatibleModel(tt.config)
			if err == nil {
				t.Fatal("NewOpenAICompatibleModel returned nil error, want validation error")
			}
		})
	}
}

type openAIChatCompletionRequestForTest struct {
	Model    string                         `json:"model"`
	Messages []openAIChatMessageForTest     `json:"messages"`
	Tools    []openAIChatCompletionToolTest `json:"tools"`
}

type openAIChatMessageForTest struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content"`
	Name       string                   `json:"name"`
	ToolCallID string                   `json:"tool_call_id"`
	ToolCalls  []openAIChatToolCallTest `json:"tool_calls"`
}

type openAIChatCompletionToolTest struct {
	Type     string                        `json:"type"`
	Function openAIChatToolFunctionForTest `json:"function"`
}

type openAIChatToolCallTest struct {
	ID       string                        `json:"id"`
	Type     string                        `json:"type"`
	Function openAIChatToolFunctionForTest `json:"function"`
}

type openAIChatToolFunctionForTest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Arguments   string         `json:"arguments"`
	Parameters  map[string]any `json:"parameters"`
}

func mapsEqual(got, want map[string]any) bool {
	gotJSON, err := json.Marshal(got)
	if err != nil {
		return false
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return false
	}
	return string(gotJSON) == string(wantJSON)
}

func strconvQuote(value string) string {
	quoted, err := json.Marshal(value)
	if err != nil {
		panic(errors.New("failed to quote test string"))
	}
	return string(quoted)
}
