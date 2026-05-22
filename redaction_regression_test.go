package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type redactionRegressionSentinels struct {
	systemPrompt              string
	userMessage               string
	assistantContent          string
	toolArguments             string
	toolResultContent         string
	toolResultMetadataValue   string
	mcpStructuredContentValue string
	rawToolErrorText          string
	rawModelErrorText         string
	rawProviderErrorText      string
	apiKey                    string
	authorization             string
	cookie                    string
	fullProviderURL           string
	mcpEnvValue               string
}

func TestObservationRedactionRegressionOmitsHighRiskPayloads(t *testing.T) {
	sentinels := newRedactionRegressionSentinels()
	var observations []Observation
	observations = append(observations, redactionSuccessObservations(t, sentinels)...)
	observations = append(observations, redactionToolErrorObservations(t, sentinels)...)
	observations = append(observations, redactionModelErrorObservations(t, sentinels)...)
	observations = append(observations, redactionProviderErrorObservations(t, sentinels)...)
	observations = append(observations, redactionMCPObservations(t, sentinels)...)

	if len(observations) == 0 {
		t.Fatal("redaction regression produced no observations")
	}
	for _, observation := range observations {
		assertObservationDoesNotContainAny(t, observation, sentinels.unsafeValues())
	}
}

func TestObservationFromEventRedactionRegressionNormalizesProviderEndpointHost(t *testing.T) {
	sentinels := newRedactionRegressionSentinels()
	observation := ObservationFromEvent(Event{
		Type: EventAfterModel,
		ProviderDiagnostics: ProviderDiagnostics{
			Provider:     "custom-provider",
			HTTPStatus:   http.StatusUnauthorized,
			EndpointHost: sentinels.fullProviderURL,
		},
	})

	if observation.ProviderDiagnostics.EndpointHost != "api.example.test" {
		t.Fatalf("provider endpoint host = %q, want host only", observation.ProviderDiagnostics.EndpointHost)
	}
	assertObservationDoesNotContainAny(t, observation, append(sentinels.unsafeValues(),
		"user:password",
		"/v1/chat/completions",
		"api_key="+sentinels.apiKey,
		"redaction-regression-fragment",
	))
}

func TestSlogObserverRedactionRegressionOmitsHighRiskPayloads(t *testing.T) {
	sentinels := newRedactionRegressionSentinels()
	observation := redactionSyntheticObservation(sentinels)
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: dropSlogTime,
	}))
	observer := NewSlogObserver(SlogObserverOptions{Logger: logger})

	observer.Observe(context.Background(), observation)

	output := buf.String()
	assertStringDoesNotContainAny(t, "slog output", output, sentinels.unsafeValues())
	record := decodeSlogRecord(t, output)
	provider := assertSlogGroup(t, record, "provider_diagnostics")
	assertSlogField(t, provider, "endpoint_host", "api.example.test")
}

func TestMetricsObserverRedactionRegressionOmitsHighRiskLabels(t *testing.T) {
	sentinels := newRedactionRegressionSentinels()
	sink := &recordingMetricSink{}
	observer := NewMetricsObserver(MetricsObserverOptions{Sink: sink})

	observer.Observe(context.Background(), redactionMetricsObservation(sentinels))

	calls := sink.Calls()
	if len(calls) == 0 {
		t.Fatal("metrics observer emitted no calls")
	}
	assertMetricCallsDoNotContainAny(t, calls, sentinels.unsafeValues())
	assertMetricLabelsDoNotUseForbiddenFieldNames(t, calls)
}

