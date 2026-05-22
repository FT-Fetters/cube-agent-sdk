# MCP

SDK 通过两种方式支持 MCP：把 server 元数据传给模型请求，或通过 stdio、HTTP
JSON-RPC、实用的 SSE endpoint 发现流程把 MCP tools 桥接成 SDK `Tool`。

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

## SDK 工具桥接

当应用希望 SDK 连接 MCP server、发现工具并把它们暴露为本地 agent tools 时，
使用 `StartMCPClient`。

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

通用构造函数会根据 `MCPServerConfig.Transport` 选择 transport。也可以直接使用
`StartMCPStdioClient`、`StartMCPHTTPClient` 和 `StartMCPSSEClient`。

HTTP client 会把 JSON-RPC requests POST 到 `MCPServerConfig.URL`：

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "remote-tools",
	URL:       "https://mcp.example.com/rpc",
	Transport: agent.MCPTransportHTTP,
})
```

SSE client 会连接 `MCPServerConfig.URL`，读取 `endpoint` event，然后把 JSON-RPC
requests 发送到发现的 HTTP endpoint：

```go
client, err := agent.StartMCPClient(ctx, agent.MCPServerConfig{
	Name:      "remote-tools",
	URL:       "https://mcp.example.com/events",
	Transport: agent.MCPTransportSSE,
})
```

所有由 SDK 管理的 clients 都会执行 initialize，支持 `tools/list` 分页，把 MCP
schemas 映射为 SDK tool schemas，调用 `tools/call`，把 MCP text content 返回为
`ToolResult`，通过 `RefreshTools(ctx)` 刷新已知工具，通过 `Health(ctx)` 探测
server，并通过 `Close()` 清理资源。HTTP 和 SSE 的启动、列表和健康检查会对临时
网络错误、HTTP 408/429 和 5xx 响应使用短重试/backoff；工具调用不会重试，以免
重复产生副作用。

## 职责边界

真实 server 二进制或 URL、凭证、环境、文件系统或网络权限、进程监管和审批 UX
仍由应用提供。SDK 的 diagnostics 和 error strings 不包含 MCP environment values、
URL query strings、原始 HTTP response bodies、工具参数或工具结果。

相关哨兵错误包括 `ErrMCPProcessExited`、`ErrMCPRPC` 和 `ErrMCPToolNotFound`。
