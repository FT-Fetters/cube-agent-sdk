package anthropic

import "github.com/cubence/cube-agent-sdk/internal/core"

// Capabilities returns protocol-level support declared by the Messages adapter.
func (m *AnthropicMessagesModel) Capabilities() core.ModelCapabilities {
	capabilities := core.ModelCapabilities{
		Provider:          providerAnthropicMessages,
		APIType:           providerAnthropicMessages,
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
