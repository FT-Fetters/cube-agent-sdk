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

func TestOpenAIResponsesModelSendsResponsesRequest(t *testing.T) {
	ctx := context.Background()
	store := false
	var sawRequest bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}

		var payload openAIResponsesRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "test-model" {
			t.Fatalf("model = %q, want test-model", payload.Model)
		}
		if payload.Instructions != "You are concise." {
			t.Fatalf("instructions = %q, want system prompt", payload.Instructions)
		}
		if payload.MaxOutputTokens != 128 {
			t.Fatalf("max output tokens = %d, want 128", payload.MaxOutputTokens)
		}
		if payload.Store == nil || *payload.Store {
			t.Fatalf("store = %#v, want false", payload.Store)
		}
		if len(payload.Input) != 4 {
			t.Fatalf("input = %#v, want user, assistant text, function call, and function output", payload.Input)
		}
		assertResponseMessageInput(t, payload.Input[0], "user", "input_text", "Find docs")
		assertResponseMessageInput(t, payload.Input[1], "assistant", "output_text", "Use search.")
		assertResponseFunctionCallInput(t, payload.Input[2], "call-prev", "search", map[string]any{"query": "previous"})
		assertResponseFunctionOutputInput(t, payload.Input[3], "call-prev", "Previous result")

		if len(payload.Tools) != 1 {
			t.Fatalf("tools = %#v, want one", payload.Tools)
		}
		tool := payload.Tools[0]
		if tool.Type != "function" || tool.Name != "search" || tool.Description != "Search documents" {
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
		if !mapsEqual(tool.Parameters, wantParameters) {
			t.Fatalf("tool parameters = %#v, want %#v", tool.Parameters, wantParameters)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"}]}]}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-key",
		Model:      "test-model",
		HTTPClient: server.Client(),
		MaxTokens:  128,
		Store:      &store,
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
				Content: "Use search.",
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

func TestOpenAIResponsesModelUsesFullResponsesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/responses" {
			t.Fatalf("path = %q, want /custom/responses", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL: server.URL + "/custom/responses",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := model.Generate(context.Background(), ModelRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIResponsesModelParsesToolCallsAndReplaysRawOutput(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			_, _ = io.WriteString(w, `{
				"output": [
					{"id":"rs_1","type":"reasoning","summary":[]},
					{"id":"fc_1","type":"function_call","call_id":"call-1","name":"search","arguments":"{\"query\":\"docs\",\"limit\":3}","status":"completed"}
				]
			}`)
		case 2:
			var payload openAIResponsesRequestForTest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Input) != 3 {
				t.Fatalf("input = %#v, want preserved reasoning, function call, and function output", payload.Input)
			}
			if got := stringValue(payload.Input[0], "type"); got != "reasoning" {
				t.Fatalf("first replayed item type = %q, want reasoning", got)
			}
			assertResponseFunctionCallInput(t, payload.Input[1], "call-1", "search", map[string]any{"query": "docs", "limit": float64(3)})
			assertResponseFunctionOutputInput(t, payload.Input[2], "call-1", "Search result")
			_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Final"}]}]}`)
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL: server.URL,
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Message.Role != RoleAssistant {
		t.Fatalf("message role = %q, want assistant", first.Message.Role)
	}
	if len(first.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one", first.ToolCalls)
	}
	call := first.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "search" {
		t.Fatalf("tool call = %#v, want search call-1", call)
	}
	if call.Arguments["query"] != "docs" || call.Arguments["limit"] != float64(3) {
		t.Fatalf("tool call arguments = %#v, want query docs and limit 3", call.Arguments)
	}
	if len(first.Message.ToolCalls) != 1 {
		t.Fatalf("message tool calls = %#v, want mirrored tool call", first.Message.ToolCalls)
	}

	second, err := model.Generate(context.Background(), ModelRequest{
		Messages: []Message{
			first.Message,
			{Role: RoleTool, Name: "search", ToolCallID: "call-1", Content: "Search result"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Message.Content != "Final" {
		t.Fatalf("second message content = %q, want Final", second.Message.Content)
	}
}

func TestOpenAIResponsesModelParsesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"output": [
				{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Done"}]}
			],
			"usage": {
				"input_tokens": 13,
				"output_tokens": 5,
				"total_tokens": 18
			}
		}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
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
	want := TokenUsage{InputTokens: 13, OutputTokens: 5, TotalTokens: 18}
	if response.Usage != want {
		t.Fatalf("usage = %#v, want %#v", response.Usage, want)
	}
}

func TestOpenAIResponsesModelStreamsDeltasDoneAndUsage(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		var payload openAIResponsesRequestForTest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !payload.Stream {
			t.Fatalf("stream = false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"Hel"}`)
		writeSSEEvent(t, w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"lo"}`)
		writeSSEEvent(t, w, "response.completed", `{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18}}}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL: server.URL,
		Model:   "test-model",
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
	assertProviderStreamSuccess(t, got, "Hel", "lo", "Hello", TokenUsage{InputTokens: 13, OutputTokens: 5, TotalTokens: 18})
}

func TestOpenAIResponsesModelStreamMapsFunctionCallEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call-1","name":"search","arguments":""}}`)
		writeSSEEvent(t, w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":`+strconvQuote(`{"query"`)+`}`)
		writeSSEEvent(t, w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":`+strconvQuote(`:"docs","limit":3}`)+`}`)
		writeSSEEvent(t, w, "response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":`+strconvQuote(`{"query":"docs","limit":3}`)+`}`)
		writeSSEEvent(t, w, "response.completed", `{"type":"response.completed","response":{"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18}}}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL: server.URL,
		Model:   "test-model",
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
	assertStreamToolCallBoundary(t, got[0], StreamEventToolCallStart, 0, "call-1", "search")
	assertStreamToolCallBoundary(t, got[1], StreamEventToolCallDone, 0, "call-1", "search")
	assertStreamDoneToolCall(t, got[2], "", "call-1", "search", map[string]any{"query": "docs", "limit": float64(3)}, TokenUsage{InputTokens: 13, OutputTokens: 5, TotalTokens: 18})
	if got[2].Finish.Reason != "completed" {
		t.Fatalf("done finish metadata = %#v, want completed", got[2].Finish)
	}
}

func TestOpenAIResponsesModelStreamRejectsInvalidFunctionCallArgumentsSafely(t *testing.T) {
	const rawProviderPayload = "secret-raw-provider-payload"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call-1","name":"search","arguments":""}}`)
		writeSSEEvent(t, w, "response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":`+strconvQuote(rawProviderPayload)+`}`)
		writeSSEEvent(t, w, "response.completed", `{"type":"response.completed","response":{"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18}}}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL: server.URL,
		Model:   "test-model",
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
		Provider:     "openai-responses",
		EndpointHost: server.Listener.Addr().String(),
	}, ModelErrorSubcategoryDecodeError)
	if strings.Contains(got[0].Error.Error(), rawProviderPayload) {
		t.Fatalf("stream error exposed unsafe provider detail: %v", got[0].Error)
	}
}

func TestOpenAIResponsesModelStreamEmitsProviderErrorWithDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEEvent(t, w, "error", `{"type":"error","error":{"code":"rate_limit_exceeded","message":"provider secret payload"}}`)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
		BaseURL: server.URL,
		Model:   "test-model",
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
		Provider:     "openai-responses",
		EndpointHost: server.Listener.Addr().String(),
	}, ModelErrorSubcategoryUnknown)
	if strings.Contains(got[0].Error.Error(), "provider secret payload") {
		t.Fatalf("stream error exposed unsafe provider detail: %v", got[0].Error)
	}
}

func TestOpenAIResponsesModelRejectsEmptyOrInvalidToolCallArguments(t *testing.T) {
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
				response := `{"output":[{"type":"function_call","call_id":"call-1","name":"search","arguments":` + strconvQuote(tt.arguments) + `}]}`
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()

			model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
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

func TestOpenAIResponsesModelReturnsNon2xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header = %q, want Bearer test-key", got)
		}
		w.Header().Set("X-Request-Id", "responses-request-1")
		http.Error(w, "provider rejected test-key", http.StatusUnauthorized)
	}))
	defer server.Close()

	model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
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
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want status", err)
	}
	if strings.Contains(err.Error(), "provider rejected") || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("error exposed unsafe provider detail: %v", err)
	}
	want := ProviderDiagnostics{
		Provider:     "openai-responses",
		HTTPStatus:   http.StatusUnauthorized,
		EndpointHost: server.Listener.Addr().String(),
		RequestID:    "responses-request-1",
	}
	got, ok := ProviderDiagnosticsFromError(err)
	if !ok || got != want {
		t.Fatalf("provider diagnostics = %#v/%t, want %#v/true", got, ok, want)
	}
}

