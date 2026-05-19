package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	agent "github.com/cubence/cube-agent-sdk"
)

const fakeMCPEnv = "CUBE_AGENT_EXAMPLE_MCP_STDIO"

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
	if os.Getenv(fakeMCPEnv) == "1" {
		runFakeMCPStdioServer()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}

	// The SDK launches this same example binary as a fake MCP server. Replace
	// Command, Args, and Env with your real MCP server binary in production.
	client, err := agent.StartMCPStdioClient(ctx, agent.MCPServerConfig{
		Name:      "fake-stdio",
		Command:   executable,
		Args:      []string{"mcp-server"},
		Env:       map[string]string{fakeMCPEnv: "1"},
		Transport: agent.MCPTransportStdio,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		log.Fatal(err)
	}

	call := agent.ToolCall{
		ID:        "call-mcp-echo",
		Name:      "echo",
		Arguments: map[string]any{"text": "hello"},
	}
	model := &scriptedModel{responses: []agent.ModelResponse{
		{
			Message:   agent.Message{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{call}},
			ToolCalls: []agent.ToolCall{call},
		},
		{Message: agent.Message{Role: agent.RoleAssistant, Content: "MCP echo completed."}},
	}}

	bot, err := agent.New(agent.Config{SystemPrompt: "Use MCP tools when requested."}, model,
		agent.WithTools(tools...),
		agent.WithApprovalPolicy(agent.AllowToolsApproval("echo")),
	)
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Call the MCP echo tool.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)
}

type fakeMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type fakeMCPCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func runFakeMCPStdioServer() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request fakeMCPRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			writeMCPError(nil, -32700, "parse error")
			continue
		}
		switch request.Method {
		case "initialize":
			writeMCPResult(request.ID, map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "example"},
			})
		case "notifications/initialized":
			continue
		case "tools/list":
			writeMCPResult(request.ID, map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo text through the fake MCP server",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string", "description": "Text to echo"},
					},
					"required": []string{"text"},
				},
			}}})
		case "tools/call":
			var params fakeMCPCallParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				writeMCPError(request.ID, -32602, "invalid params")
				continue
			}
			text, _ := params.Arguments["text"].(string)
			writeMCPResult(request.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "mcp echoed " + text}},
			})
		default:
			if len(request.ID) > 0 {
				writeMCPError(request.ID, -32601, "unknown method")
			}
		}
	}
}

func writeMCPResult(id json.RawMessage, result any) {
	writeMCPMessage(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(append([]byte(nil), id...)),
		"result":  result,
	})
}

func writeMCPError(id json.RawMessage, code int, message string) {
	response := map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": code, "message": message},
	}
	if len(id) > 0 {
		response["id"] = json.RawMessage(append([]byte(nil), id...))
	}
	writeMCPMessage(response)
}

func writeMCPMessage(response any) {
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
