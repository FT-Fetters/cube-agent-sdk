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

const (
	openAIResponsesPath              = "/responses"
	openAIResponsesDefaultPath       = "/v1/responses"
	openAIResponsesOutputMetadataKey = "openai_responses_output"
)

// OpenAIResponsesConfig configures OpenAI's Responses API endpoint.
type OpenAIResponsesConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	MaxTokens  int
	Store      *bool
}

// OpenAIResponsesModel adapts OpenAI's Responses API to the SDK Model interface.
type OpenAIResponsesModel struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
	maxTokens  int
	store      *bool
}

// NewOpenAIResponsesModel creates a Model from Responses API configuration.
// BaseURL may be a root URL, /v1 URL, or full /v1/responses URL.
func NewOpenAIResponsesModel(config OpenAIResponsesConfig) (*OpenAIResponsesModel, error) {
	endpoint, err := openAIResponsesEndpoint(config.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("agent: openai responses model is required")
	}
	var store *bool
	if config.Store != nil {
		value := *config.Store
		store = &value
	}
	return &OpenAIResponsesModel{
		endpoint:   endpoint,
		apiKey:     config.APIKey,
		model:      model,
		httpClient: config.HTTPClient,
		maxTokens:  config.MaxTokens,
		store:      store,
	}, nil
}

// Generate sends one Responses API request and maps text/function_call output
// items into the SDK's assistant message and tool-call structures.
func (m *OpenAIResponsesModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m == nil {
		return ModelResponse{}, errors.New("agent: openai responses model is nil")
	}
	diagnostics := providerdiagnostics.New(providerOpenAIResponses, m.endpoint)
	payload, err := newOpenAIResponsesRequest(m.model, m.maxTokens, m.store, request)
	if err != nil {
		return ModelResponse{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ModelResponse{}, core.NewProviderError("encode openai responses request", diagnostics, err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return ModelResponse{}, core.NewProviderError("create openai responses request", diagnostics, err)
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
		return ModelResponse{}, core.NewProviderTransportError("call openai responses", diagnostics, err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		diagnostics := providerdiagnostics.FromResponse(providerOpenAIResponses, m.endpoint, httpResponse)
		message := fmt.Sprintf("openai responses returned status %d", httpResponse.StatusCode)
		sensitiveValues := providerdiagnostics.SensitiveValuesFromModelRequest(request, m.apiKey, m.endpoint)
		if summary := providerdiagnostics.ErrorSummaryFromResponse(httpResponse, sensitiveValues...); summary != "" {
			message += ": " + summary
		}
		return ModelResponse{}, core.NewProviderError(message, diagnostics, nil)
	}

	var decoded openAIResponsesResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		return ModelResponse{}, core.NewProviderDecodeError("decode openai responses response", diagnostics, err)
	}
	return decoded.modelResponse()
}

type openAIResponsesRequest struct {
	Model           string                `json:"model"`
	Instructions    string                `json:"instructions,omitempty"`
	Input           []any                 `json:"input"`
	Tools           []openAIResponsesTool `json:"tools,omitempty"`
	MaxOutputTokens int                   `json:"max_output_tokens,omitempty"`
	Store           *bool                 `json:"store,omitempty"`
	Stream          bool                  `json:"stream,omitempty"`
}

type openAIResponsesInputMessage struct {
	Type    string                        `json:"type"`
	Role    string                        `json:"role"`
	Content []openAIResponsesContentBlock `json:"content"`
}

type openAIResponsesContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIResponsesFunctionCallInput struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponsesFunctionOutputInput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type openAIResponsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIResponsesResponse struct {
	Output     []json.RawMessage    `json:"output"`
	OutputText string               `json:"output_text,omitempty"`
	Status     string               `json:"status,omitempty"`
	Usage      openAIResponsesUsage `json:"usage"`
}

type openAIResponsesOutputItem struct {
	ID        string                              `json:"id,omitempty"`
	Type      string                              `json:"type"`
	Role      string                              `json:"role,omitempty"`
	Content   []openAIResponsesOutputContentBlock `json:"content,omitempty"`
	CallID    string                              `json:"call_id,omitempty"`
	Name      string                              `json:"name,omitempty"`
	Arguments string                              `json:"arguments,omitempty"`
}

type openAIResponsesOutputContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type openAIResponsesUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func newOpenAIResponsesRequest(model string, maxTokens int, store *bool, request ModelRequest) (openAIResponsesRequest, error) {
	input, err := openAIResponsesInput(request.Messages)
	if err != nil {
		return openAIResponsesRequest{}, err
	}
	payload := openAIResponsesRequest{
		Model:        model,
		Instructions: strings.TrimSpace(request.SystemPrompt),
		Input:        input,
		Tools:        openAIResponsesTools(request.Tools),
	}
	if maxTokens > 0 {
		payload.MaxOutputTokens = maxTokens
	}
	if store != nil {
		value := *store
		payload.Store = &value
	}
	return payload, nil
}

func openAIResponsesInput(messages []Message) ([]any, error) {
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == RoleAssistant {
			rawItems, ok, err := openAIResponsesRawOutputItems(message.Metadata)
			if err != nil {
				return nil, err
			}
			if ok {
				for _, item := range rawItems {
					input = append(input, item)
				}
				continue
			}
		}

		switch message.Role {
		case RoleTool:
			input = append(input, openAIResponsesFunctionOutputInput{
				Type:   "function_call_output",
				CallID: message.ToolCallID,
				Output: message.Content,
			})
		case RoleAssistant:
			if message.Content != "" {
				input = append(input, openAIResponsesMessage("assistant", "output_text", message.Content))
			}
			for _, call := range message.ToolCalls {
				mapped, err := openAIResponsesFunctionCallInputFromToolCall(call)
				if err != nil {
					return nil, err
				}
				input = append(input, mapped)
			}
		default:
			role := string(message.Role)
			if role == "" {
				role = "user"
			}
			input = append(input, openAIResponsesMessage(role, "input_text", message.Content))
		}
	}
	return input, nil
}

func openAIResponsesMessage(role string, contentType string, content string) openAIResponsesInputMessage {
	return openAIResponsesInputMessage{
		Type: "message",
		Role: role,
		Content: []openAIResponsesContentBlock{{
			Type: contentType,
			Text: content,
		}},
	}
}

func openAIResponsesFunctionCallInputFromToolCall(call ToolCall) (openAIResponsesFunctionCallInput, error) {
	arguments, err := openAIEncodeToolCallArguments(call.Arguments)
	if err != nil {
		return openAIResponsesFunctionCallInput{}, fmt.Errorf("agent: encode tool call arguments for %s: %w", openAIToolCallLabel(call.Name, call.ID), err)
	}
	return openAIResponsesFunctionCallInput{
		Type:      "function_call",
		CallID:    call.ID,
		Name:      call.Name,
		Arguments: arguments,
	}, nil
}

func openAIResponsesTools(tools []ToolDescriptor) []openAIResponsesTool {
	if len(tools) == 0 {
		return nil
	}
	mapped := make([]openAIResponsesTool, 0, len(tools))
	for _, tool := range tools {
		parameters := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		if tool.Parameters != nil {
			parameters = tool.Parameters.JSONSchema()
		}
		mapped = append(mapped, openAIResponsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
		})
	}
	return mapped
}

