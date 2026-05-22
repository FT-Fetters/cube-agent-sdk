package agent

import "context"

func (a *Agent) checkModelCapabilities(ctx context.Context, streaming bool) error {
	a.mu.Lock()
	agentID := a.id
	model := a.model
	hasTools := len(a.toolOrder) > 0
	hasMCPServers := len(a.mcpServers) > 0
	a.mu.Unlock()

	capabilities, ok := CapabilitiesOf(model)
	if !ok {
		return nil
	}
	if streaming && !capabilities.Streaming {
		return a.newCapabilityMismatchError(ctx, agentID, ModelCapabilityStreaming, "RunStream requested", capabilities)
	}
	if hasTools && !capabilities.Tools {
		return a.newCapabilityMismatchError(ctx, agentID, ModelCapabilityTools, "tools configured", capabilities)
	}
	if hasMCPServers && !capabilities.MCPServerMetadata {
		return a.newCapabilityMismatchError(ctx, agentID, ModelCapabilityMCPServerMetadata, "MCP servers configured", capabilities)
	}
	return nil
}

func (a *Agent) newCapabilityMismatchError(ctx context.Context, agentID string, capability ModelCapability, requirement string, capabilities ModelCapabilities) error {
	wrapped := agentError(ErrorCategoryConfig, "model.capability", &CapabilityMismatchError{
		Capability:   capability,
		Requirement:  requirement,
		Capabilities: capabilities,
	})
	wrapped.AgentID = agentID
	wrapped.RunID = runIDFromContext(ctx)
	setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
	wrapped.Round = 1
	return wrapped
}
