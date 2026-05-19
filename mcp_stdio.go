package agent

import (
	"context"

	mcpstdio "github.com/cubence/cube-agent-sdk/internal/mcp/stdio"
)

var (
	ErrMCPProcessExited = mcpstdio.ErrMCPProcessExited
	ErrMCPRPC           = mcpstdio.ErrMCPRPC
	ErrMCPToolNotFound  = mcpstdio.ErrMCPToolNotFound
)

type MCPRPCError = mcpstdio.MCPRPCError
type MCPToolDescriptor = mcpstdio.MCPToolDescriptor
type MCPContent = mcpstdio.MCPContent
type MCPToolCallResult = mcpstdio.MCPToolCallResult
type MCPStdioClient = mcpstdio.MCPStdioClient

// StartMCPStdioClient launches a stdio MCP server and completes initialize.
func StartMCPStdioClient(ctx context.Context, config MCPServerConfig) (*MCPStdioClient, error) {
	return mcpstdio.StartMCPStdioClient(ctx, config)
}
