package agent

import "context"

// ToolWithSafety wraps an existing tool with SDK-enforced safety metadata. It
// is useful for MCP, file, network, or third-party tools that cannot implement
// ToolSafetyProvider directly. Non-zero values in safety override the wrapped
// tool's own safety settings.
func ToolWithSafety(tool Tool, safety ToolSafety) Tool {
	if tool == nil {
		return nil
	}
	return toolSafetyWrapper{tool: tool, safety: safety}
}

type toolSafetyWrapper struct {
	tool   Tool
	safety ToolSafety
}

func (w toolSafetyWrapper) Name() string {
	return w.tool.Name()
}

func (w toolSafetyWrapper) Description() string {
	return w.tool.Description()
}

func (w toolSafetyWrapper) ParametersSchema() *ToolParametersSchema {
	provider, ok := w.tool.(ToolParametersSchemaProvider)
	if !ok {
		return nil
	}
	return provider.ParametersSchema()
}

func (w toolSafetyWrapper) Risk() ToolRisk {
	return w.ToolSafety().Risk
}

func (w toolSafetyWrapper) ToolSafety() ToolSafety {
	base := toolSafety(w.tool)
	return mergeToolSafety(base, w.safety)
}

func (w toolSafetyWrapper) Call(ctx context.Context, call ToolCall) (ToolResult, error) {
	return w.tool.Call(ctx, call)
}

func mergeToolSafety(base ToolSafety, override ToolSafety) ToolSafety {
	base = cloneToolSafety(base)
	override = cloneToolSafety(override)
	if override.Risk != "" {
		base.Risk = override.Risk
	}
	if override.Timeout > 0 {
		base.Timeout = override.Timeout
	}
	if override.MaxConcurrency > 0 {
		base.MaxConcurrency = override.MaxConcurrency
	}
	if override.MaxResultBytes > 0 {
		base.MaxResultBytes = override.MaxResultBytes
	}
	if len(override.Scopes) > 0 {
		base.Scopes = override.Scopes
	}
	if override.BusinessReason != "" {
		base.BusinessReason = override.BusinessReason
	}
	return cloneToolSafety(base)
}
