# MCP

The SDK supports MCP in two ways: passing server metadata to model requests and
bridging stdio MCP tools into SDK `Tool` values.

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

## Stdio Tool Bridge

Use `StartMCPStdioClient` when the application wants the SDK to launch a stdio
server, discover tools, and expose them as local agent tools.

```go
client, err := agent.StartMCPStdioClient(ctx, agent.MCPServerConfig{
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

The stdio client performs initialize, lists tools, maps MCP schemas into SDK
tool schemas, calls `tools/call`, and returns MCP text content as `ToolResult`.

## Responsibilities

The SDK launches and bridges stdio servers. Applications still provide the real
server binary, environment, filesystem or network permissions, process
supervision, and approval UX.

Relevant sentinel errors include `ErrMCPProcessExited`, `ErrMCPRPC`, and
`ErrMCPToolNotFound`.
