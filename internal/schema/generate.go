package schema

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ToolParametersSchemaFromStruct builds a tool parameter schema from exported
// struct fields and schema-related struct tags. Supported tags are json,
// description, required, enum, default, min, max, minLength, maxLength,
// minItems, maxItems, pattern, and additionalProperties. It intentionally
// supports only the SDK's lightweight JSON Schema subset.
func ToolParametersSchemaFromStruct(value any) (*ToolParametersSchema, error) {
	if value == nil {
		return nil, fmt.Errorf("agent: tool schema generation requires a struct")
	}
	structType := indirectType(reflect.TypeOf(value))
	if structType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("agent: tool schema generation requires a struct, got %s", structType)
	}
	generated, err := schemaFromStructType(structType, "")
	if err != nil {
		return nil, err
	}
	return &generated, nil
}

func schemaFromStructType(structType reflect.Type, path string) (ToolParametersSchema, error) {
	generated := ToolParametersSchema{
		Type:       SchemaTypeObject,
		Properties: make(map[string]ToolParametersSchema),
	}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, skip := jsonFieldName(field)
		if skip {
			continue
		}
		fieldPath := childParameter(path, name)
		fieldSchema, err := schemaFromType(field.Type, fieldPath)
		if err != nil {
			return ToolParametersSchema{}, err
		}
		if err := applyFieldTags(&fieldSchema, field, fieldPath); err != nil {
			return ToolParametersSchema{}, err
		}
		if required, err := boolTag(field, "required", fieldPath); err != nil {
			return ToolParametersSchema{}, err
		} else if required != nil && *required {
			generated.Required = append(generated.Required, name)
		}
		generated.Properties[name] = fieldSchema
	}
	return generated, nil
}

func schemaFromType(fieldType reflect.Type, path string) (ToolParametersSchema, error) {
	fieldType = indirectType(fieldType)
	switch fieldType.Kind() {
	case reflect.String:
		return ToolParametersSchema{Type: SchemaTypeString}, nil
	case reflect.Bool:
		return ToolParametersSchema{Type: SchemaTypeBoolean}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return ToolParametersSchema{Type: SchemaTypeInteger}, nil
	case reflect.Float32, reflect.Float64:
		return ToolParametersSchema{Type: SchemaTypeNumber}, nil
	case reflect.Struct:
		return schemaFromStructType(fieldType, path)
	case reflect.Slice:
		itemSchema, err := schemaFromType(fieldType.Elem(), childParameter(path, "[]"))
		if err != nil {
			return ToolParametersSchema{}, err
		}
		return ToolParametersSchema{Type: SchemaTypeArray, Items: &itemSchema}, nil
	case reflect.Array:
		itemSchema, err := schemaFromType(fieldType.Elem(), childParameter(path, "[]"))
		if err != nil {
			return ToolParametersSchema{}, err
		}
		length := fieldType.Len()
		return ToolParametersSchema{
			Type:     SchemaTypeArray,
			Items:    &itemSchema,
			MinItems: &length,
			MaxItems: &length,
		}, nil
	default:
		return ToolParametersSchema{}, fmt.Errorf("agent: unsupported tool schema field %q type %s", path, fieldType)
	}
}

func applyFieldTags(schema *ToolParametersSchema, field reflect.StructField, path string) error {
	if description := field.Tag.Get("description"); description != "" {
		schema.Description = description
	}
	if enumText := field.Tag.Get("enum"); enumText != "" {
		enum, err := parseEnumTag(enumText, schema.Type, path)
		if err != nil {
			return err
		}
		schema.Enum = enum
	}
	if defaultText, ok := field.Tag.Lookup("default"); ok {
		value, err := parseDefaultTag(defaultText, schema.Type, path)
		if err != nil {
			return err
		}
		schema.Default = value
	}
	if err := applyNumericTags(schema, field, path); err != nil {
		return err
	}
	if err := applyStringTags(schema, field, path); err != nil {
		return err
	}
	if err := applyArrayTags(schema, field, path); err != nil {
		return err
	}
	if additionalProperties, err := boolTag(field, "additionalProperties", path); err != nil {
		return err
	} else if additionalProperties != nil {
		if schema.Type != SchemaTypeObject {
			return fmt.Errorf("agent: additionalProperties tag on non-object field %q", path)
		}
		schema.AdditionalProperties = additionalProperties
	}
	return nil
}

