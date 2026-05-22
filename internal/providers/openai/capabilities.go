package openai

import "github.com/cubence/cube-agent-sdk/internal/core"

// Capabilities returns protocol-level support declared by the chat completions adapter.
func (m *OpenAICompatibleModel) Capabilities() core.ModelCapabilities {
	capabilities := core.ModelCapabilities{
		Provider:          providerOpenAICompatible,
		APIType:           providerOpenAICompatible,
		Tools:             true,
		Streaming:         true,
		ParallelToolCalls: true,
		TokenUsage:        true,
	}
	if m != nil {
		capabilities.Model = m.model
	}
	return capabilities
}

// Capabilities returns protocol-level support declared by the Responses adapter.
func (m *OpenAIResponsesModel) Capabilities() core.ModelCapabilities {
	capabilities := core.ModelCapabilities{
		Provider:          providerOpenAIResponses,
		APIType:           providerOpenAIResponses,
		Tools:             true,
		Streaming:         true,
		ParallelToolCalls: true,
		ReasoningMetadata: true,
		TokenUsage:        true,
	}
	if m != nil {
		capabilities.Model = m.model
	}
	return capabilities
}