func TestNewOpenAIResponsesModelValidatesRequiredConfig(t *testing.T) {
	tests := []struct {
		name   string
		config OpenAIResponsesConfig
	}{
		{name: "base URL", config: OpenAIResponsesConfig{Model: "test-model"}},
		{name: "model", config: OpenAIResponsesConfig{BaseURL: "https://example.com/v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOpenAIResponsesModel(tt.config)
			if err == nil {
				t.Fatal("NewOpenAIResponsesModel returned nil error, want validation error")
			}
		})
	}
}

type openAIResponsesRequestForTest struct {
	Model           string                       `json:"model"`
	Instructions    string                       `json:"instructions"`
	Input           []map[string]any             `json:"input"`
	Tools           []openAIResponsesToolForTest `json:"tools"`
	MaxOutputTokens int                          `json:"max_output_tokens"`
	Store           *bool                        `json:"store"`
	Stream          bool                         `json:"stream"`
}

type openAIResponsesToolForTest struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func assertResponseMessageInput(t *testing.T, item map[string]any, role string, contentType string, text string) {
	t.Helper()
	if got := stringValue(item, "type"); got != "message" {
		t.Fatalf("input item type = %q, want message", got)
	}
	if got := stringValue(item, "role"); got != role {
		t.Fatalf("input item role = %q, want %s", got, role)
	}
	content, ok := item["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("input item content = %#v, want one content block", item["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content block = %#v, want object", content[0])
	}
	if got := stringValue(block, "type"); got != contentType {
		t.Fatalf("content block type = %q, want %s", got, contentType)
	}
	if got := stringValue(block, "text"); got != text {
		t.Fatalf("content block text = %q, want %q", got, text)
	}
}