func TestBuiltInProviderDiagnosticsRedactionRegressionKeepsOnlySafeMetadata(t *testing.T) {
	sentinels := newRedactionRegressionSentinels()
	tests := []struct {
		name            string
		provider        string
		requestIDHeader string
		newModel        func(string, string, *http.Client) (Model, error)
	}{
		{
			name:            "openai-compatible",
			provider:        "openai-compatible",
			requestIDHeader: "X-Request-Id",
			newModel: func(baseURL string, apiKey string, client *http.Client) (Model, error) {
				return NewOpenAICompatibleModel(OpenAICompatibleConfig{
					BaseURL:    baseURL,
					APIKey:     apiKey,
					Model:      "test-model",
					HTTPClient: client,
				})
			},
		},
		{
			name:            "openai-responses",
			provider:        "openai-responses",
			requestIDHeader: "X-Request-Id",
			newModel: func(baseURL string, apiKey string, client *http.Client) (Model, error) {
				return NewOpenAIResponsesModel(OpenAIResponsesConfig{
					BaseURL:    baseURL,
					APIKey:     apiKey,
					Model:      "test-model",
					HTTPClient: client,
				})
			},
		},
		{
			name:            "anthropic-messages",
			provider:        "anthropic-messages",
			requestIDHeader: "Request-Id",
			newModel: func(baseURL string, apiKey string, client *http.Client) (Model, error) {
				return NewAnthropicMessagesModel(AnthropicMessagesConfig{
					BaseURL:    baseURL,
					APIKey:     apiKey,
					Model:      "claude-test-model",
					HTTPClient: client,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const requestID = "provider-request-redaction"
			var requestedURL string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestedURL = "http://" + r.Host + r.URL.RequestURI()
				w.Header().Set(tt.requestIDHeader, requestID)
				w.Header().Set("Authorization", sentinels.authorization)
				w.Header().Set("Set-Cookie", sentinels.cookie)
				w.Header().Set("Retry-After", "30")
				w.Header().Set("RateLimit-Limit", "1000")
				w.Header().Set("RateLimit-Remaining", "0")
				w.Header().Set("RateLimit-Reset", "60")
				http.Error(w, strings.Join([]string{
					sentinels.rawProviderErrorText,
					sentinels.systemPrompt,
					sentinels.userMessage,
					sentinels.apiKey,
					sentinels.authorization,
					sentinels.cookie,
				}, " "), http.StatusTooManyRequests)
			}))
			defer server.Close()

			model, err := tt.newModel(server.URL+"?api_key="+sentinels.apiKey, sentinels.apiKey, server.Client())
			if err != nil {
				t.Fatal(err)
			}

			_, err = model.Generate(context.Background(), ModelRequest{
				SystemPrompt: sentinels.systemPrompt,
				Messages:     []Message{{Role: RoleUser, Content: sentinels.userMessage}},
			})
			if err == nil {
				t.Fatal("Generate returned nil error, want provider error")
			}

			got, ok := ProviderDiagnosticsFromError(err)
			if !ok {
				t.Fatal("ProviderDiagnosticsFromError returned ok=false, want diagnostics")
			}
			want := ProviderDiagnostics{
				Provider:           tt.provider,
				HTTPStatus:         http.StatusTooManyRequests,
				EndpointHost:       server.Listener.Addr().String(),
				RequestID:          requestID,
				RetryAfter:         "30",
				RateLimitLimit:     "1000",
				RateLimitRemaining: "0",
				RateLimitReset:     "60",
			}
			if got != want {
				t.Fatalf("provider diagnostics = %#v, want %#v", got, want)
			}
			unsafeValues := append(sentinels.unsafeValues(), requestedURL, "api_key="+sentinels.apiKey, "Authorization", "Cookie", "Set-Cookie")
			assertStringDoesNotContainAny(t, "provider diagnostics", fmt.Sprintf("%#v", got), unsafeValues)
			assertStringDoesNotContainAny(t, "provider error", err.Error(), unsafeValues)
		})
	}
}

func TestForbiddenTelemetryFieldNamesCoverRedactionRegressionClasses(t *testing.T) {
	want := []string{
		"prompts",
		"message_content",
		"tool_arguments",
		"tool_result_content",
		"tool_result_metadata_values",
		"raw_errors",
		"credentials",
		"full_provider_urls",
		"mcp_environment_values",
		"tool_scope_values",
		"tool_business_reasons",
	}
	got := map[string]struct{}{}
	for _, name := range ForbiddenTelemetryFieldNames() {
		got[name] = struct{}{}
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Fatalf("ForbiddenTelemetryFieldNames missing redaction class %q", name)
		}
	}
}

func newRedactionRegressionSentinels() redactionRegressionSentinels {
	return redactionRegressionSentinels{
		systemPrompt:              "redaction-regression-system-prompt",
		userMessage:               "redaction-regression-user-message",
		assistantContent:          "redaction-regression-assistant-content",
		toolArguments:             "redaction-regression-tool-arguments",
		toolResultContent:         "redaction-regression-tool-result-content",
		toolResultMetadataValue:   "redaction-regression-tool-result-metadata-value",
		mcpStructuredContentValue: "redaction-regression-mcp-structured-content-value",
		rawToolErrorText:          "redaction-regression-raw-tool-error",
		rawModelErrorText:         "redaction-regression-raw-model-error",
		rawProviderErrorText:      "redaction-regression-raw-provider-error",
		apiKey:                    "redaction-regression-api-key",
		authorization:             "Bearer redaction-regression-authorization",
		cookie:                    "session=redaction-regression-cookie",
		fullProviderURL:           "https://user:password@api.example.test/v1/chat/completions?api_key=redaction-regression-api-key#redaction-regression-fragment",
		mcpEnvValue:               "redaction-regression-mcp-env-value",
	}
}

