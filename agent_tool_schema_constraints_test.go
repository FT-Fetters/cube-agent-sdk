package agent

import (
	"context"
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
	if err != nil {
		t.Fatalf("Run error = %v, want validation feedback to continue", err)
	}
	messages := agent.Messages()
	if len(messages) < 3 {
		t.Fatalf("agent messages = %#v, want tool feedback message", messages)
	}
	feedback := messages[len(messages)-2]
	if feedback.Role != RoleTool || feedback.ToolCallID != "call-1" {
		t.Fatalf("feedback message = %#v, want tool feedback for call-1", feedback)
	}
	if !strings.Contains(feedback.Content, "profile.token") {
		t.Fatalf("feedback content = %q, want schema path", feedback.Content)
	}
	if strings.Contains(feedback.Content, secret) {
		t.Fatalf("feedback leaked rejected value: %q", feedback.Content)
	}
	if called {
		t.Fatal("tool was called after schema validation failed")
	}
}
