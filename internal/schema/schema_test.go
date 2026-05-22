package schema

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestToolParametersSchemaJSONSchemaEmitsSupportedConstraints(t *testing.T) {
	additionalProperties := false
	minimum := 1.5
	maximum := 10.5
	minLength := 2
	maxLength := 8
	minItems := 1
	maxItems := 3

	schema := ToolParametersSchema{
		Type:                 SchemaTypeObject,
		AdditionalProperties: &additionalProperties,
		Required:             []string{"mode"},
		Properties: map[string]ToolParametersSchema{
			"mode": {
				Type:        SchemaTypeString,
				Description: "Execution mode",
				Enum:        []any{"fast", "safe"},
				Default:     "safe",
				MinLength:   &minLength,
				MaxLength:   &maxLength,
				Pattern:     "^[a-z]+$",
			},
			"limit": {
				Type:    SchemaTypeNumber,
				Default: 3.5,
				Minimum: &minimum,
				Maximum: &maximum,
			},
			"tags": {
				Type:     SchemaTypeArray,
				Items:    &ToolParametersSchema{Type: SchemaTypeString},
				MinItems: &minItems,
				MaxItems: &maxItems,
			},
		},
	}

	got := schema.JSONSchema()
	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"mode"},
		"properties": map[string]any{
			"mode": map[string]any{
				"type":        "string",
				"description": "Execution mode",
				"enum":        []any{"fast", "safe"},
				"default":     "safe",
				"minLength":   2,
				"maxLength":   8,
				"pattern":     "^[a-z]+$",
			},
			"limit": map[string]any{
				"type":    "number",
				"default": 3.5,
				"minimum": 1.5,
				"maximum": 10.5,
			},
			"tags": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"minItems": 1,
				"maxItems": 3,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSONSchema() = %#v, want %#v", got, want)
	}
}

func TestCloneDeepCopiesSupportedConstraints(t *testing.T) {
	additionalProperties := false
	minLength := 2
	defaultValue := map[string]any{"mode": "safe"}
	original := &ToolParametersSchema{
		Type:                 SchemaTypeObject,
		AdditionalProperties: &additionalProperties,
		Properties: map[string]ToolParametersSchema{
			"mode": {
				Type:      SchemaTypeString,
				Enum:      []any{"fast", "safe"},
				Default:   defaultValue,
				MinLength: &minLength,
			},
		},
	}

	cloned := Clone(original)
	*cloned.AdditionalProperties = true
	property := cloned.Properties["mode"]
	property.Enum[0] = "changed"
	property.Default.(map[string]any)["mode"] = "changed"
	*property.MinLength = 99
	cloned.Properties["mode"] = property

	if *original.AdditionalProperties {
		t.Fatal("Clone shared AdditionalProperties pointer with original")
	}
	originalProperty := original.Properties["mode"]
	if got := originalProperty.Enum[0]; got != "fast" {
		t.Fatalf("original enum[0] = %q, want fast", got)
	}
	if got := originalProperty.Default.(map[string]any)["mode"]; got != "safe" {
		t.Fatalf("original default mode = %q, want safe", got)
	}
	if got := *originalProperty.MinLength; got != 2 {
		t.Fatalf("original minLength = %d, want 2", got)
	}
}

func TestValidateToolCallArgumentsEnforcesSupportedConstraints(t *testing.T) {
	additionalProperties := false
	minimum := 1.0
	maximum := 3.0
	minLength := 3
	maxLength := 5
	minItems := 1
	maxItems := 2
	schema := &ToolParametersSchema{
		Type: SchemaTypeObject,
		Properties: map[string]ToolParametersSchema{
			"profile": {
				Type:                 SchemaTypeObject,
				AdditionalProperties: &additionalProperties,
				Properties: map[string]ToolParametersSchema{
					"mode":  {Type: SchemaTypeString, Enum: []any{"safe", "fast"}},
					"count": {Type: SchemaTypeInteger, Minimum: &minimum, Maximum: &maximum},
					"label": {
						Type:      SchemaTypeString,
						MinLength: &minLength,
						MaxLength: &maxLength,
						Pattern:   "^[a-z]+$",
					},
					"tags": {
						Type:     SchemaTypeArray,
						Items:    &ToolParametersSchema{Type: SchemaTypeString},
						MinItems: &minItems,
						MaxItems: &maxItems,
					},
				},
			},
		},
	}

	if err := ValidateToolCallArguments("lookup", validConstraintArguments(), schema); err != nil {
		t.Fatalf("valid arguments returned error: %v", err)
	}

	tests := []struct {
		name      string
		mutate    func(map[string]any)
		wantParam string
		leak      string
	}{
		{
			name: "enum",
			mutate: func(arguments map[string]any) {
				profile(arguments)["mode"] = "leaked-secret-mode"
			},
			wantParam: "profile.mode",
			leak:      "leaked-secret-mode",
		},
		{
			name: "minimum",
			mutate: func(arguments map[string]any) {
				profile(arguments)["count"] = -42
			},
			wantParam: "profile.count",
			leak:      "-42",
		},
		{
			name: "maximum",
			mutate: func(arguments map[string]any) {
				profile(arguments)["count"] = 99
			},
			wantParam: "profile.count",
			leak:      "99",
		},
		{
			name: "minLength",
			mutate: func(arguments map[string]any) {
				profile(arguments)["label"] = "xy"
			},
			wantParam: "profile.label",
			leak:      "xy",
		},
		{
			name: "maxLength",
			mutate: func(arguments map[string]any) {
				profile(arguments)["label"] = "toolong-secret-label"
			},
			wantParam: "profile.label",
			leak:      "toolong-secret-label",
		},
		{
			name: "pattern",
			mutate: func(arguments map[string]any) {
				profile(arguments)["label"] = "Secret-ABC-123"
			},
			wantParam: "profile.label",
			leak:      "Secret-ABC-123",
		},
		{
			name: "minItems",
			mutate: func(arguments map[string]any) {
				profile(arguments)["tags"] = []any{}
			},
			wantParam: "profile.tags",
		},
		{
			name: "maxItems",
			mutate: func(arguments map[string]any) {
				profile(arguments)["tags"] = []any{"a", "b", "leaked-secret-tag"}
			},
			wantParam: "profile.tags",
			leak:      "leaked-secret-tag",
		},
		{
			name: "additionalProperties",
			mutate: func(arguments map[string]any) {
				profile(arguments)["token"] = "leaked-extra-token"
			},
			wantParam: "profile.token",
			leak:      "leaked-extra-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments := validConstraintArguments()
			tt.mutate(arguments)

			err := ValidateToolCallArguments("lookup", arguments, schema)
			if err == nil {
				t.Fatal("ValidateToolCallArguments returned nil error, want validation error")
			}
			var validationErr *ToolValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want *ToolValidationError", err)
			}
			if validationErr.Parameter != tt.wantParam {
				t.Fatalf("parameter = %q, want %q", validationErr.Parameter, tt.wantParam)
			}
			if tt.leak != "" && strings.Contains(err.Error(), tt.leak) {
				t.Fatalf("validation error leaked rejected value: %v", err)
			}
		})
	}
}

func validConstraintArguments() map[string]any {
	return map[string]any{
		"profile": map[string]any{
			"mode":  "safe",
			"count": 2,
			"label": "abc",
			"tags":  []any{"one"},
		},
	}
}

func profile(arguments map[string]any) map[string]any {
	return arguments["profile"].(map[string]any)
}
