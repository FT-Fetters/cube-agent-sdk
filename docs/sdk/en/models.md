# Models

Models are responsible for converting `ModelRequest` values into
`ModelResponse` values. The SDK includes provider adapters for common wire
protocols and still allows applications to implement their own `Model`.

## Interfaces

```go
type Model interface {
	Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error)
}

type StreamModel interface {
	Model
	Stream(context.Context, agent.ModelRequest) (<-chan agent.StreamEvent, error)
}
```

`ModelRequest` contains the assembled system prompt, conversation messages,
tool descriptors, MCP server metadata, and active skills. `ModelResponse`
contains either a final assistant message or tool calls for the agent to run.
Custom models can also set `ModelResponse.Usage` with `TokenUsage` values for
input, output, and total tokens when exact model usage is available. The zero
value means usage was not reported.

## Model Factory

Use `NewModel` when the provider protocol should come from configuration.

```go
model, err := agent.NewModel(agent.ModelConfig{
	APIType: agent.ModelAPIAnthropicMessages,
	BaseURL: os.Getenv("MODEL_BASE_URL"),
	APIKey:  os.Getenv("MODEL_API_KEY"),
	Model:   os.Getenv("MODEL_NAME"),
})
```

Supported `APIType` values:

- `ModelAPIOpenAICompatible`
- `ModelAPIOpenAIResponses`
- `ModelAPIAnthropicMessages`

Unsupported values return `ErrModelAPIUnsupported`.

## OpenAI-Compatible Chat Completions

Use `NewOpenAICompatibleModel` for providers that expose the standard
`/chat/completions` request and response shape.

```go
model, err := agent.NewOpenAICompatibleModel(agent.OpenAICompatibleConfig{
	BaseURL:    "https://api.openai.com/v1",
	APIKey:     os.Getenv("OPENAI_API_KEY"),
	Model:      "gpt-4.1",
	HTTPClient: httpClient,
})
```

`BaseURL` may be a provider root or a full `/chat/completions` URL. The adapter
maps SDK messages, tools, and tool calls to the chat completions wire format.

## OpenAI Responses API

Use `NewOpenAIResponsesModel` or `NewModel` with `ModelAPIOpenAIResponses`.

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

`BaseURL` may be an API root, a `/v1` URL, or a full `/v1/responses` URL. The
adapter maps the SDK system prompt to `instructions`, tools to Responses
function tools, tool results to `function_call_output`, and preserves raw
Responses output metadata on assistant messages for multi-round tool loops.

## Anthropic Messages

Use `NewAnthropicMessagesModel` or `NewModel` with
`ModelAPIAnthropicMessages`.

```go
model, err := agent.NewAnthropicMessagesModel(agent.AnthropicMessagesConfig{
	BaseURL:          "https://api.anthropic.com",
	APIKey:           os.Getenv("ANTHROPIC_API_KEY"),
	Model:            os.Getenv("ANTHROPIC_MODEL"),
	MaxTokens:        4096,
	AnthropicVersion: "2023-06-01",
})
```

`BaseURL` may be a provider root, a `/v1` URL, or a full `/v1/messages` URL.
If `AnthropicVersion` is empty, the adapter uses `2023-06-01`. If `MaxTokens`
is empty, the adapter uses its default maximum.

## Custom Models

Implement `Model` when an application needs a provider, retry policy, logging
contract, or transport behavior that is not built in.

```go
type retryingModel struct {
	next agent.Model
}

func (m retryingModel) Generate(ctx context.Context, request agent.ModelRequest) (agent.ModelResponse, error) {
	return m.next.Generate(ctx, request)
}
```

Set `ModelResponse.Usage` when your model implementation already has exact
provider token counts:

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

Keep provider secrets and raw provider errors out of user-facing logs. Use a
custom `HTTPClient` for timeouts, proxies, tracing, or transport controls.
