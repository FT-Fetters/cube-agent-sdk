package agent

import (
	"errors"
	"testing"
)

func TestGeneratedToolParametersSchemaValidatesRuntimeArguments(t *testing.T) {
	schema, err := ToolParametersSchemaFromStruct(generatedToolSchemaArgs{})
	if err != nil {
		t.Fatal(err)
	}

	valid := map[string]any{
		"query":  "find accounts",
		"mode":   "safe",
		"limit":  5,
		"active": true,
		"address": map[string]any{
			"city": "San Francisco",
		},
		"tags":   []any{"paid"},
		"scores": []any{1.0, 2.0},
	}
	if err := validateToolCallArguments("lookup", valid, schema); err != nil {
		t.Fatalf("generated schema rejected valid arguments: %v", err)
	}

	invalid := map[string]any{
		"query": "find accounts",
		"mode":  "unsafe",
	}
	err = validateToolCallArguments("lookup", invalid, schema)
	if err == nil {
		t.Fatal("validateToolCallArguments returned nil error, want generated enum validation error")
	}
	var validationErr *ToolValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("err = %T, want *ToolValidationError", err)
	}
	if validationErr.Parameter != "mode" {
		t.Fatalf("parameter = %q, want mode", validationErr.Parameter)
	}
}
