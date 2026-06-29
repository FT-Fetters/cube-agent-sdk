package anthropic

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

const (
	anthropicMessagesPath     = "/v1/messages"
	defaultAnthropicVersion   = "2023-06-01"
	defaultAnthropicMaxTokens = 4096
	providerAnthropicMessages = "anthropic-messages"
	// Anthropic verifies thinking signatures when tool results continue an assistant turn.
	anthropicMessagesContentMetadataKey = "anthropic_messages_content"
)

const (
	DefaultVersion   = defaultAnthropicVersion
	DefaultMaxTokens = defaultAnthropicMaxTokens
)

type ModelRequest = core.ModelRequest
type ModelResponse = core.ModelResponse
type Message = core.Message
type ToolCall = core.ToolCall
type ToolDescriptor = core.ToolDescriptor

const (
	RoleUser      = core.RoleUser
	RoleAssistant = core.RoleAssistant
	RoleTool      = core.RoleTool
	RoleSystem    = core.RoleSystem
)

var cloneAnyMap = core.CloneAnyMap

// AnthropicThinkingConfig configures Anthropic extended or adaptive thinking.
type AnthropicThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

// AnthropicMessagesConfig configures an Anthropic Messages API endpoint.
type AnthropicMessagesConfig struct {
	BaseURL          string
	APIKey           string
	Model            string
	HTTPClient       *http.Client
	MaxTokens        int
	AnthropicVersion string
	Thinking         *AnthropicThinkingConfig
}

// AnthropicMessagesModel adapts the Anthropic Messages API to the SDK Model interface.
type AnthropicMessagesModel struct {
	endpoint         string
	apiKey           string
	model            string
	httpClient       *http.Client
	maxTokens        int
	anthropicVersion string
	thinking         *AnthropicThinkingConfig
}

// NewAnthropicMessagesModel creates a Model from Anthropic Messages API configuration.
// BaseURL may be a root URL, /v1 URL, or full /v1/messages URL.
func NewAnthropicMessagesModel(config AnthropicMessagesConfig) (*AnthropicMessagesModel, error) {
	endpoint, err := anthropicMessagesEndpoint(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("agent: anthropic messages model is required")
	}
	maxTokens := config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}
	version := strings.TrimSpace(config.AnthropicVersion)
	if version == "" {
		version = defaultAnthropicVersion
	}
	return &AnthropicMessagesModel{
		endpoint:         endpoint,
		apiKey:           config.APIKey,
		model:            model,
		httpClient:       config.HTTPClient,
		maxTokens:        maxTokens,
		anthropicVersion: version,
		thinking:         cloneAnthropicThinkingConfig(config.Thinking),
	}, nil
}

// Generate sends one Anthropic Messages request and maps text/tool_use content
// blocks into the SDK's assistant message and tool-call structures.
func (m *AnthropicMessagesModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m == nil {
		return ModelResponse{}, errors.New("agent: anthropic messages model is nil")
	}
	diagnostics := providerdiagnostics.New(providerAnthropicMessages, m.endpoint)
	payload, err := newAnthropicMessagesRequest(m.model, m.maxTokens, m.thinking, request)
	if err != nil {
		return ModelResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ModelResponse{}, core.NewProviderError("encode anthropic messages request", diagnostics, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, core.NewProviderError("create anthropic messages request", diagnostics, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("anthropic-version", m.anthropicVersion)
	if m.apiKey != "" {
		httpRequest.Header.Set("x-api-key", m.apiKey)
	}

	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return ModelResponse{}, core.NewProviderTransportError("call anthropic messages", diagnostics, err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		diagnostics := providerdiagnostics.FromResponse(providerAnthropicMessages, m.endpoint, httpResponse)
		message := fmt.Sprintf("anthropic messages returned status %d", httpResponse.StatusCode)
		sensitiveValues := providerdiagnostics.SensitiveValuesFromModelRequest(request, m.apiKey, m.endpoint)
		if summary := providerdiagnostics.ErrorSummaryFromResponse(httpResponse, sensitiveValues...); summary != "" {
			message += ": " + summary
		}
		return ModelResponse{}, core.NewProviderError(message, diagnostics, nil)
	}

	var decoded anthropicMessagesResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		return ModelResponse{}, core.NewProviderDecodeError("decode anthropic messages response", diagnostics, err)
	}
	return decoded.modelResponse()
}

type anthropicMessagesRequest struct {
	Model     string                   `json:"model"`
	MaxTokens int                      `json:"max_tokens"`
	System    string                   `json:"system,omitempty"`
	Messages  []anthropicMessage       `json:"messages"`
	Tools     []anthropicToolDef       `json:"tools,omitempty"`
	Thinking  *AnthropicThinkingConfig `json:"thinking,omitempty"`
	Stream    bool                     `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	Signature string         `json:"signature,omitempty"`
	Data      string         `json:"data,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
}

type anthropicMessagesResponse struct {
	Content []json.RawMessage      `json:"content"`
	Usage   anthropicMessagesUsage `json:"usage"`
}

type anthropicMessagesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func newAnthropicMessagesRequest(model string, maxTokens int, thinking *AnthropicThinkingConfig, request ModelRequest) (anthropicMessagesRequest, error) {
	messages, err := anthropicMessages(request.Messages)
	if err != nil {
		return anthropicMessagesRequest{}, err
	}
	return anthropicMessagesRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    strings.TrimSpace(request.SystemPrompt),
		Messages:  messages,
		Tools:     anthropicTools(request.Tools),
		Thinking:  cloneAnthropicThinkingConfig(thinking),
	}, nil
}

func cloneAnthropicThinkingConfig(config *AnthropicThinkingConfig) *AnthropicThinkingConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	return &cloned
}

