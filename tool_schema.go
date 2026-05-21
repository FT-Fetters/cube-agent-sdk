package agent

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sort"

	"github.com/cubence/cube-agent-sdk/internal/schema"
)

const (
	toolSchemaHashAlgorithm = "cube-agent-sdk-tool-schema-hash-v1"
	toolSchemaHashPrefix    = "sha256:"
)

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

func toolSchemaHashForTool(tool Tool, parameters *ToolParametersSchema, risk ToolRisk) string {
	if tool == nil || parameters == nil {
		return ""
	}
	return toolDescriptorSchemaHash(ToolDescriptor{
		Name:        tool.Name(),
		Description: tool.Description(),
		Parameters:  parameters,
		Risk:        risk,
	})
}

func toolDescriptorSchemaHash(descriptor ToolDescriptor) string {
	if descriptor.Parameters == nil {
		return ""
	}
	hasher := sha256.New()
	writeToolSchemaHashString(hasher, toolSchemaHashAlgorithm)
	writeToolSchemaHashString(hasher, descriptor.Name)
	writeToolSchemaHashString(hasher, descriptor.Description)
	writeToolSchemaHashString(hasher, string(descriptor.Risk))
	writeToolParametersSchemaHash(hasher, descriptor.Parameters)
	return toolSchemaHashPrefix + hex.EncodeToString(hasher.Sum(nil))
}

// writeToolParametersSchemaHash uses sorted object keys so the same schema
// always produces the same hash regardless of Go map iteration order.
func writeToolParametersSchemaHash(hasher hash.Hash, parameters *ToolParametersSchema) {
	if parameters == nil {
		writeToolSchemaHashBool(hasher, false)
		return
	}
	writeToolSchemaHashBool(hasher, true)
	writeToolSchemaHashString(hasher, string(parameters.Type))
	writeToolSchemaHashString(hasher, parameters.Description)

	required := append([]string(nil), parameters.Required...)
	sort.Strings(required)
	writeToolSchemaHashUint64(hasher, uint64(len(required)))
	for _, name := range required {
		writeToolSchemaHashString(hasher, name)
	}

	propertyNames := make([]string, 0, len(parameters.Properties))
	for name := range parameters.Properties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	writeToolSchemaHashUint64(hasher, uint64(len(propertyNames)))
	for _, name := range propertyNames {
		property := parameters.Properties[name]
		writeToolSchemaHashString(hasher, name)
		writeToolParametersSchemaHash(hasher, &property)
	}

	writeToolParametersSchemaHash(hasher, parameters.Items)
}

func writeToolSchemaHashString(hasher hash.Hash, value string) {
	writeToolSchemaHashUint64(hasher, uint64(len(value)))
	_, _ = hasher.Write([]byte(value))
}

func writeToolSchemaHashBool(hasher hash.Hash, value bool) {
	if value {
		writeToolSchemaHashUint64(hasher, 1)
		return
	}
	writeToolSchemaHashUint64(hasher, 0)
}

func writeToolSchemaHashUint64(hasher hash.Hash, value uint64) {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], value)
	_, _ = hasher.Write(buffer[:])
}
