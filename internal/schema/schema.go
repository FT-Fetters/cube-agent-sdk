package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
)

var ErrToolValidation = errors.New("agent: tool validation failed")

// SchemaType is the JSON Schema primitive type used for tool parameters.
type SchemaType string

const (
	SchemaTypeString  SchemaType = "string"
	SchemaTypeNumber  SchemaType = "number"
	SchemaTypeInteger SchemaType = "integer"
	SchemaTypeBoolean SchemaType = "boolean"
	SchemaTypeObject  SchemaType = "object"
	SchemaTypeArray   SchemaType = "array"
)

// ToolParametersSchema is a lightweight JSON Schema subset for function calling arguments.
type ToolParametersSchema struct {
	Type        SchemaType
	Description string
	Properties  map[string]ToolParametersSchema
	Required    []string
	Items       *ToolParametersSchema
}

// JSONSchema converts the typed schema into the map shape expected by model providers.
func (s ToolParametersSchema) JSONSchema() map[string]any {
	schema := make(map[string]any)
	if s.Type != "" {
		schema["type"] = string(s.Type)
	}
	if s.Description != "" {
		schema["description"] = s.Description
	}
	if len(s.Properties) > 0 {
		properties := make(map[string]any, len(s.Properties))
		for name, property := range s.Properties {
			properties[name] = property.JSONSchema()
		}
		schema["properties"] = properties
	}
	if len(s.Required) > 0 {
		schema["required"] = append([]string(nil), s.Required...)
	}
	if s.Items != nil {
		schema["items"] = s.Items.JSONSchema()
	}
	return schema
}

// ToolValidationError identifies a tool argument rejected by parameter schema validation.
type ToolValidationError struct {
	ToolName  string
	Parameter string
	Message   string
}

func (e *ToolValidationError) Error() string {
	if e == nil {
		return ""
	}
	target := e.ToolName
	if e.Parameter != "" {
		target += "." + e.Parameter
	}
	if e.Message == "" {
		return fmt.Sprintf("%v for %s", ErrToolValidation, target)
	}
	return fmt.Sprintf("%v for %s: %s", ErrToolValidation, target, e.Message)
}

func (e *ToolValidationError) Unwrap() error {
	return ErrToolValidation
}

// Clone returns a deep copy so callers cannot mutate registered schemas.
func Clone(schema *ToolParametersSchema) *ToolParametersSchema {
	if schema == nil {
		return nil
	}
	cloned := *schema
	if len(schema.Properties) > 0 {
		cloned.Properties = make(map[string]ToolParametersSchema, len(schema.Properties))
		for name, property := range schema.Properties {
			propertyCopy := Clone(&property)
			if propertyCopy != nil {
				cloned.Properties[name] = *propertyCopy
			}
		}
	}
	if len(schema.Required) > 0 {
		cloned.Required = append([]string(nil), schema.Required...)
	}
	cloned.Items = Clone(schema.Items)
	return &cloned
}

// ValidateToolCallArguments checks a tool call argument object against its schema.
func ValidateToolCallArguments(toolName string, arguments map[string]any, schema *ToolParametersSchema) error {
	if schema == nil {
		return nil
	}
	return validateSchemaValue(toolName, "", arguments, *schema)
}

func validateSchemaValue(toolName, parameter string, value any, schema ToolParametersSchema) error {
	schemaType := schema.Type
	if schemaType == "" {
		switch {
		case len(schema.Properties) > 0 || len(schema.Required) > 0:
			schemaType = SchemaTypeObject
		case schema.Items != nil:
			schemaType = SchemaTypeArray
		default:
			return nil
		}
	}

	switch schemaType {
	case SchemaTypeObject:
		object, ok := asObject(value)
		if !ok {
			return newToolValidationError(toolName, parameter, "expected object")
		}
		for _, required := range schema.Required {
			requiredValue, exists := object[required]
			if !exists || requiredValue == nil {
				return newToolValidationError(toolName, childParameter(parameter, required), "missing required parameter")
			}
		}
		for name, propertySchema := range schema.Properties {
			propertyValue, exists := object[name]
			if !exists || propertyValue == nil {
				continue
			}
			if err := validateSchemaValue(toolName, childParameter(parameter, name), propertyValue, propertySchema); err != nil {
				return err
			}
		}
	case SchemaTypeArray:
		items, ok := asArray(value)
		if !ok {
			return newToolValidationError(toolName, parameter, "expected array")
		}
		if schema.Items == nil {
			return nil
		}
		for index, item := range items {
			if err := validateSchemaValue(toolName, childParameter(parameter, fmt.Sprintf("[%d]", index)), item, *schema.Items); err != nil {
				return err
			}
		}
	case SchemaTypeString:
		if _, ok := value.(string); !ok {
			return newToolValidationError(toolName, parameter, "expected string")
		}
	case SchemaTypeNumber:
		if !isNumber(value) {
			return newToolValidationError(toolName, parameter, "expected number")
		}
	case SchemaTypeInteger:
		if !isInteger(value) {
			return newToolValidationError(toolName, parameter, "expected integer")
		}
	case SchemaTypeBoolean:
		if _, ok := value.(bool); !ok {
			return newToolValidationError(toolName, parameter, "expected boolean")
		}
	default:
		return newToolValidationError(toolName, parameter, fmt.Sprintf("unsupported schema type %q", schema.Type))
	}
	return nil
}

func newToolValidationError(toolName, parameter, message string) *ToolValidationError {
	if parameter == "" {
		parameter = "$"
	}
	return &ToolValidationError{
		ToolName:  toolName,
		Parameter: parameter,
		Message:   message,
	}
}

func childParameter(parent, child string) string {
	if parent == "" || parent == "$" {
		return child
	}
	if len(child) > 0 && child[0] == '[' {
		return parent + child
	}
	return parent + "." + child
}

func asObject(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if object, ok := value.(map[string]any); ok {
		return object, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Map || reflected.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	object := make(map[string]any, reflected.Len())
	iterator := reflected.MapRange()
	for iterator.Next() {
		object[iterator.Key().String()] = iterator.Value().Interface()
	}
	return object, true
}

func asArray(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if items, ok := value.([]any); ok {
		return items, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return nil, false
	}
	items := make([]any, reflected.Len())
	for i := range items {
		items[i] = reflected.Index(i).Interface()
	}
	return items, true
}

func isNumber(value any) bool {
	switch number := value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return isFinite(float64(number))
	case float64:
		return isFinite(number)
	case json.Number:
		parsed, err := number.Float64()
		return err == nil && isFinite(parsed)
	default:
		return false
	}
}

func isInteger(value any) bool {
	switch number := value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		parsed := float64(number)
		return isFinite(parsed) && math.Trunc(parsed) == parsed
	case float64:
		return isFinite(number) && math.Trunc(number) == number
	case json.Number:
		if _, err := number.Int64(); err == nil {
			return true
		}
		parsed, err := number.Float64()
		return err == nil && isFinite(parsed) && math.Trunc(parsed) == parsed
	default:
		return false
	}
}

func isFinite(number float64) bool {
	return !math.IsNaN(number) && !math.IsInf(number, 0)
}
