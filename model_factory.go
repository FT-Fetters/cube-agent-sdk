package agent

import (
	"errors"
	"fmt"
	"net/http"
)

// ModelAPIType selects the provider wire protocol used by NewModel.
type ModelAPIType string

const (
	ModelAPIOpenAICompatible  ModelAPIType = "openai-compatible"
	ModelAPIAnthropicMessages ModelAPIType = "anthropic-messages"
)

var ErrModelAPIUnsupported = errors.New("agent: unsupported model api type")

// ModelConfig is the shared provider configuration accepted by NewModel.
type ModelConfig struct {
	APIType          ModelAPIType
	BaseURL          string
	APIKey           string
	Model            string
	HTTPClient       *http.Client
	MaxTokens        int
	AnthropicVersion string
}

// NewModel creates a model adapter for the requested provider API type.
func NewModel(config ModelConfig) (Model, error) {
	switch config.APIType {
	case ModelAPIOpenAICompatible:
		return NewOpenAICompatibleModel(OpenAICompatibleConfig{
			BaseURL:    config.BaseURL,
			APIKey:     config.APIKey,
			Model:      config.Model,
			HTTPClient: config.HTTPClient,
		})
	case ModelAPIAnthropicMessages:
		return NewAnthropicMessagesModel(AnthropicMessagesConfig{
			BaseURL:          config.BaseURL,
			APIKey:           config.APIKey,
			Model:            config.Model,
			HTTPClient:       config.HTTPClient,
			MaxTokens:        config.MaxTokens,
			AnthropicVersion: config.AnthropicVersion,
		})
	default:
		return nil, fmt.Errorf("%w: %s", ErrModelAPIUnsupported, config.APIType)
	}
}
