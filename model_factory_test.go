package agent

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestNewModelCreatesConfiguredAPIType(t *testing.T) {
	openAIServer := httptest.NewServer(newOpenAICompatibleTestHandler(t))
	defer openAIServer.Close()

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
			}
		})
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