func (s redactionRegressionSentinels) unsafeValues() []string {
	return []string{
		s.systemPrompt,
		s.userMessage,
		s.assistantContent,
		s.toolArguments,
		s.toolResultContent,
		s.toolResultMetadataValue,
		s.mcpStructuredContentValue,
		s.rawToolErrorText,
		s.rawModelErrorText,
		s.rawProviderErrorText,
		s.apiKey,
		s.authorization,
		s.cookie,
		s.fullProviderURL,
		s.mcpEnvValue,
	}
}

func redactionSuccessObservations(t *testing.T, sentinels redactionRegressionSentinels) []Observation {
	t.Helper()
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{redactionToolCall(sentinels)}},
		{Message: Message{Role: RoleAssistant, Content: sentinels.assistantContent}},
	}}
	bot, err := New(Config{ID: "redaction-regression-agent", SystemPrompt: sentinels.systemPrompt}, model,
		WithObserver(recorder),
		WithTools(redactionTool(sentinels, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := bot.Run(context.Background(), sentinels.userMessage)
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != sentinels.assistantContent {
		t.Fatalf("response content = %q, want scripted assistant content", response.Content)
	}
	return recorder.Observations()
}

func redactionToolErrorObservations(t *testing.T, sentinels redactionRegressionSentinels) []Observation {
	t.Helper()
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{redactionToolCall(sentinels)}},
	}}
	bot, err := New(Config{ID: "redaction-regression-tool-error-agent", SystemPrompt: sentinels.systemPrompt}, model,
		WithObserver(recorder),
		WithTools(redactionTool(sentinels, errors.New(sentinels.rawToolErrorText))),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(context.Background(), sentinels.userMessage); err == nil {
		t.Fatal("Run error = nil, want raw tool error")
	}
	return recorder.Observations()
}

func redactionModelErrorObservations(t *testing.T, sentinels redactionRegressionSentinels) []Observation {
	t.Helper()
	recorder := &recordingObserver{}
	bot, err := New(Config{ID: "redaction-regression-model-error-agent", SystemPrompt: sentinels.systemPrompt},
		failingModel{err: errors.New(sentinels.rawModelErrorText)},
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(context.Background(), sentinels.userMessage); err == nil {
		t.Fatal("Run error = nil, want raw model error")
	}
	return recorder.Observations()
}

func redactionProviderErrorObservations(t *testing.T, sentinels redactionRegressionSentinels) []Observation {
	t.Helper()
	recorder := &recordingObserver{}
	cause := errors.New(sentinels.rawProviderErrorText)
	modelErr := NewProviderError("provider rejected request", ProviderDiagnostics{
		Provider:     "custom-provider",
		HTTPStatus:   http.StatusUnauthorized,
		EndpointHost: sentinels.fullProviderURL,
		RequestID:    "provider-request-redaction",
	}, cause)
	bot, err := New(Config{ID: "redaction-regression-provider-error-agent", SystemPrompt: sentinels.systemPrompt},
		failingModel{err: modelErr},
		WithObserver(recorder),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(context.Background(), sentinels.userMessage); err == nil {
		t.Fatal("Run error = nil, want provider error")
	}
	afterModel := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	if afterModel.ProviderDiagnostics.EndpointHost != "api.example.test" {
		t.Fatalf("provider endpoint host = %q, want host only", afterModel.ProviderDiagnostics.EndpointHost)
	}
	return recorder.Observations()
}

func redactionMCPObservations(t *testing.T, sentinels redactionRegressionSentinels) []Observation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := fakeMCPServerConfig("ok")
	config.Env["CUBE_AGENT_MCP_ENV_SECRET"] = sentinels.mcpEnvValue
	config.Env["CUBE_AGENT_FAKE_MCP_STRUCTURED_SECRET"] = sentinels.mcpStructuredContentValue
	client, err := StartMCPStdioClient(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingObserver{}
	model := &recordingModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{
			ID:        "mcp-call-1",
			Name:      "echo",
			Arguments: map[string]any{"text": sentinels.toolArguments},
		}}},
		{Message: Message{Role: RoleAssistant, Content: sentinels.assistantContent}},
	}}
	bot, err := New(Config{ID: "redaction-regression-mcp-agent", SystemPrompt: sentinels.systemPrompt}, model,
		WithObserver(recorder),
		WithTools(tools...),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := bot.Run(ctx, sentinels.userMessage); err != nil {
		t.Fatal(err)
	}
	return recorder.Observations()
}

func redactionToolCall(sentinels redactionRegressionSentinels) ToolCall {
	return ToolCall{
		ID:   "call-redaction",
		Name: "redaction_lookup",
		Arguments: map[string]any{
			"query":         sentinels.toolArguments,
			"api_key":       sentinels.apiKey,
			"authorization": sentinels.authorization,
			"cookie":        sentinels.cookie,
			"url":           sentinels.fullProviderURL,
			"mcp_env":       sentinels.mcpEnvValue,
		},
	}
}

