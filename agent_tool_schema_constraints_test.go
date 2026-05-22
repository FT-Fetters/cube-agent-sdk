package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAgentRejectsToolCallWithSchemaConstraintPathWithoutLeakingValue(t *testing.T) {
	ctx := context.Background()
	const secret = "leaked-secret-token"
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "lookup", Arguments: map[string]any{
			"profile": map[string]any{"token": secret},
		}}}},
	}}
	additionalProperties := false
	called := false
	agent, err := New(Config{SystemPrompt: "base"}, model,
		WithTools(ToolFunc{
			ToolName:        "lookup",
			ToolDescription: "Lookup account",
			Parameters: &ToolParametersSchema{
				Type: SchemaTypeObject,
				Properties: map[string]ToolParametersSchema{
					"profile": {
						Type:                 SchemaTypeObject,
						AdditionalProperties: &additionalProperties,
						Properties: map[string]ToolParametersSchema{
							"id": {Type: SchemaTypeString},
						},
					},
				},
			},
			Fn: func(context.Context, ToolCall) (ToolResult, error) {
				called = true
				return ToolResult{}, nil
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(ctx, "lookup")
	if !errors.Is(err, ErrToolValidation) {
		t.Fatalf("err = %v, want ErrToolValidation", err)
	}
	var validationErr *ToolValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("err = %T, want *ToolValidationError", err)
	}
	if validationErr.Parameter != "profile.token" {
		t.Fatalf("parameter = %q, want profile.token", validationErr.Parameter)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("validation error leaked rejected value: %v", err)
	}
	if called {
		t.Fatal("tool was called after schema validation failed")
	}
}
