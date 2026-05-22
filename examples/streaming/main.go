package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/cubence/cube-agent-sdk"
)

type streamingModel struct{}

func (streamingModel) Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error) {
	return agent.ModelResponse{
		Message: agent.Message{Role: agent.RoleAssistant, Content: "non-streaming fallback"},
	}, nil
}

func (streamingModel) Stream(ctx context.Context, request agent.ModelRequest) (<-chan agent.StreamEvent, error) {
	events := make(chan agent.StreamEvent)
	go func() {
		defer close(events)
		for _, delta := range []string{"streamed ", "assistant ", "text"} {
			select {
			case <-ctx.Done():
				return
			case events <- agent.StreamEvent{Type: agent.StreamEventDelta, Delta: delta}:
			}
		}

		// Done commits the assistant message into the SDK-managed session.
		select {
		case <-ctx.Done():
		case events <- agent.StreamEvent{
			Type:    agent.StreamEventDone,
			Message: agent.Message{Role: agent.RoleAssistant, Content: "streamed assistant text"},
			Usage:   agent.TokenUsage{InputTokens: 6, OutputTokens: 3, TotalTokens: 9},
		}:
		}
	}()
	return events, nil
}

func main() {
	bot, err := agent.New(agent.Config{
		SystemPrompt: "Stream short answers.",
	}, streamingModel{})
	if err != nil {
		log.Fatal(err)
	}

	events, err := bot.RunStream(context.Background(), "Write a three-word response.")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		switch event.Type {
		case agent.StreamEventDelta:
			fmt.Print(event.Delta)
		case agent.StreamEventDone:
			fmt.Printf("\nfinal: %s\n", event.Message.Content)
		case agent.StreamEventError:
			log.Fatal(event.Error)
		}
	}
}
