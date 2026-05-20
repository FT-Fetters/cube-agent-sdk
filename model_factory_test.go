package agent

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewModelCreatesConfiguredAPIType(t *testing.T) {
	openAIServer := httptest.NewServer(newOpenAICompatibleTestHandler(t))
	defer openAIServer.Close()

	openAIResponsesServer := httptest.NewServer(newOpenAIResponsesTestHandler(t))
	defer openAIResponsesServer.Close()

	anthropicServer := httptest.NewServer(newAnthropicMessagesTestHandler(t))
	defer anthropicServer.Close()

	tests := []struct {
		name    string
		config  ModelConfig
		wantTyp any
	}{
		{
			name: "openai compatible",
			config: ModelConfig{
				APIType:    ModelAPIOpenAICompatible,
				BaseURL:    openAIServer.URL,
				APIKey:     "test-key",
				Model:      "openai-test-model",
				HTTPClient: openAIServer.Client(),
			},
			wantTyp: &OpenAICompatibleModel{},
		},
		{
			name: "anthropic messages",
			config: ModelConfig{
				APIType:    ModelAPIAnthropicMessages,
				BaseURL:    anthropicServer.URL,
				APIKey:     "test-key",
				Model:      "claude-test-model",
				HTTPClient: anthropicServer.Client(),
			},
			wantTyp: &AnthropicMessagesModel{},
		},
		{
			name: "openai responses",
			config: ModelConfig{
				APIType:    ModelAPIOpenAIResponses,
				BaseURL:    openAIResponsesServer.URL,
				APIKey:     "test-key",
				Model:      "openai-responses-test-model",
				HTTPClient: openAIResponsesServer.Client(),
			},
			wantTyp: &OpenAIResponsesModel{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := NewModel(tt.config)
			if err != nil {
				t.Fatal(err)
			}
			switch tt.wantTyp.(type) {
			case *OpenAICompatibleModel:
				if _, ok := model.(*OpenAICompatibleModel); !ok {
					t.Fatalf("model = %T, want *OpenAICompatibleModel", model)
				}
			case *AnthropicMessagesModel:
				if _, ok := model.(*AnthropicMessagesModel); !ok {
					t.Fatalf("model = %T, want *AnthropicMessagesModel", model)
				}
			case *OpenAIResponsesModel:
				if _, ok := model.(*OpenAIResponsesModel); !ok {
					t.Fatalf("model = %T, want *OpenAIResponsesModel", model)
				}
			}
		})
	}
}

func newOpenAIResponsesTestHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatal("missing authorization header")
		}
		_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello from OpenAI Responses"}]}]}`)
	}
}

func TestNewModelRejectsUnsupportedAPIType(t *testing.T) {
	_, err := NewModel(ModelConfig{
		APIType: "unsupported",
		BaseURL: "https://example.invalid",
		Model:   "test-model",
	})
	if !errors.Is(err, ErrModelAPIUnsupported) {
		t.Fatalf("err = %v, want ErrModelAPIUnsupported", err)
	}
}
