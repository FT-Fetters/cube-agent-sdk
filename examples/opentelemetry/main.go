package main

import (
	"context"
	"fmt"
	"log"
	"time"

	agent "github.com/cubence/cube-agent-sdk"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type localModel struct {
	responses []agent.ModelResponse
	next      int
}

func (m *localModel) Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error) {
	if m.next >= len(m.responses) {
		return agent.ModelResponse{Message: agent.Message{Role: agent.RoleAssistant, Content: "no scripted response left"}}, nil
	}
	response := m.responses[m.next]
	m.next++
	return response, nil
}

func (m *localModel) Stream(ctx context.Context, request agent.ModelRequest) (<-chan agent.StreamEvent, error) {
	events := make(chan agent.StreamEvent)
	go func() {
		defer close(events)
		for _, delta := range []string{"streamed ", "local ", "answer"} {
			time.Sleep(time.Millisecond)
			select {
			case <-ctx.Done():
				return
			case events <- agent.StreamEvent{Type: agent.StreamEventDelta, Delta: delta}:
			}
		}
		select {
		case <-ctx.Done():
		case events <- agent.StreamEvent{
			Type:    agent.StreamEventDone,
			Message: agent.Message{Role: agent.RoleAssistant, Content: "streamed local answer"},
		}:
		}
	}()
	return events, nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown tracer provider: %v", err)
		}
	}()

	tracer := provider.Tracer("cube-agent-sdk-otel-example")
	observer := NewOTelObserver(tracer)
	ctx, root := tracer.Start(ctx, "example.run")
	defer root.End()

	traceContext := root.SpanContext()
	ctx = agent.WithTraceContext(ctx, agent.TraceContext{
		TraceID:    traceContext.TraceID().String(),
		SpanID:     traceContext.SpanID().String(),
		TraceState: traceContext.TraceState().String(),
	})

	call := agent.ToolCall{
		ID:        "call-lookup-account",
		Name:      "lookup_account",
		Arguments: map[string]any{"account_id": "demo-account"},
	}
	model := &localModel{responses: []agent.ModelResponse{
		{
			Message:   agent.Message{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{call}},
			ToolCalls: []agent.ToolCall{call},
			Usage:     agent.TokenUsage{InputTokens: 16, OutputTokens: 8, TotalTokens: 24},
		},
		{
			Message: agent.Message{Role: agent.RoleAssistant, Content: "The demo account is active."},
			Usage:   agent.TokenUsage{InputTokens: 20, OutputTokens: 7, TotalTokens: 27},
		},
	}}

	lookup := agent.ToolFunc{
		ToolName:        "lookup_account",
		ToolDescription: "Read account status from a local fake data source",
		ToolRisk:        agent.ToolRiskRead,
		Parameters: &agent.ToolParametersSchema{
			Type:     agent.SchemaTypeObject,
			Required: []string{"account_id"},
			Properties: map[string]agent.ToolParametersSchema{
				"account_id": {Type: agent.SchemaTypeString, Description: "Application account identifier"},
			},
		},
		Fn: func(ctx context.Context, call agent.ToolCall) (agent.ToolResult, error) {
			return agent.ToolResult{
				CallID:  call.ID,
				Name:    call.Name,
				Content: "demo account is active",
				Metadata: map[string]any{
					"source": "local_fake_store",
				},
			}, nil
		},
	}

	bot, err := agent.New(agent.Config{ID: "otel-example-agent", SystemPrompt: "Use local demo data."}, model,
		agent.WithTools(lookup),
		agent.WithApprovalPolicy(agent.RequireAllApprovals(
			agent.AllowToolsApproval("lookup_account"),
			agent.AllowRisksApproval(agent.ToolRiskRead),
		)),
		agent.WithObserver(observer),
	)
	if err != nil {
		return err
	}

	reply, err := bot.Run(ctx, "Check the demo account.", agent.WithRunID("otel-example-run"))
	if err != nil {
		return err
	}
	fmt.Println(reply.Content)

	stream, err := bot.RunStream(ctx, "Stream a local answer.", agent.WithRunID("otel-stream-run"), agent.WithStreamObservations())
	if err != nil {
		return err
	}
	for event := range stream {
		switch event.Type {
		case agent.StreamEventDelta:
			fmt.Print(event.Delta)
		case agent.StreamEventDone:
			fmt.Printf("\nfinal: %s\n", event.Message.Content)
		case agent.StreamEventError:
			return event.Error
		}
	}
	return nil
}
