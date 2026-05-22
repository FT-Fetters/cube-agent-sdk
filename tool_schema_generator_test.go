package agent

import (
	"reflect"
	"strings"
	"testing"
)

type generatedToolSchemaAddress struct {
	City string `json:"city" description:"City name" required:"true" minLength:"2" maxLength:"30" pattern:"^[A-Za-z ]+$"`
}

type generatedToolSchemaArgs struct {
	Query   string                      `json:"query" description:"Search query" required:"true" minLength:"3" maxLength:"80" pattern:"^[a-z ]+$"`
	Mode    string                      `json:"mode,omitempty" enum:"fast,safe" default:"safe"`
	Limit   int                         `json:"limit,omitempty" min:"1" max:"20" default:"5"`
	Active  bool                        `json:"active,omitempty" default:"true"`
	Address *generatedToolSchemaAddress `json:"address,omitempty" description:"Address filter" additionalProperties:"false"`
	Tags    []string                    `json:"tags,omitempty" minItems:"1" maxItems:"3"`
	Scores  [2]float64                  `json:"scores,omitempty"`
	Ignored string                      `json:"-"`
	hidden  string
}

func TestToolParametersSchemaFromStructUsesTagsAndNestedTypes(t *testing.T) {
	schema, err := ToolParametersSchemaFromStruct(generatedToolSchemaArgs{})
	if err != nil {
		t.Fatal(err)
	}

	got := schema.JSONSchema()
	want := map[string]any{
		"type":     "object",
		"required": []string{"query"},
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
				"minLength":   3,
				"maxLength":   80,
				"pattern":     "^[a-z ]+$",
			},
			"mode": map[string]any{
				"type":    "string",
				"enum":    []any{"fast", "safe"},
				"default": "safe",
			},
			"limit": map[string]any{
				"type":    "integer",
				"minimum": 1.0,
				"maximum": 20.0,
				"default": 5,
			},
			"active": map[string]any{
				"type":    "boolean",
				"default": true,
			},
			"address": map[string]any{
				"type":                 "object",
				"description":          "Address filter",
				"additionalProperties": false,
				"required":             []string{"city"},
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "City name",
						"minLength":   2,
						"maxLength":   30,
						"pattern":     "^[A-Za-z ]+$",
					},
				},
			},
			"tags": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"minItems": 1,
				"maxItems": 3,
			},
			"scores": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "number"},
				"minItems": 2,
				"maxItems": 2,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated JSON schema = %#v, want %#v", got, want)
	}
	if _, exists := schema.Properties["ignored"]; exists {
		t.Fatal("json:\"-\" field was included in generated schema")
	}
	if _, exists := schema.Properties["hidden"]; exists {
		t.Fatal("unexported field was included in generated schema")
	}
}

func TestToolParametersSchemaFromStructAcceptsPointerTypes(t *testing.T) {
	schema, err := ToolParametersSchemaFromStruct((*generatedToolSchemaArgs)(nil))
	if err != nil {
		t.Fatal(err)
	}
	if schema.Type != SchemaTypeObject {
		t.Fatalf("schema type = %q, want object", schema.Type)
	}
}

func TestToolParametersSchemaFromStructRejectsUnsupportedFieldTypes(t *testing.T) {
	type unsupportedArgs struct {
		Handler func() `json:"handler"`
	}

	_, err := ToolParametersSchemaFromStruct(unsupportedArgs{})
	if err == nil {
		t.Fatal("ToolParametersSchemaFromStruct returned nil error, want unsupported type error")
	}
	if !strings.Contains(err.Error(), "handler") {
		t.Fatalf("error = %q, want field path", err)
	}
}
