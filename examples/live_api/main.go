package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	agent "github.com/cubence/cube-agent-sdk"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Keep real provider settings in environment variables so secrets never need
	// to be committed with the example.
	config, err := liveModelConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	model, err := agent.NewModel(config)
	if err != nil {
		log.Fatal(err)
	}

	echoTool := agent.ToolFunc{
		ToolName:        "echo",
		ToolDescription: "Echo text back to verify tool calling works",
		ToolRisk:        agent.ToolRiskRead,
		Parameters: &agent.ToolParametersSchema{
			Type:     agent.SchemaTypeObject,
			Required: []string{"text"},
			Properties: map[string]agent.ToolParametersSchema{
				"text": {
					Type:        agent.SchemaTypeString,
					Description: "Text to echo back",
				},
			},
		},
		Fn: func(ctx context.Context, call agent.ToolCall) (agent.ToolResult, error) {
			text, _ := call.Arguments["text"].(string)
			return agent.ToolResult{
				CallID:  call.ID,
				Name:    call.Name,
				Content: "echo tool received: " + text,
			}, nil
		},
	}

	var observations int
	bot, err := agent.New(agent.Config{
		SystemPrompt: "You are testing an SDK. If useful, call the echo tool exactly once.",
	}, model,
		agent.WithTools(echoTool),
		agent.WithApprovalPolicy(agent.RequireAllApprovals(
			agent.AllowToolsApproval("echo"),
			agent.AllowRisksApproval(agent.ToolRiskRead),
		)),
		agent.WithObserver(agent.ObserverFunc(func(ctx context.Context, observation agent.Observation) {
			// Print event metadata only. Avoid logging prompts, tool arguments, or
			// provider credentials in live test output.
			observations++
			fmt.Println(formatObservationLine(observations, time.Now(), observation))
		})),
	)
	if err != nil {
		log.Fatal(err)
	}

	prompt := strings.TrimSpace(os.Getenv("LIVE_PROMPT"))
	if prompt == "" {
		prompt = `Call the echo tool with text "live api check", then summarize the tool result in one sentence.`
	}

	reply, err := bot.Run(ctx, prompt)
	if err != nil {
		var agentErr *agent.AgentError
		if errors.As(err, &agentErr) {
			log.Printf("agent error category=%s operation=%s request=%s", agentErr.Category, agentErr.Operation, agentErr.RequestID)
		}
		log.Fatal(err)
	}

	fmt.Println("assistant:", reply.Content)
	fmt.Println("observations:", observations)
}

func liveModelConfigFromEnv() (agent.ModelConfig, error) {
	apiType := agent.ModelAPIType(strings.TrimSpace(os.Getenv("MODEL_API_TYPE")))
	if apiType == "" {
		apiType = agent.ModelAPIAnthropicMessages
	}

	baseURL := strings.TrimSpace(os.Getenv("MODEL_BASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("MODEL_API_KEY"))
	modelName := strings.TrimSpace(os.Getenv("MODEL_NAME"))

	var missing []string
	if baseURL == "" {
		missing = append(missing, "MODEL_BASE_URL")
	}
	if apiKey == "" {
		missing = append(missing, "MODEL_API_KEY")
	}
	if modelName == "" {
		missing = append(missing, "MODEL_NAME")
	}
	if len(missing) > 0 {
		return agent.ModelConfig{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return agent.ModelConfig{
		APIType:          apiType,
		BaseURL:          baseURL,
		APIKey:           apiKey,
		Model:            modelName,
		AnthropicVersion: strings.TrimSpace(os.Getenv("ANTHROPIC_VERSION")),
	}, nil
}

func formatObservationLine(sequence int, observedAt time.Time, observation agent.Observation) string {
	status := "ok"
	if observation.Failed {
		status = "failed"
	}

	fields := []string{
		fmt.Sprintf("observation=%d", sequence),
		fmt.Sprintf("time=%s", observedAt.UTC().Format(time.RFC3339Nano)),
		fmt.Sprintf("event=%s", observation.Type),
		fmt.Sprintf("status=%s", status),
	}
	add := func(name string, value any) {
		fields = append(fields, fmt.Sprintf("%s=%v", name, value))
	}

	if observation.AgentID != "" {
		add("agent", observation.AgentID)
	}
	if observation.SubagentID != "" {
		add("subagent", observation.SubagentID)
	}
	if observation.RequestID != "" {
		add("request", observation.RequestID)
	}
	if observation.Round > 0 {
		add("round", observation.Round)
	}
	if observation.Duration > 0 {
		add("duration", observation.Duration)
	}
	if observation.EstimatedTokens > 0 {
		add("estimated_tokens", observation.EstimatedTokens)
	}
	if observation.ToolName != "" {
		add("tool", observation.ToolName)
	}
	if observation.ToolRisk != "" {
		add("risk", observation.ToolRisk)
	}
	if observation.SkillName != "" {
		add("skill", observation.SkillName)
	}
	if observation.Approved || observation.ApprovalReason != "" || observation.Type == agent.EventAfterApproval {
		add("approved", observation.Approved)
	}
	if observation.ApprovalReason != "" {
		fields = append(fields, fmt.Sprintf("approval_reason=%q", observation.ApprovalReason))
	}
	if observation.ErrorCategory != "" {
		add("error_category", observation.ErrorCategory)
	}

	return strings.Join(fields, " ")
}
