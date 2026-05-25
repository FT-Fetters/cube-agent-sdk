package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuiltInProviderCapabilities(t *testing.T) {
	tests := []struct {
		name string
		new  func(t *testing.T) Model
		want ModelCapabilities
	}{
		{
			name: "openai compatible",
			new: func(t *testing.T) Model {
				t.Helper()
				model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
					BaseURL: "https://example.invalid/v1",
					Model:   "chat-test-model",
				})
				if err != nil {
					t.Fatal(err)
				}
				return model
			},
			want: ModelCapabilities{
				Provider:          "openai-compatible",
				APIType:           string(ModelAPIOpenAICompatible),
				Model:             "chat-test-model",
				Tools:             true,
				Streaming:         true,
				ParallelToolCalls: true,
				TokenUsage:        true,
				MCPServerMetadata: false,
				ModelHandledMCP:   false,
				JSONMode:          false,
				StructuredOutput:  false,
				ReasoningMetadata: false,
			},
		},
		{
			name: "openai responses",
			new: func(t *testing.T) Model {
				t.Helper()
				model, err := NewOpenAIResponsesModel(OpenAIResponsesConfig{
					BaseURL: "https://example.invalid",
					Model:   "responses-test-model",
				})
				if err != nil {
					t.Fatal(err)
				}
				return model
			},
			want: ModelCapabilities{
				Provider:          "openai-responses",
				APIType:           string(ModelAPIOpenAIResponses),
				Model:             "responses-test-model",
				Tools:             true,
				Streaming:         true,
				ParallelToolCalls: true,
				TokenUsage:        true,
				ReasoningMetadata: true,
				MCPServerMetadata: false,
				ModelHandledMCP:   false,
				JSONMode:          false,
				StructuredOutput:  false,
			},
		},
		{
			name: "anthropic messages",
			new: func(t *testing.T) Model {
				t.Helper()
				model, err := NewAnthropicMessagesModel(AnthropicMessagesConfig{
					BaseURL: "https://example.invalid",
					Model:   "claude-test-model",
				})
				if err != nil {
					t.Fatal(err)
				}
				return model
			},
			want: ModelCapabilities{
				Provider:          "anthropic-messages",
				APIType:           string(ModelAPIAnthropicMessages),
				Model:             "claude-test-model",
				Tools:             true,
				Streaming:         true,
				ParallelToolCalls: true,
				TokenUsage:        true,
				ReasoningMetadata: true,
				MCPServerMetadata: false,
				ModelHandledMCP:   false,
				JSONMode:          false,
				StructuredOutput:  false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CapabilitiesOf(tt.new(t))
			if !ok {
				t.Fatal("CapabilitiesOf returned ok=false for built-in model")
			}
			if got != tt.want {
				t.Fatalf("capabilities = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCapabilitiesOfCustomModelAndSelection(t *testing.T) {
	unknown := &recordingModel{}
	toolOnly := &capabilityRecordingModel{
		caps: ModelCapabilities{Provider: "tool-only", Tools: true},
	}
	streamingTools := &capabilityRecordingModel{
		caps: ModelCapabilities{Provider: "streaming-tools", Tools: true, Streaming: true},
	}

	if _, ok := CapabilitiesOf(unknown); ok {
		t.Fatal("CapabilitiesOf returned ok=true for model without capabilities")
	}

	caps, ok := CapabilitiesOf(toolOnly)
	if !ok {
		t.Fatal("CapabilitiesOf returned ok=false for custom capability model")
	}
	if caps.Provider != "tool-only" || !caps.Tools || caps.Streaming {
		t.Fatalf("custom capabilities = %#v, want tool-only declaration", caps)
	}

	required := ModelCapabilityRequirement{Tools: true, Streaming: true}
	if caps.Supports(required) {
		t.Fatalf("tool-only capabilities satisfied %#v, want false", required)
	}
	if !streamingTools.caps.Supports(required) {
		t.Fatalf("streaming tool capabilities did not satisfy %#v", required)
	}

	selected, selectedCaps, ok := SelectModelByCapabilities([]Model{unknown, toolOnly, streamingTools}, required)
	if !ok {
		t.Fatal("SelectModelByCapabilities returned ok=false")
	}
	if selected != streamingTools || selectedCaps.Provider != "streaming-tools" {
		t.Fatalf("selected = %T/%#v, want streaming-tools model", selected, selectedCaps)
	}

	if ModelSatisfiesCapabilities(unknown, required) {
		t.Fatal("unknown capabilities satisfied explicit requirement")
	}
	if _, _, ok := SelectModelByCapabilities([]Model{unknown, toolOnly}, required); ok {
		t.Fatal("SelectModelByCapabilities found model without required streaming support")
	}
}

func TestReliableModelPreservesDeclaredCapabilities(t *testing.T) {
	legacy := NewReliableModel(&recordingModel{})
	if _, ok := CapabilitiesOf(legacy); ok {
		t.Fatal("reliable wrapper exposed capabilities for legacy model without declarations")
	}

	base := &capabilityRecordingModel{
		caps: ModelCapabilities{Provider: "custom", Tools: true, TokenUsage: true},
	}
	wrapped := NewReliableModel(base)
	caps, ok := CapabilitiesOf(wrapped)
	if !ok {
		t.Fatal("CapabilitiesOf returned ok=false for reliable capability model")
	}
	if caps.Provider != "custom" || !caps.Tools || !caps.TokenUsage || caps.Streaming {
		t.Fatalf("reliable capabilities = %#v, want copied non-streaming capabilities", caps)
	}

	streamBase := &capabilityStreamModel{
		capabilityRecordingModel: capabilityRecordingModel{
			caps: ModelCapabilities{Provider: "custom-stream", Streaming: true},
		},
	}
	wrappedStream := NewReliableModel(streamBase)
	if _, ok := wrappedStream.(StreamModel); !ok {
		t.Fatalf("wrapped stream model = %T, want StreamModel", wrappedStream)
	}
	streamCaps, ok := CapabilitiesOf(wrappedStream)
	if !ok {
		t.Fatal("CapabilitiesOf returned ok=false for reliable stream capability model")
	}
	if streamCaps.Provider != "custom-stream" || !streamCaps.Streaming {
		t.Fatalf("reliable stream capabilities = %#v, want streaming capability", streamCaps)
	}
}

func TestAgentCapabilityChecksKeepNoCapabilityModelsBackwardCompatible(t *testing.T) {
	model := &recordingModel{responses: []ModelResponse{
		{Message: Message{Role: RoleAssistant, Content: "ok"}},
	}}
	bot, err := New(Config{ID: "compat-agent"}, model,
		WithTools(noopCapabilityTool()),
		WithMCPServers(MCPServerConfig{Name: "filesystem", Transport: MCPTransportStdio}),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, err := bot.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "ok" {
		t.Fatalf("reply = %q, want ok", reply.Content)
	}
	if len(model.requests) != 1 {
		t.Fatalf("model requests = %d, want one", len(model.requests))
	}
	request := model.requests[0]
	if len(request.Tools) != 1 || len(request.MCPServers) != 1 {
		t.Fatalf("request tools/MCP = %d/%d, want 1/1", len(request.Tools), len(request.MCPServers))
	}
}

func TestAgentCapabilityMismatchForTools(t *testing.T) {
	model := &capabilityRecordingModel{
		caps: ModelCapabilities{
			Provider: "custom",
			Tools:    false,
		},
		responses: []ModelResponse{{Message: Message{Role: RoleAssistant, Content: "should not run"}}},
	}
	bot, err := New(Config{ID: "tool-capability-agent"}, model, WithTools(noopCapabilityTool()))
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(context.Background(), "secret prompt should not appear")
	assertCapabilityMismatch(t, err, ModelCapabilityTools)
	if len(model.requests) != 0 {
		t.Fatalf("model requests = %d, want no model call", len(model.requests))
	}
	if strings.Contains(err.Error(), "secret prompt") {
		t.Fatalf("capability mismatch error leaked prompt: %v", err)
	}
}

func TestAgentCapabilityMismatchForMCPServers(t *testing.T) {
	model := &capabilityRecordingModel{
		caps: ModelCapabilities{
			Provider:          "custom",
			MCPServerMetadata: false,
		},
		responses: []ModelResponse{{Message: Message{Role: RoleAssistant, Content: "should not run"}}},
	}
	bot, err := New(Config{ID: "mcp-capability-agent"}, model,
		WithMCPServers(MCPServerConfig{Name: "filesystem", Transport: MCPTransportStdio}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(context.Background(), "hello")
	assertCapabilityMismatch(t, err, ModelCapabilityMCPServerMetadata)
	if len(model.requests) != 0 {
		t.Fatalf("model requests = %d, want no model call", len(model.requests))
	}
}

func TestAgentCapabilityMismatchForStreaming(t *testing.T) {
	model := &capabilityStreamModel{
		capabilityRecordingModel: capabilityRecordingModel{
			caps: ModelCapabilities{
				Provider:  "custom",
				Streaming: false,
			},
		},
	}
	bot, err := New(Config{ID: "stream-capability-agent"}, model)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.RunStream(context.Background(), "hello")
	assertCapabilityMismatch(t, err, ModelCapabilityStreaming)
	if model.streamCalls != 0 {
		t.Fatalf("stream calls = %d, want no stream call", model.streamCalls)
	}
}

func TestAgentStreamingUnsupportedWithoutCapabilitiesStaysCompatible(t *testing.T) {
	bot, err := New(Config{ID: "legacy-stream-agent"}, &recordingModel{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.RunStream(context.Background(), "hello")
	if !errors.Is(err, ErrStreamingUnsupported) {
		t.Fatalf("err = %v, want ErrStreamingUnsupported", err)
	}
	if errors.Is(err, ErrCapabilityMismatch) {
		t.Fatalf("err = %v, did not want ErrCapabilityMismatch", err)
	}
}

func TestModelCapabilitiesDocsCoverBuiltInMatrix(t *testing.T) {
	readme := readDocFile(t, "README.md")
	requireDocContains(t, "README", readme, []string{
		"Provider capability matrix",
		"CapabilitiesOf(model)",
		"ErrCapabilityMismatch",
		"ModelAPIOpenAICompatible",
		"ModelAPIOpenAIResponses",
		"ModelAPIAnthropicMessages",
	})

	englishModels := readDocFile(t, "docs/sdk/en/models.md")
	requireDocContains(t, "English model docs", englishModels, []string{
		"## Provider Capability Matrix",
		"protocol-level adapter support",
		"`MCPServerMetadata`",
		"`ModelHandledMCP`",
		"`CapabilitiesOf(model)`",
		"`SelectModelByCapabilities`",
	})

	chineseModels := readDocFile(t, "docs/sdk/zh/models.md")
	requireDocContains(t, "Chinese model docs", chineseModels, []string{
		"## Provider 能力矩阵",
		"protocol-level adapter support",
		"`MCPServerMetadata`",
		"`ModelHandledMCP`",
		"`CapabilitiesOf(model)`",
		"`SelectModelByCapabilities`",
	})

	englishAPI := readDocFile(t, "docs/sdk/en/api-reference.md")
	requireDocContains(t, "English API reference", englishAPI, []string{
		"`ModelCapabilities`",
		"`ModelCapabilitiesProvider`",
		"`ModelCapabilityRequirement`",
		"`CapabilityMismatchError`",
		"`ErrCapabilityMismatch`",
	})

	chineseAPI := readDocFile(t, "docs/sdk/zh/api-reference.md")
	requireDocContains(t, "Chinese API reference", chineseAPI, []string{
		"`ModelCapabilities`",
		"`ModelCapabilitiesProvider`",
		"`ModelCapabilityRequirement`",
		"`CapabilityMismatchError`",
		"`ErrCapabilityMismatch`",
	})
}

func assertCapabilityMismatch(t *testing.T, err error, want ModelCapability) {
	t.Helper()

	if !errors.Is(err, ErrCapabilityMismatch) {
		t.Fatalf("err = %v, want ErrCapabilityMismatch", err)
	}
	var mismatch *CapabilityMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %T, want *CapabilityMismatchError", err)
	}
	if mismatch.Capability != want {
		t.Fatalf("mismatch capability = %q, want %q", mismatch.Capability, want)
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.Category != ErrorCategoryConfig || agentErr.Operation != "model.capability" {
		t.Fatalf("agent error category/operation = %q/%q, want config/model.capability", agentErr.Category, agentErr.Operation)
	}
}

func noopCapabilityTool() Tool {
	return ToolFunc{
		ToolName:        "lookup",
		ToolDescription: "Lookup data",
		Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			return ToolResult{CallID: call.ID, Name: call.Name, Content: "ok"}, nil
		},
	}
}

type capabilityRecordingModel struct {
	caps      ModelCapabilities
	requests  []ModelRequest
	responses []ModelResponse
}

func (m *capabilityRecordingModel) Capabilities() ModelCapabilities {
	return m.caps
}

func (m *capabilityRecordingModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	m.requests = append(m.requests, request)
	if len(m.responses) == 0 {
		return ModelResponse{Message: Message{Role: RoleAssistant, Content: "ok"}}, nil
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

type capabilityStreamModel struct {
	capabilityRecordingModel
	streamCalls int
}

func (m *capabilityStreamModel) Stream(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
	m.streamCalls++
	events := make(chan StreamEvent, 1)
	events <- StreamEvent{Type: StreamEventDone, Message: Message{Role: RoleAssistant, Content: "ok"}}
	close(events)
	return events, nil
}