func anthropicMessages(messages []Message) ([]anthropicMessage, error) {
	mapped := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case RoleUser:
			mapped = append(mapped, anthropicMessage{Role: "user", Content: message.Content})
		case RoleAssistant:
			content, err := anthropicAssistantContent(message)
			if err != nil {
				return nil, err
			}
			mapped = append(mapped, anthropicMessage{Role: "assistant", Content: content})
		case RoleTool:
			toolResult := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: message.ToolCallID,
				Content:   message.Content,
			}
			if len(mapped) > 0 {
				last := &mapped[len(mapped)-1]
				if last.Role == "user" {
					if blocks, ok := last.Content.([]anthropicContentBlock); ok {
						last.Content = append(blocks, toolResult)
						continue
					}
				}
			}
			mapped = append(mapped, anthropicMessage{
				Role:    "user",
				Content: []anthropicContentBlock{toolResult},
			})
		case RoleSystem:
			if strings.TrimSpace(message.Content) != "" {
				mapped = append(mapped, anthropicMessage{Role: "user", Content: message.Content})
			}
		default:
			return nil, fmt.Errorf("agent: unsupported message role for anthropic messages: %q", message.Role)
		}
	}
	return mapped, nil
}

func anthropicAssistantContent(message Message) (any, error) {
	rawBlocks, ok, err := anthropicMessagesRawContentBlocks(message.Metadata)
	if err != nil {
		return nil, err
	}
	if ok {
		return rawBlocks, nil
	}
	if len(message.ToolCalls) == 0 {
		return message.Content, nil
	}
	blocks := make([]anthropicContentBlock, 0, len(message.ToolCalls)+1)
	if strings.TrimSpace(message.Content) != "" {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: cloneAnyMap(call.Arguments),
		})
	}
	return blocks, nil
}

func anthropicMessagesRawContentBlocks(metadata map[string]any) ([]map[string]any, bool, error) {
	if len(metadata) == 0 {
		return nil, false, nil
	}
	value, ok := metadata[anthropicMessagesContentMetadataKey]
	if !ok {
		return nil, false, nil
	}
	blocks, err := normalizeAnthropicMessagesRawContentBlocks(value)
	if err != nil {
		return nil, true, err
	}
	return blocks, true, nil
}

func normalizeAnthropicMessagesRawContentBlocks(value any) ([]map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("agent: encode anthropic messages content metadata: %w", err)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(data, &blocks); err != nil {
		return nil, fmt.Errorf("agent: decode anthropic messages content metadata: %w", err)
	}
	return blocks, nil
}

func anthropicTools(tools []ToolDescriptor) []anthropicToolDef {
	if len(tools) == 0 {
		return nil
	}
	mapped := make([]anthropicToolDef, 0, len(tools))
	for _, tool := range tools {
		inputSchema := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		if tool.Parameters != nil {
			inputSchema = tool.Parameters.JSONSchema()
		}
		mapped = append(mapped, anthropicToolDef{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: inputSchema,
		})
	}
	return mapped
}

func (r anthropicMessagesResponse) modelResponse() (ModelResponse, error) {
	var textParts []string
	toolCalls := make([]ToolCall, 0)
	rawContent := make([]map[string]any, 0, len(r.Content))
	for _, raw := range r.Content {
		var block anthropicContentBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return ModelResponse{}, fmt.Errorf("agent: decode anthropic messages content block: %w", err)
		}
		var rawBlock map[string]any
		if err := json.Unmarshal(raw, &rawBlock); err != nil {
			return ModelResponse{}, fmt.Errorf("agent: preserve anthropic messages content block: %w", err)
		}
		rawContent = append(rawContent, rawBlock)

		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: cloneAnyMap(block.Input),
			})
		}
	}
	metadata := map[string]any(nil)
	if len(rawContent) > 0 {
		metadata = map[string]any{
			anthropicMessagesContentMetadataKey: rawContent,
		}
	}
	message := Message{
		Role:      RoleAssistant,
		Content:   strings.Join(textParts, ""),
		ToolCalls: core.CloneToolCalls(toolCalls),
		Metadata:  metadata,
	}
	return ModelResponse{Message: message, ToolCalls: toolCalls, Usage: r.Usage.tokenUsage()}, nil
}

func (u anthropicMessagesUsage) tokenUsage() core.TokenUsage {
	totalTokens := u.TotalTokens
	if totalTokens == 0 && (u.InputTokens != 0 || u.OutputTokens != 0) {
		totalTokens = u.InputTokens + u.OutputTokens
	}
	return core.TokenUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  totalTokens,
	}
}

func anthropicMessagesEndpoint(rawBaseURL string) (string, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return "", errors.New("agent: anthropic messages base URL is required")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return "", fmt.Errorf("agent: parse anthropic messages base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("agent: anthropic messages base URL must be absolute")
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, anthropicMessagesPath):
		parsed.Path = path
	case strings.HasSuffix(path, "/v1"):
		parsed.Path = path + "/messages"
	case path == "":
		parsed.Path = anthropicMessagesPath
	default:
		parsed.Path = path + anthropicMessagesPath
	}
	return parsed.String(), nil
}
