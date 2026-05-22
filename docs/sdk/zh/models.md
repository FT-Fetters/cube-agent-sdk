# 模型

模型负责把 `ModelRequest` 转换为 `ModelResponse`。SDK 提供常见 wire protocol
的 provider 适配器，同时允许应用自行实现 `Model`。

## 接口

```go
type Model interface {
	Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error)
}

type StreamModel interface {
	Model
	Stream(context.Context, agent.ModelRequest) (<-chan agent.StreamEvent, error)
}
```

`ModelRequest` 包含组装后的 system prompt、会话消息、工具描述、MCP server
元数据和 active skills。`ModelResponse` 包含最终 assistant 消息，或要求
agent 执行的 tool calls。自定义模型在已经拥有精确模型用量时，可以为非 streaming
调用设置 `ModelResponse.Usage`，也可以在 streaming 调用的最终 done event 上设置
`StreamEvent.Usage`。零值表示未报告 usage。

## 模型工厂

当 provider protocol 来自配置时，使用 `NewModel`。

```go
model, err := agent.NewModel(agent.ModelConfig{
	APIType: agent.ModelAPIAnthropicMessages,
	BaseURL: os.Getenv("MODEL_BASE_URL"),
	APIKey:  os.Getenv("MODEL_API_KEY"),
	Model:   os.Getenv("MODEL_NAME"),
})
```

支持的 `APIType`：

- `ModelAPIOpenAICompatible`
- `ModelAPIOpenAIResponses`
- `ModelAPIAnthropicMessages`

不支持的值会返回 `ErrModelAPIUnsupported`。


## Provider 能力矩阵

内置 capability 声明描述的是 protocol-level adapter support，不保证该协议背后的每个
远端模型都启用同样能力。应用可以用 `CapabilitiesOf(model)` 读取声明，用
`ModelCapabilities.Supports` 做判断，或用 `SelectModelByCapabilities` 在创建 agent
前选择符合要求的模型或降级模型。

| Provider API | Tools | Streaming | JSON mode | Structured output | Reasoning metadata | Parallel tool calls | `MCPServerMetadata` | `ModelHandledMCP` | Token usage |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `ModelAPIOpenAICompatible` | Yes | Yes | No | No | No | Yes | No | No | Yes |
| `ModelAPIOpenAIResponses` | Yes | Yes | No | No | Yes | Yes | No | No | Yes |
| `ModelAPIAnthropicMessages` | Yes | Yes | No | No | No | Yes | No | No | Yes |

`MCPServerMetadata` 表示适配器会消费 `ModelRequest.MCPServers`。
`ModelHandledMCP` 表示远端模型或 provider 负责访问 MCP server。当前内置适配器会暴露
本地 tool descriptors，但不会把直接 MCP server 元数据发送给 provider；使用这些适配器
时，应把 SDK 管理的 MCP clients 作为 tools 挂载。`TokenUsage` 表示 provider 报告 usage
时适配器会映射它；零值仍表示本次调用没有可用 usage。

当模型声明 capabilities 后，agent 会在 model call 前检查明显不兼容的配置。配置 tools
需要 `Tools`，直接配置 MCP servers 需要 `MCPServerMetadata`，`RunStream` 需要
`Streaming`。不匹配时返回结构化错误，可用
`errors.Is(err, agent.ErrCapabilityMismatch)` 判断，并可用 `errors.As` 提取
`*agent.CapabilityMismatchError`。没有实现 `ModelCapabilitiesProvider` 的自定义模型保持
之前的宽松兼容行为。

## OpenAI-Compatible Chat Completions

对暴露标准 `/chat/completions` 请求和响应结构的 provider，使用
`NewOpenAICompatibleModel`。

```go
model, err := agent.NewOpenAICompatibleModel(agent.OpenAICompatibleConfig{
	BaseURL:    "https://api.openai.com/v1",
	APIKey:     os.Getenv("OPENAI_API_KEY"),
	Model:      "gpt-4.1",
	HTTPClient: httpClient,
})
```

`BaseURL` 可以是 provider root，也可以是完整的 `/chat/completions` URL。该
适配器会把 SDK messages、tools 和 tool calls 映射到 chat completions wire
format。它也实现了 `StreamModel`，会发送 `stream` 并通过
`stream_options.include_usage` 请求最终 usage chunk。当 provider 返回
`usage.prompt_tokens`、`usage.completion_tokens` 和 `usage.total_tokens` 时，
适配器会映射到 `ModelResponse.Usage` 或最终 `StreamEvent.Usage`。

## OpenAI Responses API

使用 `NewOpenAIResponsesModel`，或在 `NewModel` 中选择
`ModelAPIOpenAIResponses`。

