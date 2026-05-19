package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/cubence/cube-agent-sdk"
)

type scriptedModel struct {
	responses []agent.ModelResponse
	next      int
}

func (m *scriptedModel) Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error) {
	if m.next >= len(m.responses) {
		return agent.ModelResponse{
			Message: agent.Message{Role: agent.RoleAssistant, Content: "no scripted response left"},
		}, nil
	}
	response := m.responses[m.next]
	m.next++
	return response, nil
}

type collectingObserver struct {
	observations []agent.Observation
}

func (o *collectingObserver) Observe(ctx context.Context, observation agent.Observation) {
	o.observations = append(o.observations, observation)
}

func main() {
	ctx := context.Background()
	observer := &collectingObserver{}

	lookup := agent.ToolFunc{
		ToolName:        "lookup_account",
		ToolDescription: "Read account status from an application data source",
		ToolRisk:        agent.ToolRiskRead,
		Parameters: &agent.ToolParametersSchema{
			Type:     agent.SchemaTypeObject,
			Required: []string{"account_id"},
			Properties: map[string]agent.ToolParametersSchema{
				"account_id": {Type: agent.SchemaTypeString, Description: "Application account identifier"},
			},
		},
		Fn: func(ctx context.Context, call agent.ToolCall) (agent.ToolResult, error) {
			accountID, _ := call.Arguments["account_id"].(string)
			return agent.ToolResult{
				CallID:  call.ID,
				Name:    call.Name,
				Content: fmt.Sprintf("account %s is active", accountID),
			}, nil
		},
	}

	call := agent.ToolCall{
		ID:        "call-lookup-account",
		Name:      "lookup_account",
		Arguments: map[string]any{"account_id": "demo-account"},
	}
	model := &scriptedModel{responses: []agent.ModelResponse{
		{
			Message:   agent.Message{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{call}},
			ToolCalls: []agent.ToolCall{call},
		},
		{Message: agent.Message{Role: agent.RoleAssistant, Content: "The account is active."}},
	}}

	bot, err := agent.New(agent.Config{ID: "approval-observer-example"}, model,
		agent.WithTools(lookup),
		agent.WithApprovalPolicy(agent.RequireAllApprovals(
			agent.AllowToolsApproval("lookup_account"),
			agent.AllowRisksApproval(agent.ToolRiskRead),
		)),
		agent.WithObserver(observer),
		agent.WithHook(func(ctx context.Context, event agent.Event) error {
			if event.Type == agent.EventAfterApproval {
				fmt.Printf("approval=%v reason=%q\n", event.Approved, event.ApprovalReason)
			}
			return nil
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Check the demo account.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)

	// Observations intentionally omit message content, tool arguments, and raw errors.
	for _, observation := range observer.observations {
		if observation.Type == agent.EventAfterTool || observation.Type == agent.EventAfterModel {
			fmt.Printf("observed type=%s request=%s failed=%v\n", observation.Type, observation.RequestID, observation.Failed)
		}
	}
}
