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

type chatCompletionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func main() {
	ctx := context.Background()

	// The example endpoint is local. In production, BaseURL points at your
	// provider or proxy, and the API key comes from deployment secrets.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		var payload chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.Model == "" || len(payload.Messages) == 0 {
			http.Error(w, "missing model or messages", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hello from a fake OpenAI-compatible endpoint"}}]}`)
	}))
	defer server.Close()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = "example-api-key-from-env"
	}

	model, err := agent.NewOpenAICompatibleModel(agent.OpenAICompatibleConfig{
		BaseURL:    server.URL + "/v1",
		APIKey:     apiKey,
		Model:      "example-chat-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		log.Fatal(err)
	}

	bot, err := agent.New(agent.Config{
		SystemPrompt: "You answer briefly.",
	}, model)
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Say hello.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)
}
