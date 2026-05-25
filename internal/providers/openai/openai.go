package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cubence/cube-agent-sdk/internal/core"
	providerdiagnostics "github.com/cubence/cube-agent-sdk/internal/providers/diagnostics"
)

type ModelRequest = core.ModelRequest
type ModelResponse = core.ModelResponse
type Message = core.Message
type ToolCall = core.ToolCall
type ToolDescriptor = core.ToolDescriptor
type Role = core.Role

const (
	RoleSystem    = core.RoleSystem
	RoleUser      = core.RoleUser
	RoleAssistant = core.RoleAssistant
	RoleTool      = core.RoleTool
)

const openAICompatibleChatCompletionsPath = "/chat/completions"

const (
	providerOpenAICompatible = "openai-compatible"
	providerOpenAIResponses  = "openai-responses"
)

// OpenAICompatibleConfig configures a chat completions endpoint that follows
// OpenAI's request and response shape.
type OpenAICompatibleConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// OpenAICompatibleModel adapts an OpenAI-compatible chat completions endpoint
// to the SDK Model interface.
type OpenAICompatibleModel struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAICompatibleModel creates a Model from OpenAI-compatible endpoint
// configuration. BaseURL may be a root URL or a full /chat/completions URL.
func NewOpenAICompatibleModel(config OpenAICompatibleConfig) (*OpenAICompatibleModel, error) {
	endpoint, err := openAICompatibleEndpoint(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("agent: openai-compatible model is required")
	}
	return &OpenAICompatibleModel{
		endpoint:   endpoint,
		apiKey:     config.APIKey,
		model:      model,
		httpClient: config.HTTPClient,
	}, nil
}

// Generate sends one chat completion request and maps the first returned choice
// into the SDK's assistant message and tool-call structures.
func (m *OpenAICompatibleModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m == nil {
		return ModelResponse{}, errors.New("agent: openai-compatible model is nil")
	}

	diagnostics := providerdiagnostics.New(providerOpenAICompatible, m.endpoint)
	payload, err := newOpenAIChatCompletionRequest(m.model, request)
	if err != nil {
		return ModelResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ModelResponse{}, core.NewProviderError("encode openai-compatible request", diagnostics, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, core.NewProviderError("create openai-compatible request", diagnostics, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if m.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return ModelResponse{}, core.NewProviderTransportError("call openai-compatible chat completions", diagnostics, err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		diagnostics := providerdiagnostics.FromResponse(providerOpenAICompatible, m.endpoint, httpResponse)
		return ModelResponse{}, core.NewProviderError(fmt.Sprintf("openai-compatible chat completions returned status %d", httpResponse.StatusCode), diagnostics, nil)
	}

	var decoded openAIChatCompletionResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		return ModelResponse{}, core.NewProviderDecodeError("decode openai-compatible response", diagnostics, err)
	}
	return decoded.modelResponse()
}

type openAIChatCompletionRequest struct {
	Model         string                             `json:"model"`
	Messages      []openAIChatMessage                `json:"messages"`
	Tools         []openAIChatCompletionTool         `json:"tools,omitempty"`
	Stream        bool                               `json:"stream,omitempty"`
	StreamOptions *openAIChatCompletionStreamOptions `json:"stream_options,omitempty"`
}

type openAIChatCompletionStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type openAIChatMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content"`
	Name       string               `json:"name,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIChatToolCall `json:"tool_calls,omitempty"`
}

type openAIChatCompletionTool struct {
	Type     string                       `json:"type"`
	Function openAIChatCompletionFunction `json:"function"`
}

type openAIChatCompletionFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIChatToolCall struct {
	Index    int                    `json:"index,omitempty"`
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type"`
	Function openAIChatToolFunction `json:"function"`
}

type openAIChatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatCompletionResponse struct {
	Choices []openAIChatCompletionChoice `json:"choices"`
	Usage   openAIChatCompletionUsage    `json:"usage"`
}

type openAIChatCompletionChoice struct {
	Message openAIChatCompletionResponseMessage `json:"message"`
}

type openAIChatCompletionResponseMessage struct {
	Role      string               `json:"role"`
	Content   *string              `json:"content"`
	ToolCalls []openAIChatToolCall `json:"tool_calls"`
}

type openAIChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func newOpenAIChatCompletionRequest(model string, request ModelRequest) (openAIChatCompletionRequest, error) {
	messages, err := openAIChatMessages(request)
	if err != nil {
		return openAIChatCompletionRequest{}, err
	}
	return openAIChatCompletionRequest{
		Model:    model,
		Messages: messages,
		Tools:    openAIChatCompletionTools(request.Tools),
	}, nil
}

func openAIChatMessages(request ModelRequest) ([]openAIChatMessage, error) {
	messages := make([]openAIChatMessage, 0, len(request.Messages)+1)
	if strings.TrimSpace(request.SystemPrompt) != "" {
		messages = append(messages, openAIChatMessage{
			Role:    string(RoleSystem),
			Content: request.SystemPrompt,
		})
	}
	for _, message := range request.Messages {
		mapped := openAIChatMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
		}
		for _, call := range message.ToolCalls {
			mappedCall, err := openAIChatToolCallFromToolCall(call)
			if err != nil {
				return nil, err
			}
			mapped.ToolCalls = append(mapped.ToolCalls, mappedCall)
		}
		messages = append(messages, mapped)
	}
	return messages, nil
}

