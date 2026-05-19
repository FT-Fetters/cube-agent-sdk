package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	agent "github.com/cubence/cube-agent-sdk"
)

func main() {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			if r.Header.Get("x-api-key") == "" {
				http.Error(w, "missing API key", http.StatusUnauthorized)
				return
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"hello from Anthropic Messages"}]}`)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hello from OpenAI-compatible"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	apiType := agent.ModelAPIAnthropicMessages
	if os.Getenv("EXAMPLE_MODEL_API_TYPE") == string(agent.ModelAPIOpenAICompatible) {
		apiType = agent.ModelAPIOpenAICompatible
	}

	modelName := "claude-example"
	if apiType == agent.ModelAPIOpenAICompatible {
		modelName = "openai-compatible-example"
	}

	model, err := agent.NewModel(agent.ModelConfig{
		APIType:    apiType,
		BaseURL:    server.URL,
		APIKey:     "example-key",
		Model:      modelName,
		HTTPClient: server.Client(),
	})
	if err != nil {
		log.Fatal(err)
	}

	bot, err := agent.New(agent.Config{SystemPrompt: "Answer briefly."}, model)
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Say hello.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)
}
