package agent

import (
	"github.com/cubence/cube-agent-sdk/internal/providers/anthropic"
	"github.com/cubence/cube-agent-sdk/internal/providers/openai"
)

type OpenAICompatibleConfig = openai.OpenAICompatibleConfig
type OpenAICompatibleModel = openai.OpenAICompatibleModel

// NewOpenAICompatibleModel creates a Model from OpenAI-compatible endpoint
// configuration. BaseURL may be a root URL or a full /chat/completions URL.
func NewOpenAICompatibleModel(config OpenAICompatibleConfig) (*OpenAICompatibleModel, error) {
	return openai.NewOpenAICompatibleModel(config)
}

type AnthropicMessagesConfig = anthropic.AnthropicMessagesConfig
type AnthropicMessagesModel = anthropic.AnthropicMessagesModel

const (
	defaultAnthropicVersion   = anthropic.DefaultVersion
	defaultAnthropicMaxTokens = anthropic.DefaultMaxTokens
)

// NewAnthropicMessagesModel creates a Model from Anthropic Messages API configuration.
// BaseURL may be a root URL, /v1 URL, or full /v1/messages URL.
func NewAnthropicMessagesModel(config AnthropicMessagesConfig) (*AnthropicMessagesModel, error) {
	return anthropic.NewAnthropicMessagesModel(config)
}
