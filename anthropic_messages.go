package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	anthropicMessagesPath     = "/v1/messages"
	defaultAnthropicVersion   = "2023-06-01"
	defaultAnthropicMaxTokens = 4096
)

// AnthropicMessagesConfig configures an Anthropic Messages API endpoint.
type AnthropicMessagesConfig struct {
	BaseURL          string
	APIKey           string
	Model            string
	HTTPClient       *http.Client
	MaxTokens        int
	AnthropicVersion string
}

// AnthropicMessagesModel adapts the Anthropic Messages API to the SDK Model interface.
type AnthropicMessagesModel struct {
	endpoint         string
	apiKey           string
	model            string
	httpClient       *http.Client
	maxTokens        int
	anthropicVersion string
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
	}, nil
}

// Generate sends one Anthropic Messages request and maps text/tool_use content
// blocks into the SDK's assistant message and tool-call structures.
func (m *AnthropicMessagesModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m == nil {
		return ModelResponse{}, errors.New("agent: anthropic messages model is nil")
	}
	payload, err := newAnthropicMessagesRequest(m.model, m.maxTokens, request)
	if err != nil {
		return ModelResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ModelResponse{}, fmt.Errorf("agent: encode anthropic messages request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, fmt.Errorf("agent: create anthropic messages request: %w", err)
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
		return ModelResponse{}, fmt.Errorf("agent: call anthropic messages: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		summary := redactedErrorSummary(httpResponse.Body, m.apiKey)
		return ModelResponse{}, fmt.Errorf("agent: anthropic messages returned status %d: %s", httpResponse.StatusCode, summary)
	}

	var decoded anthropicMessagesResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		return ModelResponse{}, fmt.Errorf("agent: decode anthropic messages response: %w", err)
	}
	return decoded.modelResponse()
}

type anthropicMessagesRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicToolDef `json:"tools,omitempty"`
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
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
}

type anthropicMessagesResponse struct {
	Content []anthropicContentBlock `json:"content"`
}

func newAnthropicMessagesRequest(model string, maxTokens int, request ModelRequest) (anthropicMessagesRequest, error) {
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
	}, nil
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
			mapped = append(mapped, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: message.ToolCallID,
					Content:   message.Content,
				}},
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
	for _, block := range r.Content {
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
	message := Message{
		Role:      RoleAssistant,
		Content:   strings.Join(textParts, ""),
		ToolCalls: cloneToolCalls(toolCalls),
	}
	return ModelResponse{Message: message, ToolCalls: toolCalls}, nil
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

func redactedErrorSummary(body io.Reader, apiKey string) string {
	limited, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return "failed to read error response body"
	}
	summary := strings.TrimSpace(string(limited))
	if summary == "" {
		return "empty response body"
	}
	if apiKey != "" {
		summary = strings.ReplaceAll(summary, "Bearer "+apiKey, "Bearer [redacted]")
		summary = strings.ReplaceAll(summary, apiKey, "[redacted]")
	}
	if len(summary) > 512 {
		summary = summary[:512] + "..."
	}
	return summary
}