```go
store := false
model, err := agent.NewOpenAIResponsesModel(agent.OpenAIResponsesConfig{
	BaseURL:   "https://api.openai.com",
	APIKey:    os.Getenv("OPENAI_API_KEY"),
	Model:     os.Getenv("OPENAI_RESPONSES_MODEL"),
	MaxTokens: 4096,
	Store:     &store,
})
```

`BaseURL` 可以是 API root、`/v1` URL，或完整的 `/v1/responses` URL。该适配器
把 SDK system prompt 映射到 `instructions`，把 tools 映射为 Responses
function tools，把 tool results 映射为 `function_call_output`，并在 assistant
消息上保留原始 Responses output 元数据，支持多轮工具循环。它也通过
`response.output_text.delta`、`response.completed` 等 Responses semantic streaming
events 实现了 `StreamModel`。当响应包含 token usage 时，适配器会把常见的
input、output 和 total token 字段映射到 `ModelResponse.Usage` 或最终
`StreamEvent.Usage`。

## Anthropic Messages

使用 `NewAnthropicMessagesModel`，或在 `NewModel` 中选择
`ModelAPIAnthropicMessages`。

```go
model, err := agent.NewAnthropicMessagesModel(agent.AnthropicMessagesConfig{
	BaseURL:          "https://api.anthropic.com",
	APIKey:           os.Getenv("ANTHROPIC_API_KEY"),
	Model:            os.Getenv("ANTHROPIC_MODEL"),
	MaxTokens:        4096,
	AnthropicVersion: "2023-06-01",
})
```

`BaseURL` 可以是 provider root、`/v1` URL，或完整的 `/v1/messages` URL。如果
`AnthropicVersion` 为空，适配器使用 `2023-06-01`。如果 `MaxTokens` 为空，适配器
使用自己的默认上限。它也通过 Anthropic `content_block_delta`、`message_delta`
和 `message_stop` SSE events 实现了 `StreamModel`。当 Anthropic 返回
`usage.input_tokens` 和 `usage.output_tokens` 时，适配器会映射到
`ModelResponse.Usage` 或最终 `StreamEvent.Usage`；如果 provider 没有报告 total，
则由 input 和 output 相加得到。

## 可靠性模型 Wrapper

当应用需要在任意模型适配器外增加无依赖的可靠性控制时，使用 `NewReliableModel`。
wrapper 支持最大尝试次数、单次尝试 timeout、总 timeout、自定义 backoff、固定窗口
rate limit、circuit breaker、token budget、cost budget，以及安全的 `ReliabilityEvent`
回调。

```go
model := agent.NewReliableModel(baseModel,
	agent.WithReliableMaxAttempts(3),
	agent.WithReliablePerAttemptTimeout(15*time.Second),
	agent.WithReliableTotalTimeout(45*time.Second),
	agent.WithReliableBackoff(func(attempt int) time.Duration {
		return time.Duration(attempt) * time.Second
	}),
	agent.WithReliableRateLimit(60, time.Minute),
	agent.WithReliableCircuitBreaker(5, time.Minute),
	agent.WithReliableTokenBudget(200_000),
	agent.WithReliableCostBudget(25, 0.01, 0.03),
	agent.WithReliabilityObserver(func(ctx context.Context, event agent.ReliabilityEvent) {
		// 只导出安全 event metadata。
	}),
)
```

默认 retry 分类是保守的：只 retry timeout、HTTP 408/429、以及 5xx provider
diagnostics 或 model subcategory。bad request、auth failure、validation error、approval
error、tool error 和其他不可重试类别默认不会 retry。

如果被包装模型实现了 `StreamModel`，返回的 wrapper 也会实现 `StreamModel`。streaming
可靠性会在 stream start 前执行检查，并可 retry 安全的启动失败；delta 开始后不会重放
stream。token budget 在尝试前使用 estimated input tokens，并在存在
`ModelResponse.Usage` 或最终 `StreamEvent.Usage` 时进行校正；cost budget 使用调用方提供的
每 1K token 价格。

## 自定义模型

当应用需要内置适配器没有覆盖的 provider、日志契约或传输行为时，实现 `Model`。

```go
type retryingModel struct {
	next agent.Model
}

func (m retryingModel) Generate(ctx context.Context, request agent.ModelRequest) (agent.ModelResponse, error) {
	return m.next.Generate(ctx, request)
}
```

当模型实现已经拿到 provider 的精确 token 计数时，可以设置
`ModelResponse.Usage` 或最终 `StreamEvent.Usage`：

```go
return agent.ModelResponse{
	Message: agent.Message{Role: agent.RoleAssistant, Content: "done"},
	Usage: agent.TokenUsage{
		InputTokens:  120,
		OutputTokens: 34,
		TotalTokens:  154,
	},
}, nil
```

不要把 provider secret 和原始 provider 错误写入面向用户的日志。需要 timeout、
proxy、tracing 或 transport 控制时，提供自定义 `HTTPClient`。
