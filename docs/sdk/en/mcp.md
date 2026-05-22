# MCP

The SDK supports MCP in two ways: passing server metadata to model requests and
bridging MCP tools into SDK `Tool` values over stdio, HTTP JSON-RPC, or a
pragmatic SSE endpoint-discovery flow.

## Model-Handled MCP Metadata

Use `WithMCPServers` when the model adapter or provider handles MCP servers.

```go
bot, err := agent.New(cfg, model,
	agent.WithMCPServers(agent.MCPServerConfig{
		Name:      "filesystem",
		Command:   "mcp-filesystem",
		Args:      []string{"--root", "."},
		Env:       map[string]string{"MODE": "readonly"},
		Transport: agent.MCPTransportStdio,
	}),
)
```

The SDK includes configured servers in `ModelRequest.MCPServers`. The adapter
decides whether and how to use that metadata.

## SDK Tool Bridge

Use `StartMCPClient` when the application wants the SDK to connect to an MCP
server, discover tools, and expose them as local agent tools.

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "filesystem",
	Command:   os.Getenv("MCP_FILESYSTEM_COMMAND"),
	Args:      []string{"--root", os.Getenv("MCP_FILESYSTEM_ROOT")},
	Transport: agent.MCPTransportStdio,
})
if err != nil {
	return err
}
defer client.Close()

tools, err := client.Tools(ctx)
if err != nil {
	return err
}

bot, err := agent.New(cfg, model,
	agent.WithTools(tools...),
	agent.WithApprovalPolicy(agent.AllowToolsApproval("read_file")),
)
```

The generic constructor selects the transport from `MCPServerConfig.Transport`.
The transport-specific constructors are `StartMCPStdioClient`,
`StartMCPHTTPClient`, and `StartMCPSSEClient`.

HTTP clients send JSON-RPC requests to `MCPServerConfig.URL`:

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "remote-tools",
	URL:       "https://mcp.example.com/rpc",
	Transport: agent.MCPTransportHTTP,
})
```

SSE clients connect to `MCPServerConfig.URL`, read an `endpoint` event, and then
send JSON-RPC requests to that discovered HTTP endpoint:

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "remote-tools",
	URL:       "https://mcp.example.com/events",
	Transport: agent.MCPTransportSSE,
})
```

All SDK-managed clients perform initialize, support `tools/list` pagination,
map MCP schemas into SDK tool schemas, call `tools/call`, return MCP text
content as `ToolResult`, refresh cached descriptors with `RefreshTools(ctx)`,
probe the server with `Health(ctx)`, and release resources with `Close()`.
HTTP and SSE startup, list, and health operations use a short retry/backoff
window for transient network failures, HTTP 408/429, and 5xx responses. Tool
calls are not retried to avoid duplicating side effects.

## Responsibilities

Applications provide the real server binary or URL, credentials, environment,
filesystem or network permissions, process supervision, and approval UX. The SDK
keeps MCP environment values, URL query strings, raw HTTP response bodies, tool
arguments, and tool results out of diagnostics and error strings.

Relevant sentinel errors include `ErrMCPProcessExited`, `ErrMCPRPC`, and
`ErrMCPToolNotFound`.
