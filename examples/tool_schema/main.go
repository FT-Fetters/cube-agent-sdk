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

func main() {
	ctx := context.Background()

	searchTool := agent.ToolFunc{
		ToolName:        "search_docs",
		ToolDescription: "Search a small documentation index",
		ToolRisk:        agent.ToolRiskRead,
		Parameters: &agent.ToolParametersSchema{
			Type:     agent.SchemaTypeObject,
			Required: []string{"query"},
			Properties: map[string]agent.ToolParametersSchema{
				"query": {Type: agent.SchemaTypeString, Description: "Search query"},
				"limit": {Type: agent.SchemaTypeInteger, Description: "Maximum number of results"},
			},
		},
		Fn: func(ctx context.Context, call agent.ToolCall) (agent.ToolResult, error) {
			query, _ := call.Arguments["query"].(string)
			limit, _ := call.Arguments["limit"].(int)
			if limit == 0 {
				limit = 1
			}
			return agent.ToolResult{
				CallID:  call.ID,
				Name:    call.Name,
				Content: fmt.Sprintf("top %d result for %q: README.md", limit, query),
			}, nil
		},
	}

	call := agent.ToolCall{
		ID:        "call-search-docs",
		Name:      "search_docs",
		Arguments: map[string]any{"query": "streaming", "limit": 1},
	}
	model := &scriptedModel{responses: []agent.ModelResponse{
		{
			Message:   agent.Message{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{call}},
			ToolCalls: []agent.ToolCall{call},
		},
		{Message: agent.Message{Role: agent.RoleAssistant, Content: "README.md documents streaming support."}},
	}}

	bot, err := agent.New(agent.Config{SystemPrompt: "Use tools when helpful."}, model,
		agent.WithTools(searchTool),
		agent.WithApprovalPolicy(agent.RequireAllApprovals(
			agent.AllowToolsApproval("search_docs"),
			agent.AllowRisksApproval(agent.ToolRiskRead),
		)),
	)
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Find the streaming documentation.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)
}
