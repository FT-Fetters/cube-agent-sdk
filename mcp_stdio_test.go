package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestMCPStdioClientDiscoversToolsAndBridgesAgentCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := StartMCPStdioClient(ctx, fakeMCPServerConfig("ok"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	if tools[0].Name() != "echo" || tools[0].Description() != "Echo text through MCP" {
		t.Fatalf("tool metadata = %q/%q, want echo metadata", tools[0].Name(), tools[0].Description())
	}
	schemaProvider, ok := tools[0].(ToolParametersSchemaProvider)
	if !ok {
		t.Fatal("bridged MCP tool did not expose a parameter schema")
	}
	gotSchema := schemaProvider.ParametersSchema()
	wantSchema := &ToolParametersSchema{
		Type:     SchemaTypeObject,
		Required: []string{"text"},
		Properties: map[string]ToolParametersSchema{
			"text": {Type: SchemaTypeString, Description: "Text to echo"},
		},
	}
	if !reflect.DeepEqual(gotSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", gotSchema, wantSchema)
	}

	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "mcp-call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}}}},
		{Message: Message{Role: RoleAssistant, Content: "done"}},
	}}
	bot, err := New(Config{SystemPrompt: "base"}, model, WithTools(tools...))
	if err != nil {
		t.Fatal(err)
	}

	response, err := bot.Run(ctx, "use the MCP echo tool")
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "done" {
		t.Fatalf("response content = %q, want done", response.Content)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(model.requests))
	}
	if len(model.requests[0].Tools) != 1 || model.requests[0].Tools[0].Name != "echo" {
		t.Fatalf("model tools = %#v, want bridged echo tool", model.requests[0].Tools)
	}
	last := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	if last.Role != RoleTool || last.ToolCallID != "mcp-call-1" || last.Name != "echo" || last.Content != "mcp echoed hello" {
		t.Fatalf("last message = %#v, want MCP tool result", last)
	}
	if got := last.Metadata["mcpServer"]; got != "fake" {
		t.Fatalf("mcp server metadata = %#v, want fake", got)
	}
}

func TestMCPStdioClientReturnsRPCError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := StartMCPStdioClient(ctx, fakeMCPServerConfig("rpc_error"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Tools(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = client.CallTool(ctx, "explode", map[string]any{"text": "boom"})
	if !errors.Is(err, ErrMCPRPC) {
		t.Fatalf("err = %v, want ErrMCPRPC", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryMCP || agentErr.Operation != "mcp.tool.call" {
		t.Fatalf("agent error category/operation = %q/%q, want mcp/mcp.tool.call", agentErr.Category, agentErr.Operation)
	}
	var rpcErr *MCPRPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err = %T, want *MCPRPCError", err)
	}
	if rpcErr.Code != -32000 || rpcErr.Message != "tool exploded" {
		t.Fatalf("rpc error = %#v, want code/message from server", rpcErr)
	}
}

func TestMCPStdioClientReturnsToolNotFoundForUnknownTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := StartMCPStdioClient(ctx, fakeMCPServerConfig("ok"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.CallTool(ctx, "missing", map[string]any{"text": "hello"})
	if !errors.Is(err, ErrMCPToolNotFound) {
		t.Fatalf("err = %v, want ErrMCPToolNotFound", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryMCP || agentErr.Operation != "mcp.tool.lookup" || agentErr.ToolName != "missing" {
		t.Fatalf("agent error context = %#v, want MCP lookup context for missing tool", agentErr)
	}
}

func TestMCPStdioClientReportsProcessExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := StartMCPStdioClient(ctx, fakeMCPServerConfig("exit_on_list"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.Tools(ctx)
	if !errors.Is(err, ErrMCPProcessExited) {
		t.Fatalf("err = %v, want ErrMCPProcessExited", err)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryMCP || agentErr.Operation != "mcp.tools.list" {
		t.Fatalf("agent error category/operation = %q/%q, want mcp/mcp.tools.list", agentErr.Category, agentErr.Operation)
	}
}

func TestMCPStdioFakeServer(t *testing.T) {
	mode := os.Getenv("CUBE_AGENT_FAKE_MCP_STDIO")
	if mode == "" {
		return
	}
	runFakeMCPStdioServer(mode)
	os.Exit(0)
}

func fakeMCPServerConfig(mode string) MCPServerConfig {
	return MCPServerConfig{
		Name:      "fake",
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestMCPStdioFakeServer"},
		Env:       map[string]string{"CUBE_AGENT_FAKE_MCP_STDIO": mode},
		Transport: MCPTransportStdio,
	}
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

func runFakeMCPStdioServer(mode string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request fakeMCPRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			fakeMCPWriteError(nil, -32700, "parse error")
			continue
		}

		switch request.Method {
		case "initialize":
			fakeMCPWriteResult(request.ID, map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities": map[string]any{
					"tools": map[string]any{"listChanged": false},
				},
				"serverInfo": map[string]any{
					"name":    "fake-mcp",
					"version": "test",
				},
			})
		case "notifications/initialized":
			continue
		case "tools/list":
			if mode == "exit_on_list" {
				os.Exit(0)
			}
			fakeMCPWriteResult(request.ID, map[string]any{
				"tools": fakeMCPTools(mode),
			})
		case "tools/call":
			var params fakeMCPCallParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				fakeMCPWriteError(request.ID, -32602, "invalid call params")
				continue
			}
			if mode == "rpc_error" || params.Name == "explode" {
				fakeMCPWriteError(request.ID, -32000, "tool exploded")
				continue
			}
			if params.Name != "echo" {
				fakeMCPWriteError(request.ID, -32602, "unknown tool")
				continue
			}
			text, _ := params.Arguments["text"].(string)
			fakeMCPWriteResult(request.ID, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "mcp echoed " + text},
				},
				"isError": false,
			})
		default:
			if len(request.ID) > 0 {
				fakeMCPWriteError(request.ID, -32601, fmt.Sprintf("unknown method %s", request.Method))
			}
		}
	}
}

func fakeMCPTools(mode string) []map[string]any {
	name := "echo"
	description := "Echo text through MCP"
	if mode == "rpc_error" {
		name = "explode"
		description = "Always returns a JSON-RPC error"
	}
	return []map[string]any{{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Text to echo",
				},
			},
			"required": []string{"text"},
		},
	}}
}

func fakeMCPWriteResult(id json.RawMessage, result any) {
	fakeMCPWrite(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(append([]byte(nil), id...)),
		"result":  result,
	})
}

func fakeMCPWriteError(id json.RawMessage, code int, message string) {
	response := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	if len(id) > 0 {
		response["id"] = json.RawMessage(append([]byte(nil), id...))
	}
	fakeMCPWrite(response)
}

func fakeMCPWrite(response any) {
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
