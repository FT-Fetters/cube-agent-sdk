# MCP

SDK 通过两种方式支持 MCP：把 server 元数据传给模型请求，或把 stdio MCP tools
桥接成 SDK `Tool`。

## 由模型处理的 MCP 元数据

当模型适配器或 provider 负责处理 MCP servers 时，使用 `WithMCPServers`。

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

SDK 会把配置的 servers 放进 `ModelRequest.MCPServers`。适配器决定是否以及如何
使用这些元数据。

## Stdio 工具桥接

当应用希望 SDK 启动 stdio server、发现工具并把它们暴露为本地 agent tools 时，
使用 `StartMCPStdioClient`。

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

stdio client 会执行 initialize、列出 tools、把 MCP schemas 映射为 SDK tool
schemas、调用 `tools/call`，并把 MCP text content 返回为 `ToolResult`。

## 职责边界

SDK 负责启动和桥接 stdio servers。真实 server 二进制、环境、文件系统或网络
权限、进程监管和审批 UX 仍由应用提供。

相关哨兵错误包括 `ErrMCPProcessExited`、`ErrMCPRPC` 和 `ErrMCPToolNotFound`。
