package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"unicode/utf8"
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
// Default is emitted to model providers but validation does not inject default
// values into missing tool arguments.
type ToolParametersSchema struct {
	Type                 SchemaType
	Description          string
	Properties           map[string]ToolParametersSchema
	Required             []string
	Items                *ToolParametersSchema
	Enum                 []any
	Default              any
	Minimum              *float64
	Maximum              *float64
	MinLength            *int
	MaxLength            *int
	MinItems             *int
	MaxItems             *int
	Pattern              string
	AdditionalProperties *bool
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
	if len(s.Enum) > 0 {
		schema["enum"] = cloneAnySlice(s.Enum)
	}
	if s.Default != nil {
		schema["default"] = cloneJSONValue(s.Default)
	}
	if s.Minimum != nil {
		schema["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		schema["maximum"] = *s.Maximum
	}
	if s.MinLength != nil {
		schema["minLength"] = *s.MinLength
	}
	if s.MaxLength != nil {
		schema["maxLength"] = *s.MaxLength
	}
	if s.MinItems != nil {
		schema["minItems"] = *s.MinItems
	}
	if s.MaxItems != nil {
		schema["maxItems"] = *s.MaxItems
	}
	if s.Pattern != "" {
		schema["pattern"] = s.Pattern
	}
	if s.AdditionalProperties != nil {
		schema["additionalProperties"] = *s.AdditionalProperties
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
	if len(schema.Enum) > 0 {
		cloned.Enum = cloneAnySlice(schema.Enum)
	}
	cloned.Default = cloneJSONValue(schema.Default)
	cloned.Minimum = cloneFloat64Pointer(schema.Minimum)
	cloned.Maximum = cloneFloat64Pointer(schema.Maximum)
	cloned.MinLength = cloneIntPointer(schema.MinLength)
	cloned.MaxLength = cloneIntPointer(schema.MaxLength)
	cloned.MinItems = cloneIntPointer(schema.MinItems)
	cloned.MaxItems = cloneIntPointer(schema.MaxItems)
	cloned.AdditionalProperties = cloneBoolPointer(schema.AdditionalProperties)
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
		if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
			for name := range object {
				if _, exists := schema.Properties[name]; !exists {
					return newToolValidationError(toolName, childParameter(parameter, name), "unexpected parameter")
				}
			}
		}
	case SchemaTypeArray:
		items, ok := asArray(value)
		if !ok {
			return newToolValidationError(toolName, parameter, "expected array")
		}
		if schema.MinItems != nil && len(items) < *schema.MinItems {
			return newToolValidationError(toolName, parameter, "failed minItems constraint")
		}
		if schema.MaxItems != nil && len(items) > *schema.MaxItems {
			return newToolValidationError(toolName, parameter, "failed maxItems constraint")
		}
		if schema.Items == nil {
			return validateEnumConstraint(toolName, parameter, value, schema)
		}
		for index, item := range items {
			if err := validateSchemaValue(toolName, childParameter(parameter, fmt.Sprintf("[%d]", index)), item, *schema.Items); err != nil {
				return err
			}
		}
	case SchemaTypeString:
		text, ok := value.(string)
		if !ok {
			return newToolValidationError(toolName, parameter, "expected string")
		}
		if schema.MinLength != nil && utf8.RuneCountInString(text) < *schema.MinLength {
			return newToolValidationError(toolName, parameter, "failed minLength constraint")
		}
		if schema.MaxLength != nil && utf8.RuneCountInString(text) > *schema.MaxLength {
			return newToolValidationError(toolName, parameter, "failed maxLength constraint")
		}
		if schema.Pattern != "" {
			matched, err := regexp.MatchString(schema.Pattern, text)
			if err != nil {
				return newToolValidationError(toolName, parameter, "invalid pattern constraint")
			}
			if !matched {
				return newToolValidationError(toolName, parameter, "failed pattern constraint")
			}
		}
	case SchemaTypeNumber:
		number, ok := numberAsFloat64(value)
		if !ok {
			return newToolValidationError(toolName, parameter, "expected number")
		}
		if err := validateNumericBounds(toolName, parameter, number, schema); err != nil {
			return err
		}
	case SchemaTypeInteger:
		if !isInteger(value) {
			return newToolValidationError(toolName, parameter, "expected integer")
		}
		number, _ := numberAsFloat64(value)
		if err := validateNumericBounds(toolName, parameter, number, schema); err != nil {
			return err
		}
	case SchemaTypeBoolean:
		if _, ok := value.(bool); !ok {
			return newToolValidationError(toolName, parameter, "expected boolean")
		}
	default:
		return newToolValidationError(toolName, parameter, fmt.Sprintf("unsupported schema type %q", schema.Type))
	}
	return validateEnumConstraint(toolName, parameter, value, schema)
}

func validateNumericBounds(toolName, parameter string, number float64, schema ToolParametersSchema) error {
	if schema.Minimum != nil && number < *schema.Minimum {
		return newToolValidationError(toolName, parameter, "failed minimum constraint")
	}
	if schema.Maximum != nil && number > *schema.Maximum {
		return newToolValidationError(toolName, parameter, "failed maximum constraint")
	}
	return nil
}

func validateEnumConstraint(toolName, parameter string, value any, schema ToolParametersSchema) error {
	if len(schema.Enum) == 0 {
		return nil
	}
	if enumContains(schema.Type, schema.Enum, value) {
		return nil
	}
	return newToolValidationError(toolName, parameter, "failed enum constraint")
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
	_, ok := numberAsFloat64(value)
	return ok
}

func numberAsFloat64(value any) (float64, bool) {
	switch number := value.(type) {
	case int, int8, int16, int32, int64:
		return float64(reflect.ValueOf(number).Int()), true
	case uint, uint8, uint16, uint32, uint64:
		return float64(reflect.ValueOf(number).Uint()), true
	case float32:
		parsed := float64(number)
		return parsed, isFinite(parsed)
	case float64:
		return number, isFinite(number)
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil && isFinite(parsed)
	default:
		return 0, false
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

func enumContains(schemaType SchemaType, enum []any, value any) bool {
	for _, candidate := range enum {
		if schemaType == SchemaTypeNumber || schemaType == SchemaTypeInteger {
			candidateNumber, candidateOK := numberAsFloat64(candidate)
			valueNumber, valueOK := numberAsFloat64(value)
			if candidateOK && valueOK && candidateNumber == valueNumber {
				return true
			}
			continue
		}
		if reflect.DeepEqual(candidate, value) {
			return true
		}
	}
	return false
}

func cloneAnySlice(values []any) []any {
	cloned := make([]any, len(values))
	for i, value := range values {
		cloned[i] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, nested := range typed {
			cloned[key] = cloneJSONValue(nested)
		}
		return cloned
	case []any:
		return cloneAnySlice(typed)
	default:
		return value
	}
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
