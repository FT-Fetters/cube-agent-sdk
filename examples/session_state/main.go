package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	agent "github.com/cubence/cube-agent-sdk"
)

type contextCountingModel struct{}

func (contextCountingModel) Generate(ctx context.Context, request agent.ModelRequest) (agent.ModelResponse, error) {
	return agent.ModelResponse{
		Message: agent.Message{
			Role:    agent.RoleAssistant,
			Content: fmt.Sprintf("model saw %d message(s)", len(request.Messages)),
		},
	}, nil
}

func main() {
	ctx := context.Background()
	model := contextCountingModel{}

	bot, err := agent.New(agent.Config{
		ID:           "session-example",
		SystemPrompt: "Track conversation state.",
	}, model)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := bot.Run(ctx, "Start the session."); err != nil {
		log.Fatal(err)
	}

	snapshot := bot.Snapshot()
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("snapshot bytes: %d\n", len(encoded))

	// The SDK gives you a portable snapshot. Durable storage, encryption, and
	// retention policy belong to the application.
	var restoredSnapshot agent.SessionSnapshot
	if err := json.Unmarshal(encoded, &restoredSnapshot); err != nil {
		log.Fatal(err)
	}

	restored, err := agent.New(agent.Config{
		ID:           "restored-session",
		SystemPrompt: "Track conversation state.",
	}, model)
	if err != nil {
		log.Fatal(err)
	}
	if err := restored.Restore(restoredSnapshot); err != nil {
		log.Fatal(err)
	}
	reply, err := restored.Run(ctx, "Continue after restore.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)

	fork, err := restored.Fork("what-if-branch")
	if err != nil {
		log.Fatal(err)
	}
	branchReply, err := fork.Run(ctx, "Explore a branch without mutating the restored agent.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(branchReply.Content)
}
