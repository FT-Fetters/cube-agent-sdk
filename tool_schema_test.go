package agent

import "testing"

func TestToolSchemaHashIncludesSupportedConstraints(t *testing.T) {
	base := &ToolParametersSchema{
		Type: SchemaTypeObject,
		Properties: map[string]ToolParametersSchema{
			"value": {Type: SchemaTypeString},
		},
	}
	baseHash := testToolSchemaHash(base)

	tests := []struct {
		name   string
		mutate func(*ToolParametersSchema)
	}{
		{
			name: "enum",
			mutate: func(schema *ToolParametersSchema) {
				property := schema.Properties["value"]
				property.Enum = []any{"a", "b"}
				schema.Properties["value"] = property
			},
		},
		{
			name: "default",
			mutate: func(schema *ToolParametersSchema) {
				property := schema.Properties["value"]
				property.Default = "a"
				schema.Properties["value"] = property
			},
		},
		{
			name: "numeric min max",
			mutate: func(schema *ToolParametersSchema) {
				minimum := 1.0
				maximum := 2.0
				property := schema.Properties["value"]
				property.Minimum = &minimum
				property.Maximum = &maximum
				schema.Properties["value"] = property
			},
		},
		{
			name: "string min max length",
			mutate: func(schema *ToolParametersSchema) {
				minLength := 1
				maxLength := 2
				property := schema.Properties["value"]
				property.MinLength = &minLength
				property.MaxLength = &maxLength
				schema.Properties["value"] = property
			},
		},
		{
			name: "array min max items",
			mutate: func(schema *ToolParametersSchema) {
				minItems := 1
				maxItems := 2
				property := schema.Properties["value"]
				property.MinItems = &minItems
				property.MaxItems = &maxItems
				schema.Properties["value"] = property
			},
		},
		{
			name: "pattern",
			mutate: func(schema *ToolParametersSchema) {
				property := schema.Properties["value"]
				property.Pattern = "^[a-z]+$"
				schema.Properties["value"] = property
			},
		},
		{
			name: "additionalProperties",
			mutate: func(schema *ToolParametersSchema) {
				additionalProperties := false
				schema.AdditionalProperties = &additionalProperties
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := cloneToolParametersSchema(base)
			tt.mutate(schema)
			if got := testToolSchemaHash(schema); got == baseHash {
				t.Fatalf("hash did not change after %s constraint was added", tt.name)
			}
		})
	}
}

func testToolSchemaHash(parameters *ToolParametersSchema) string {
	return toolDescriptorSchemaHash(ToolDescriptor{
		Name:        "lookup",
		Description: "Lookup",
		Parameters:  parameters,
		Risk:        ToolRiskRead,
	})
}
