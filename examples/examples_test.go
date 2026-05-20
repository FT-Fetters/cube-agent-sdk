package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTask9ExamplesAndReadmeCoverage(t *testing.T) {
	examples := []string{
		"openai_compatible",
		"model_factory",
		"live_api",
		"tool_schema",
		"streaming",
		"mcp_stdio",
		"session_state",
		"approval_observer",
	}
	for _, example := range examples {
		t.Run(example, func(t *testing.T) {
			path := filepath.Join(example, "main.go")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("example %s must provide a compilable main.go: %v", example, err)
			}
		})
	}

	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	sections := []string{
		"## Quick Start",
		"## OpenAI-Compatible Models",
		"## Optional Live API Tests",
		"go test -v -run '^TestLiveAPIModelRun$' .",
		"## Built-In Model API Types",
		"## Anthropic Messages",
		"## Tool Schema",
		"## Streaming",
		"## MCP Stdio",
		"## Session State",
		"## Approval Policies",
		"## Observability",
		"## Error Handling",
		"## Production Integration",
		"## SDK Responsibilities",
	}
	for _, section := range sections {
		if !strings.Contains(text, section) {
			t.Fatalf("README must contain section %q", section)
		}
	}
	for _, phrase := range []string{
		"MODEL_API_TYPE=anthropic-messages",
		"MODEL_BASE_URL=https://api.anthropic.com",
		"When these variables are present in the process environment or root .env",
		"Do not commit real credentials",
	} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("README must contain phrase %q", phrase)
		}
	}

	contributing, err := os.ReadFile(filepath.Join("..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatal(err)
	}
	contributingText := string(contributing)
	// Keep live-test docs tied to provider types committed in this repository.
	for _, phrase := range []string{
		"Optional live API tests",
		"MODEL_API_TYPE=anthropic-messages",
		"MODEL_BASE_URL=https://api.anthropic.com",
		"When these variables are present in the process environment or root .env",
		"Do not commit real credentials",
		"go test -v -run '^TestLiveAPIModelRun$' .",
	} {
		if !strings.Contains(contributingText, phrase) {
			t.Fatalf("CONTRIBUTING must contain phrase %q", phrase)
		}
	}
}
