package core

import (
	"errors"
	"fmt"
	"strings"
)

// ModelCapability identifies one model or adapter behavior that applications
// can inspect before choosing a model.
type ModelCapability string

const (
	ModelCapabilityTools             ModelCapability = "tools"
	ModelCapabilityStreaming         ModelCapability = "streaming"
	ModelCapabilityJSONMode          ModelCapability = "json_mode"
	ModelCapabilityStructuredOutput  ModelCapability = "structured_output"
	ModelCapabilityReasoningMetadata ModelCapability = "reasoning_metadata"
	ModelCapabilityParallelToolCalls ModelCapability = "parallel_tool_calls"
	ModelCapabilityMCPServerMetadata ModelCapability = "mcp_server_metadata"
	ModelCapabilityModelHandledMCP   ModelCapability = "model_handled_mcp"
	ModelCapabilityTokenUsage        ModelCapability = "token_usage"
)

// ModelCapabilities describes protocol-level behavior supported by a model
// adapter. These flags are intentionally conservative: a true value means the
// SDK adapter can map the capability, not that every remote model behind that
// protocol supports it.
type ModelCapabilities struct {
	// Provider is a safe provider or adapter identifier, such as "openai-responses".
	Provider string
	// APIType is the SDK provider API type when one is known.
	APIType string
	// Model is the configured remote model identifier. It should not contain secrets.
	Model string

	// Tools reports whether ModelRequest.Tools are sent to the provider.
	Tools bool
	// Streaming reports whether the model can be used through StreamModel.
	Streaming bool
	// JSONMode reports whether the adapter exposes a provider JSON mode switch.
	JSONMode bool
	// StructuredOutput reports whether the adapter exposes provider-native structured output.
	StructuredOutput bool
	// ReasoningMetadata reports whether provider reasoning metadata is preserved when present.
	ReasoningMetadata bool
	// ParallelToolCalls reports whether multiple tool calls in one model response are mapped.
	ParallelToolCalls bool
	// MCPServerMetadata reports whether ModelRequest.MCPServers are consumed by the adapter.
	MCPServerMetadata bool
	// ModelHandledMCP reports whether the remote model/provider handles MCP server access.
	ModelHandledMCP bool
	// TokenUsage reports whether provider token usage is mapped when the provider returns it.
	TokenUsage bool
}

// Supports reports whether all required capabilities are declared by c.
func (c ModelCapabilities) Supports(required ModelCapabilityRequirement) bool {
	return len(c.Missing(required)) == 0
}

// Missing returns the required capabilities that c does not declare.
func (c ModelCapabilities) Missing(required ModelCapabilityRequirement) []ModelCapability {
	var missing []ModelCapability
	if required.Tools && !c.Tools {
		missing = append(missing, ModelCapabilityTools)
	}
	if required.Streaming && !c.Streaming {
		missing = append(missing, ModelCapabilityStreaming)
	}
	if required.JSONMode && !c.JSONMode {
		missing = append(missing, ModelCapabilityJSONMode)
	}
	if required.StructuredOutput && !c.StructuredOutput {
		missing = append(missing, ModelCapabilityStructuredOutput)
	}
	if required.ReasoningMetadata && !c.ReasoningMetadata {
		missing = append(missing, ModelCapabilityReasoningMetadata)
	}
	if required.ParallelToolCalls && !c.ParallelToolCalls {
		missing = append(missing, ModelCapabilityParallelToolCalls)
	}
	if required.MCPServerMetadata && !c.MCPServerMetadata {
		missing = append(missing, ModelCapabilityMCPServerMetadata)
	}
	if required.ModelHandledMCP && !c.ModelHandledMCP {
		missing = append(missing, ModelCapabilityModelHandledMCP)
	}
	if required.TokenUsage && !c.TokenUsage {
		missing = append(missing, ModelCapabilityTokenUsage)
	}
	return missing
}

// ModelCapabilityRequirement describes capabilities an application or agent
// needs before selecting or calling a model.
type ModelCapabilityRequirement struct {
	Tools             bool
	Streaming         bool
	JSONMode          bool
	StructuredOutput  bool
	ReasoningMetadata bool
	ParallelToolCalls bool
	MCPServerMetadata bool
	ModelHandledMCP   bool
	TokenUsage        bool
}

// ModelCapabilitiesProvider is implemented by models that declare their
// protocol-level adapter capabilities.
type ModelCapabilitiesProvider interface {
	Capabilities() ModelCapabilities
}

// ErrCapabilityMismatch marks a pre-run model capability incompatibility.
var ErrCapabilityMismatch = errors.New("agent: model capability mismatch")

// CapabilityMismatchError describes one missing capability without including
// prompts, messages, tool arguments, credentials, or raw provider payloads.
type CapabilityMismatchError struct {
	Capability   ModelCapability
	Requirement  string
	Capabilities ModelCapabilities
}

func (e *CapabilityMismatchError) Error() string {
	if e == nil {
		return ""
	}
	capability := strings.TrimSpace(string(e.Capability))
	if capability == "" {
		capability = "capability"
	}
	var details []string
	if requirement := strings.TrimSpace(e.Requirement); requirement != "" {
		details = append(details, "required="+requirement)
	}
	capabilities := normalizeModelCapabilities(e.Capabilities)
	if capabilities.Provider != "" {
		details = append(details, "provider="+capabilities.Provider)
	}
	if capabilities.APIType != "" {
		details = append(details, "api_type="+capabilities.APIType)
	}
	if capabilities.Model != "" {
		details = append(details, "model="+capabilities.Model)
	}
	if len(details) == 0 {
		return fmt.Sprintf("%s: %s unsupported", ErrCapabilityMismatch, capability)
	}
	return fmt.Sprintf("%s: %s unsupported (%s)", ErrCapabilityMismatch, capability, strings.Join(details, ", "))
}

func (e *CapabilityMismatchError) Unwrap() error {
	return ErrCapabilityMismatch
}

// CapabilitiesOf returns a model's declared capabilities when it implements
// ModelCapabilitiesProvider. Models without declarations return ok=false so
// existing custom implementations remain backward-compatible.
func CapabilitiesOf(model Model) (ModelCapabilities, bool) {
	provider, ok := model.(ModelCapabilitiesProvider)
	if !ok {
		return ModelCapabilities{}, false
	}
	return normalizeModelCapabilities(provider.Capabilities()), true
}

// ModelSatisfiesCapabilities reports whether a model declares all required
// capabilities. Models without declarations return false.
func ModelSatisfiesCapabilities(model Model, required ModelCapabilityRequirement) bool {
	capabilities, ok := CapabilitiesOf(model)
	return ok && capabilities.Supports(required)
}

// SelectModelByCapabilities returns the first model that declares all required
// capabilities. Models without declarations are skipped.
func SelectModelByCapabilities(models []Model, required ModelCapabilityRequirement) (Model, ModelCapabilities, bool) {
	for _, model := range models {
		capabilities, ok := CapabilitiesOf(model)
		if !ok || !capabilities.Supports(required) {
			continue
		}
		return model, capabilities, true
	}
	return nil, ModelCapabilities{}, false
}

func normalizeModelCapabilities(capabilities ModelCapabilities) ModelCapabilities {
	capabilities.Provider = strings.TrimSpace(capabilities.Provider)
	capabilities.APIType = strings.TrimSpace(capabilities.APIType)
	capabilities.Model = strings.TrimSpace(capabilities.Model)
	return capabilities
}
