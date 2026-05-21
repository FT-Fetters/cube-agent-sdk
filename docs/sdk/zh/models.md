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
agent 执行的 tool calls。自定义模型在已经拥有精确模型用量时，也可以设置
`ModelResponse.Usage`，使用 `TokenUsage` 表示 input、output 和 total tokens。
零值表示未报告 usage。

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
format。

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
消息上保留原始 Responses output 元数据，支持多轮工具循环。

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
使用自己的默认上限。

## 自定义模型

当应用需要内置适配器没有覆盖的 provider、重试策略、日志契约或传输行为时，
实现 `Model`。

```go
type retryingModel struct {
	next agent.Model
}

func (m retryingModel) Generate(ctx context.Context, request agent.ModelRequest) (agent.ModelResponse, error) {
	return m.next.Generate(ctx, request)
}
```

当模型实现已经拿到 provider 的精确 token 计数时，可以设置
`ModelResponse.Usage`：

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