func assertResponseFunctionCallInput(t *testing.T, item map[string]any, callID string, name string, arguments map[string]any) {
	t.Helper()
	if got := stringValue(item, "type"); got != "function_call" {
		t.Fatalf("input item type = %q, want function_call", got)
	}
	if got := stringValue(item, "call_id"); got != callID {
		t.Fatalf("function call ID = %q, want %s", got, callID)
	}
	if got := stringValue(item, "name"); got != name {
		t.Fatalf("function call name = %q, want %s", got, name)
	}
	var gotArgs map[string]any
	if err := json.Unmarshal([]byte(stringValue(item, "arguments")), &gotArgs); err != nil {
		t.Fatalf("function call arguments are not JSON: %v", err)
	}
	if !mapsEqual(gotArgs, arguments) {
		t.Fatalf("function call arguments = %#v, want %#v", gotArgs, arguments)
	}
}

func assertResponseFunctionOutputInput(t *testing.T, item map[string]any, callID string, output string) {
	t.Helper()
	if got := stringValue(item, "type"); got != "function_call_output" {
		t.Fatalf("input item type = %q, want function_call_output", got)
	}
	if got := stringValue(item, "call_id"); got != callID {
		t.Fatalf("function output call ID = %q, want %s", got, callID)
	}
	if got := stringValue(item, "output"); got != output {
		t.Fatalf("function output = %q, want %q", got, output)
	}
}

func stringValue(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}