func redactionTool(sentinels redactionRegressionSentinels, err error) ToolFunc {
	return ToolFunc{
		ToolName:        "redaction_lookup",
		ToolDescription: "Lookup data for redaction regression testing",
		ToolRisk:        ToolRiskRead,
		Fn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			return ToolResult{
				CallID:  call.ID,
				Name:    call.Name,
				Content: sentinels.toolResultContent,
				Metadata: map[string]any{
					"safeMetadataKey":      sentinels.toolResultMetadataValue,
					"mcpStructuredContent": map[string]any{"safeKey": sentinels.mcpStructuredContentValue},
					"mcpIsError":           false,
				},
			}, err
		},
	}
}

func redactionSyntheticObservation(sentinels redactionRegressionSentinels) Observation {
	return ObservationFromEvent(Event{
		Type:            EventAfterTool,
		AgentID:         "agent-redaction",
		RunID:           "run-redaction",
		RequestID:       "request-redaction",
		ParentRequestID: "parent-redaction",
		ToolName:        "redaction_lookup",
		ToolRisk:        ToolRiskRead,
		ProviderDiagnostics: ProviderDiagnostics{
			Provider:     "custom-provider",
			HTTPStatus:   http.StatusTooManyRequests,
			EndpointHost: sentinels.fullProviderURL,
			RequestID:    "provider-request-redaction",
		},
		Message: Message{
			Role:    RoleAssistant,
			Content: sentinels.assistantContent,
		},
		ToolCall: redactionToolCall(sentinels),
		ToolResult: ToolResult{
			Name:    "redaction_lookup",
			Content: sentinels.toolResultContent,
			Metadata: map[string]any{
				"safeMetadataKey":      sentinels.toolResultMetadataValue,
				"mcpStructuredContent": map[string]any{"safeKey": sentinels.mcpStructuredContentValue},
				"mcpIsError":           true,
			},
		},
		Error: errors.New(strings.Join([]string{
			sentinels.rawToolErrorText,
			sentinels.rawModelErrorText,
			sentinels.rawProviderErrorText,
		}, " ")),
	})
}

func redactionMetricsObservation(sentinels redactionRegressionSentinels) Observation {
	observation := redactionSyntheticObservation(sentinels)
	observation.AgentID = sentinels.systemPrompt
	observation.RunID = sentinels.userMessage
	observation.SubagentID = sentinels.assistantContent
	observation.TraceID = sentinels.authorization
	observation.SpanID = sentinels.cookie
	observation.TraceState = sentinels.apiKey
	observation.RequestID = sentinels.fullProviderURL
	observation.ParentRequestID = sentinels.mcpEnvValue
	observation.ToolSchemaHash = sentinels.toolArguments
	observation.SkillName = sentinels.toolResultMetadataValue
	observation.ApprovalReason = sentinels.rawToolErrorText
	observation.ProviderDiagnostics.EndpointHost = sentinels.fullProviderURL
	observation.ProviderDiagnostics.RequestID = sentinels.rawProviderErrorText
	observation.ProviderDiagnostics.RetryAfter = sentinels.rawModelErrorText
	return observation
}

func assertObservationDoesNotContainAny(t *testing.T, observation Observation, unsafeValues []string) {
	t.Helper()
	for _, unsafe := range unsafeValues {
		assertObservationDoesNotContain(t, observation, unsafe)
	}
}

func assertStringDoesNotContainAny(t *testing.T, name string, text string, unsafeValues []string) {
	t.Helper()
	for _, unsafe := range unsafeValues {
		if unsafe == "" {
			continue
		}
		if strings.Contains(text, unsafe) {
			t.Fatalf("%s leaked unsafe value %q in %s", name, unsafe, text)
		}
	}
}

func assertMetricCallsDoNotContainAny(t *testing.T, calls []metricCall, unsafeValues []string) {
	t.Helper()
	var text strings.Builder
	for _, call := range calls {
		text.WriteString(call.kind)
		text.WriteByte(' ')
		text.WriteString(call.name)
		for _, label := range call.labels {
			text.WriteByte(' ')
			text.WriteString(label.Name)
			text.WriteByte('=')
			text.WriteString(label.Value)
		}
	}
	assertStringDoesNotContainAny(t, "metric labels", text.String(), unsafeValues)
}

func assertMetricLabelsDoNotUseForbiddenFieldNames(t *testing.T, calls []metricCall) {
	t.Helper()
	forbidden := map[string]struct{}{}
	for _, name := range ForbiddenTelemetryFieldNames() {
		forbidden[name] = struct{}{}
	}
	for _, call := range calls {
		for _, label := range call.labels {
			if _, ok := forbidden[label.Name]; ok {
				t.Fatalf("metric label used forbidden telemetry field name %q", label.Name)
			}
		}
	}
}
