package agent

import "github.com/cubence/cube-agent-sdk/internal/core"

type ModelCapability = core.ModelCapability
type ModelCapabilities = core.ModelCapabilities
type ModelCapabilityRequirement = core.ModelCapabilityRequirement
type ModelCapabilitiesProvider = core.ModelCapabilitiesProvider
type CapabilityMismatchError = core.CapabilityMismatchError

const (
	ModelCapabilityTools             = core.ModelCapabilityTools
	ModelCapabilityStreaming         = core.ModelCapabilityStreaming
	ModelCapabilityJSONMode          = core.ModelCapabilityJSONMode
	ModelCapabilityStructuredOutput  = core.ModelCapabilityStructuredOutput
	ModelCapabilityReasoningMetadata = core.ModelCapabilityReasoningMetadata
	ModelCapabilityParallelToolCalls = core.ModelCapabilityParallelToolCalls
	ModelCapabilityMCPServerMetadata = core.ModelCapabilityMCPServerMetadata
	ModelCapabilityModelHandledMCP   = core.ModelCapabilityModelHandledMCP
	ModelCapabilityTokenUsage        = core.ModelCapabilityTokenUsage
)

var ErrCapabilityMismatch = core.ErrCapabilityMismatch

// CapabilitiesOf returns a model's declared capabilities when it implements
// ModelCapabilitiesProvider. Models without declarations return ok=false so
// existing custom implementations remain backward-compatible.
func CapabilitiesOf(model Model) (ModelCapabilities, bool) {
	return core.CapabilitiesOf(model)
}

// ModelSatisfiesCapabilities reports whether a model declares all required
// capabilities. Models without declarations return false.
func ModelSatisfiesCapabilities(model Model, required ModelCapabilityRequirement) bool {
	return core.ModelSatisfiesCapabilities(model, required)
}

// SelectModelByCapabilities returns the first model that declares all required
// capabilities. Models without declarations are skipped.
func SelectModelByCapabilities(models []Model, required ModelCapabilityRequirement) (Model, ModelCapabilities, bool) {
	return core.SelectModelByCapabilities(models, required)
}
