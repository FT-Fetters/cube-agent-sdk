package agent

import (
	"context"
	"fmt"

	mcpruntime "github.com/cubence/cube-agent-sdk/internal/mcp"
	mcpstdio "github.com/cubence/cube-agent-sdk/internal/mcp/stdio"
)

var (
	ErrMCPProcessExited = mcpruntime.ErrMCPProcessExited
	ErrMCPRPC           = mcpruntime.ErrMCPRPC
	ErrMCPToolNotFound  = mcpruntime.ErrMCPToolNotFound
)

type MCPRPCError = mcpruntime.MCPRPCError
type MCPToolDescriptor = mcpruntime.MCPToolDescriptor
type MCPContent = mcpruntime.MCPContent
type MCPToolCallResult = mcpruntime.MCPToolCallResult
type MCPStdioClient = mcpstdio.MCPStdioClient
type MCPHTTPClient = mcpruntime.Client
type MCPSSEClient = mcpruntime.Client

// MCPClient is the common runtime API for stdio, HTTP, and SSE MCP clients.
type MCPClient interface {
	ListTools(ctx context.Context) ([]MCPToolDescriptor, error)
	RefreshTools(ctx context.Context) ([]MCPToolDescriptor, error)
	Tools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]any) (MCPToolCallResult, error)
	Health(ctx context.Context) error
	Close() error
}

// StartMCPClient starts an MCP client for the configured transport.
func StartMCPClient(ctx context.Context, config MCPServerConfig) (MCPClient, error) {
	switch config.Transport {
	case "", MCPTransportStdio:
		return StartMCPStdioClient(ctx, config)
	case MCPTransportHTTP:
		return StartMCPHTTPClient(ctx, config)
	case MCPTransportSSE:
		return StartMCPSSEClient(ctx, config)
	default:
		return nil, fmt.Errorf("agent: mcp server %q uses unsupported transport %q", config.Name, config.Transport)
	}
}

// StartMCPStdioClient launches a stdio MCP server and completes initialize.
func StartMCPStdioClient(ctx context.Context, config MCPServerConfig) (*MCPStdioClient, error) {
	return mcpstdio.StartMCPStdioClient(ctx, config)
}

// StartMCPHTTPClient creates an HTTP JSON-RPC MCP client and completes initialize.
func StartMCPHTTPClient(ctx context.Context, config MCPServerConfig) (*MCPHTTPClient, error) {
	if config.Transport == "" {
		config.Transport = MCPTransportHTTP
	}
	return mcpruntime.StartHTTPClient(ctx, config)
}

// StartMCPSSEClient creates an SSE MCP client and completes initialize.
func StartMCPSSEClient(ctx context.Context, config MCPServerConfig) (*MCPSSEClient, error) {
	if config.Transport == "" {
		config.Transport = MCPTransportSSE
	}
	return mcpruntime.StartSSEClient(ctx, config)
}