func openAIChatCompletionTools(tools []ToolDescriptor) []openAIChatCompletionTool {
	if len(tools) == 0 {
		return nil
	}
	mapped := make([]openAIChatCompletionTool, 0, len(tools))
	for _, tool := range tools {
		parameters := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		if tool.Parameters != nil {
			parameters = tool.Parameters.JSONSchema()
		}
		mapped = append(mapped, openAIChatCompletionTool{
			Type: "function",
			Function: openAIChatCompletionFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		})
	}
	return mapped
}

func openAIChatToolCallFromToolCall(call ToolCall) (openAIChatToolCall, error) {
	arguments, err := openAIEncodeToolCallArguments(call.Arguments)
	if err != nil {
		return openAIChatToolCall{}, fmt.Errorf("agent: encode tool call arguments for %s: %w", openAIToolCallLabel(call.Name, call.ID), err)
	}
	return openAIChatToolCall{
		ID:   call.ID,
		Type: "function",
		Function: openAIChatToolFunction{
			Name:      call.Name,
			Arguments: arguments,
		},
	}, nil
}

func (r openAIChatCompletionResponse) modelResponse() (ModelResponse, error) {
	if len(r.Choices) == 0 {
		return ModelResponse{}, errors.New("agent: openai-compatible response has no choices")
	}
	message := r.Choices[0].Message
	role := Role(message.Role)
	if role == "" {
		role = RoleAssistant
	}
	content := ""
	if message.Content != nil {
		content = *message.Content
	}

	toolCalls := make([]ToolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		arguments, err := openAIParseToolCallArguments(call.Function.Arguments, call.Function.Name, call.ID)
		if err != nil {
			return ModelResponse{}, err
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: arguments,
		})
	}

	assistantMessage := Message{
		Role:      role,
		Content:   content,
		ToolCalls: core.CloneToolCalls(toolCalls),
	}
	return ModelResponse{
		Message:   assistantMessage,
		ToolCalls: toolCalls,
		Usage:     r.Usage.tokenUsage(),
	}, nil
}

func (u openAIChatCompletionUsage) tokenUsage() core.TokenUsage {
	return core.TokenUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
	}
}

func openAIParseToolCallArguments(raw string, name string, id string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("agent: openai-compatible tool call arguments for %s are empty", openAIToolCallLabel(name, id))
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
		return nil, fmt.Errorf("agent: openai-compatible tool call arguments for %s contain invalid JSON: %w", openAIToolCallLabel(name, id), err)
	}
	if arguments == nil {
		return nil, fmt.Errorf("agent: openai-compatible tool call arguments for %s must be a JSON object", openAIToolCallLabel(name, id))
	}
	return arguments, nil
}

func openAIEncodeToolCallArguments(arguments map[string]any) (string, error) {
	if arguments == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func openAICompatibleEndpoint(rawBaseURL string) (string, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return "", errors.New("agent: openai-compatible base URL is required")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return "", fmt.Errorf("agent: parse openai-compatible base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("agent: openai-compatible base URL must be absolute")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, openAICompatibleChatCompletionsPath) {
		parsed.Path = path
		return parsed.String(), nil
	}
	if path == "" {
		parsed.Path = openAICompatibleChatCompletionsPath
	} else {
		parsed.Path = path + openAICompatibleChatCompletionsPath
	}
	return parsed.String(), nil
}

func openAIToolCallLabel(name string, id string) string {
	switch {
	case name != "" && id != "":
		return fmt.Sprintf("%s (%s)", name, id)
	case name != "":
		return name
	case id != "":
		return id
	default:
		return "unknown"
	}
}