func openAIResponsesRawOutputItems(metadata map[string]any) ([]map[string]any, bool, error) {
	if len(metadata) == 0 {
		return nil, false, nil
	}
	value, ok := metadata[openAIResponsesOutputMetadataKey]
	if !ok {
		return nil, false, nil
	}
	items, err := normalizeOpenAIResponsesRawOutputItems(value)
	if err != nil {
		return nil, true, err
	}
	return items, true, nil
}

func normalizeOpenAIResponsesRawOutputItems(value any) ([]map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("agent: encode openai responses output metadata: %w", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("agent: decode openai responses output metadata: %w", err)
	}
	return items, nil
}

func (r openAIResponsesResponse) modelResponse() (ModelResponse, error) {
	if len(r.Output) == 0 && strings.TrimSpace(r.OutputText) == "" {
		return ModelResponse{}, errors.New("agent: openai responses response has no output")
	}

	var textParts []string
	toolCalls := make([]ToolCall, 0)
	rawOutput := make([]map[string]any, 0, len(r.Output))
	for _, raw := range r.Output {
		var item openAIResponsesOutputItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return ModelResponse{}, fmt.Errorf("agent: decode openai responses output item: %w", err)
		}
		var rawItem map[string]any
		if err := json.Unmarshal(raw, &rawItem); err != nil {
			return ModelResponse{}, fmt.Errorf("agent: preserve openai responses output item: %w", err)
		}
		rawOutput = append(rawOutput, rawItem)

		switch item.Type {
		case "message":
			for _, block := range item.Content {
				if block.Type == "output_text" && block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			}
		case "function_call":
			arguments, err := openAIParseToolCallArguments(item.Arguments, item.Name, item.CallID)
			if err != nil {
				return ModelResponse{}, err
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: arguments,
			})
		}
	}
	if len(textParts) == 0 && r.OutputText != "" {
		textParts = append(textParts, r.OutputText)
	}

	metadata := map[string]any(nil)
	if len(rawOutput) > 0 {
		metadata = map[string]any{
			openAIResponsesOutputMetadataKey: rawOutput,
		}
	}
	assistantMessage := Message{
		Role:      RoleAssistant,
		Content:   strings.Join(textParts, ""),
		ToolCalls: core.CloneToolCalls(toolCalls),
		Metadata:  metadata,
	}
	return ModelResponse{
		Message:   assistantMessage,
		ToolCalls: toolCalls,
		Usage:     r.Usage.tokenUsage(),
	}, nil
}

func (u openAIResponsesUsage) tokenUsage() core.TokenUsage {
	inputTokens := u.InputTokens
	if inputTokens == 0 {
		inputTokens = u.PromptTokens
	}
	outputTokens := u.OutputTokens
	if outputTokens == 0 {
		outputTokens = u.CompletionTokens
	}
	return core.TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  u.TotalTokens,
	}
}

func openAIResponsesEndpoint(rawBaseURL string) (string, error) {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return "", errors.New("agent: openai responses base URL is required")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return "", fmt.Errorf("agent: parse openai responses base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("agent: openai responses base URL must be absolute")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, openAIResponsesPath):
		parsed.Path = path
	case strings.HasSuffix(path, "/v1"):
		parsed.Path = path + openAIResponsesPath
	case path == "":
		parsed.Path = openAIResponsesDefaultPath
	default:
		parsed.Path = path + openAIResponsesPath
	}
	return parsed.String(), nil
}