func applyNumericTags(schema *ToolParametersSchema, field reflect.StructField, path string) error {
	if minimumText, ok := field.Tag.Lookup("min"); ok {
		if schema.Type != SchemaTypeNumber && schema.Type != SchemaTypeInteger {
			return fmt.Errorf("agent: min tag on non-numeric field %q", path)
		}
		minimum, err := parseFloatTag("min", minimumText, path)
		if err != nil {
			return err
		}
		schema.Minimum = &minimum
	}
	if maximumText, ok := field.Tag.Lookup("max"); ok {
		if schema.Type != SchemaTypeNumber && schema.Type != SchemaTypeInteger {
			return fmt.Errorf("agent: max tag on non-numeric field %q", path)
		}
		maximum, err := parseFloatTag("max", maximumText, path)
		if err != nil {
			return err
		}
		schema.Maximum = &maximum
	}
	return nil
}

func applyStringTags(schema *ToolParametersSchema, field reflect.StructField, path string) error {
	if minimumText, ok := field.Tag.Lookup("minLength"); ok {
		if schema.Type != SchemaTypeString {
			return fmt.Errorf("agent: minLength tag on non-string field %q", path)
		}
		minimum, err := parseIntTag("minLength", minimumText, path)
		if err != nil {
			return err
		}
		schema.MinLength = &minimum
	}
	if maximumText, ok := field.Tag.Lookup("maxLength"); ok {
		if schema.Type != SchemaTypeString {
			return fmt.Errorf("agent: maxLength tag on non-string field %q", path)
		}
		maximum, err := parseIntTag("maxLength", maximumText, path)
		if err != nil {
			return err
		}
		schema.MaxLength = &maximum
	}
	if pattern := field.Tag.Get("pattern"); pattern != "" {
		if schema.Type != SchemaTypeString {
			return fmt.Errorf("agent: pattern tag on non-string field %q", path)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("agent: invalid pattern tag on field %q", path)
		}
		schema.Pattern = pattern
	}
	return nil
}

func applyArrayTags(schema *ToolParametersSchema, field reflect.StructField, path string) error {
	if minimumText, ok := field.Tag.Lookup("minItems"); ok {
		if schema.Type != SchemaTypeArray {
			return fmt.Errorf("agent: minItems tag on non-array field %q", path)
		}
		minimum, err := parseIntTag("minItems", minimumText, path)
		if err != nil {
			return err
		}
		schema.MinItems = &minimum
	}
	if maximumText, ok := field.Tag.Lookup("maxItems"); ok {
		if schema.Type != SchemaTypeArray {
			return fmt.Errorf("agent: maxItems tag on non-array field %q", path)
		}
		maximum, err := parseIntTag("maxItems", maximumText, path)
		if err != nil {
			return err
		}
		schema.MaxItems = &maximum
	}
	return nil
}

func parseEnumTag(value string, schemaType SchemaType, path string) ([]any, error) {
	parts := strings.Split(value, ",")
	enum := make([]any, 0, len(parts))
	for _, part := range parts {
		parsed, err := parseScalarTag(strings.TrimSpace(part), schemaType, path, "enum")
		if err != nil {
			return nil, err
		}
		enum = append(enum, parsed)
	}
	return enum, nil
}

func parseDefaultTag(value string, schemaType SchemaType, path string) (any, error) {
	return parseScalarTag(value, schemaType, path, "default")
}

func parseScalarTag(value string, schemaType SchemaType, path, tagName string) (any, error) {
	switch schemaType {
	case SchemaTypeString:
		return value, nil
	case SchemaTypeBoolean:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("agent: invalid %s tag on field %q", tagName, path)
		}
		return parsed, nil
	case SchemaTypeInteger:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("agent: invalid %s tag on field %q", tagName, path)
		}
		return int(parsed), nil
	case SchemaTypeNumber:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("agent: invalid %s tag on field %q", tagName, path)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("agent: %s tag on non-scalar field %q", tagName, path)
	}
}

func parseFloatTag(name, value, path string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("agent: invalid %s tag on field %q", name, path)
	}
	return parsed, nil
}

func parseIntTag(name, value, path string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("agent: invalid %s tag on field %q", name, path)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("agent: negative %s tag on field %q", name, path)
	}
	return parsed, nil
}

func boolTag(field reflect.StructField, name, path string) (*bool, error) {
	value, ok := field.Tag.Lookup(name)
	if !ok {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, fmt.Errorf("agent: invalid %s tag on field %q", name, path)
	}
	return &parsed, nil
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return "", true
	}
	if name == "" {
		return field.Name, false
	}
	return name, false
}

func indirectType(fieldType reflect.Type) reflect.Type {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return fieldType
}
