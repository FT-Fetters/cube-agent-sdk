package agent

import "github.com/cubence/cube-agent-sdk/internal/schema"

func toolParametersSchema(tool Tool) *ToolParametersSchema {
	provider, ok := tool.(ToolParametersSchemaProvider)
	if !ok {
		return nil
	}
	return cloneToolParametersSchema(provider.ParametersSchema())
}

func cloneToolParametersSchema(parameters *ToolParametersSchema) *ToolParametersSchema {
	return schema.Clone(parameters)
}

func validateToolCallArguments(toolName string, arguments map[string]any, parameters *ToolParametersSchema) error {
	return schema.ValidateToolCallArguments(toolName, arguments, parameters)
}
