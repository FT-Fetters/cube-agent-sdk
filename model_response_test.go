package agent

import (
	"context"
	"testing"
)

func TestModelResponseCarriesTokenUsage(t *testing.T) {
	var model Model = usageModel{}

	response, err := model.Generate(context.Background(), ModelRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if response.Usage.InputTokens != 8 || response.Usage.OutputTokens != 5 || response.Usage.TotalTokens != 13 {
		t.Fatalf("usage = %#v, want input/output/total token counts", response.Usage)
	}
}

func TestModelResponseUsageIsOptional(t *testing.T) {
	response := ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}

	if response.Usage != (TokenUsage{}) {
		t.Fatalf("usage = %#v, want zero value when usage is not set", response.Usage)
	}
}

type usageModel struct{}

func (usageModel) Generate(context.Context, ModelRequest) (ModelResponse, error) {
	return ModelResponse{
		Message: Message{Role: RoleAssistant, Content: "ok"},
		Usage: TokenUsage{
			InputTokens:  8,
			OutputTokens: 5,
			TotalTokens:  13,
		},
	}, nil
}
