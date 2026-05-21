package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuiltInProviderNon2xxErrorsCarrySafeDiagnostics(t *testing.T) {
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
			const requestID = "provider-request-123"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(tt.requestIDHeader, requestID)
				http.Error(w, `provider rejected secret-prompt test-key query-secret`, http.StatusUnauthorized)
			}))
			defer server.Close()

			model, err := tt.newModel(server.URL+"?api_key=query-secret", "test-key", server.Client())
			if err != nil {
				t.Fatal(err)
			}

			_, err = model.Generate(context.Background(), ModelRequest{
				SystemPrompt: "secret-prompt",
				Messages:     []Message{{Role: RoleUser, Content: "secret-prompt"}},
			})
			if err == nil {
				t.Fatal("Generate returned nil error, want provider error")
			}

			var providerErr *ProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("err = %T, want *ProviderError", err)
			}
			want := ProviderDiagnostics{
				Provider:     tt.provider,
				HTTPStatus:   http.StatusUnauthorized,
				EndpointHost: server.Listener.Addr().String(),
				RequestID:    requestID,
			}
			if providerErr.Diagnostics != want {
				t.Fatalf("provider diagnostics = %#v, want %#v", providerErr.Diagnostics, want)
			}
			got, ok := ProviderDiagnosticsFromError(err)
			if !ok || got != want {
				t.Fatalf("ProviderDiagnosticsFromError = %#v/%t, want %#v/true", got, ok, want)
			}
			assertProviderErrorStringIsSafe(t, err, "provider rejected", "secret-prompt", "test-key", "query-secret", "api_key=query-secret")
		})
	}
}

func TestBuiltInProviderNon2xxErrorsCarryDiagnosticResponseHeaders(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		setHeaders func(http.Header)
		want       ProviderDiagnostics
		newModel   func(string, string, *http.Client) (Model, error)
	}{
		{
			name:     "openai-compatible legacy rate limit headers",
			provider: "openai-compatible",
			setHeaders: func(header http.Header) {
				header.Set("Retry-After", "30")
				header.Set("X-RateLimit-Limit", "1000")
				header.Set("X-RateLimit-Remaining", "42")
				header.Set("X-RateLimit-Reset", "1710000000")
			},
			want: ProviderDiagnostics{
				RetryAfter:         "30",
				RateLimitLimit:     "1000",
				RateLimitRemaining: "42",
				RateLimitReset:     "1710000000",
			},
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
			name:     "openai-responses standard rate limit headers",
			provider: "openai-responses",
			setHeaders: func(header http.Header) {
				header.Set("Retry-After", "Wed, 21 Oct 2015 07:28:00 GMT")
				header.Set("RateLimit-Limit", "500")
				header.Set("RateLimit-Remaining", "0")
				header.Set("RateLimit-Reset", "60")
			},
			want: ProviderDiagnostics{
				RetryAfter:         "Wed, 21 Oct 2015 07:28:00 GMT",
				RateLimitLimit:     "500",
				RateLimitRemaining: "0",
				RateLimitReset:     "60",
			},
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
			name:     "anthropic-messages standard rate limit headers",
			provider: "anthropic-messages",
			setHeaders: func(header http.Header) {
				header.Set("Retry-After", "15")
				header.Set("RateLimit-Limit", "200")
				header.Set("RateLimit-Remaining", "3")
				header.Set("RateLimit-Reset", "45")
			},
			want: ProviderDiagnostics{
				RetryAfter:         "15",
				RateLimitLimit:     "200",
				RateLimitRemaining: "3",
				RateLimitReset:     "45",
			},
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.setHeaders(w.Header())
				w.Header().Set("Authorization", "Bearer response-secret")
				w.Header().Set("Set-Cookie", "session=response-secret")
				http.Error(w, `provider rejected secret-prompt test-key query-secret`, http.StatusTooManyRequests)
			}))
			defer server.Close()

			model, err := tt.newModel(server.URL+"?api_key=query-secret", "test-key", server.Client())
			if err != nil {
				t.Fatal(err)
			}

			_, err = model.Generate(context.Background(), ModelRequest{
				SystemPrompt: "secret-prompt",
				Messages:     []Message{{Role: RoleUser, Content: "secret-prompt"}},
			})
			if err == nil {
				t.Fatal("Generate returned nil error, want provider error")
			}

			got, ok := ProviderDiagnosticsFromError(err)
			if !ok {
				t.Fatalf("ProviderDiagnosticsFromError returned ok=false, want diagnostics")
			}
			want := tt.want
			want.Provider = tt.provider
			want.HTTPStatus = http.StatusTooManyRequests
			want.EndpointHost = server.Listener.Addr().String()
			if got != want {
				t.Fatalf("provider diagnostics = %#v, want %#v", got, want)
			}
			assertProviderErrorStringIsSafe(t, err, "provider rejected", "secret-prompt", "test-key", "query-secret", "response-secret", "api_key=query-secret")
			if diagnosticsText := fmt.Sprintf("%#v", got); strings.Contains(diagnosticsText, "response-secret") {
				t.Fatalf("provider diagnostics exposed unsafe response header value: %#v", got)
			}
		})
	}
}

func TestProviderDiagnosticsNormalizesDiagnosticResponseHeaders(t *testing.T) {
	err := NewProviderError("provider error", ProviderDiagnostics{
		Provider:           " test-provider ",
		HTTPStatus:         http.StatusTooManyRequests,
		EndpointHost:       " example.test ",
		RequestID:          " request-1 ",
		RetryAfter:         " 30 ",
		RateLimitLimit:     " 1000 ",
		RateLimitRemaining: " 42 ",
		RateLimitReset:     " 1710000000 ",
	}, nil)

	got, ok := ProviderDiagnosticsFromError(err)
	if !ok {
		t.Fatalf("ProviderDiagnosticsFromError returned ok=false, want diagnostics")
	}
	want := ProviderDiagnostics{
		Provider:           "test-provider",
		HTTPStatus:         http.StatusTooManyRequests,
		EndpointHost:       "example.test",
		RequestID:          "request-1",
		RetryAfter:         "30",
		RateLimitLimit:     "1000",
		RateLimitRemaining: "42",
		RateLimitReset:     "1710000000",
	}
	if got != want {
		t.Fatalf("ProviderDiagnosticsFromError = %#v, want %#v", got, want)
	}
}

func TestBuiltInProviderTransportErrorsCarrySafeDiagnostics(t *testing.T) {
	transportErr := errors.New("transport failed with transport-secret")
	tests := []struct {
		name     string
		provider string
		newModel func(*http.Client) (Model, error)
	}{
		{
			name:     "openai-compatible",
			provider: "openai-compatible",
			newModel: func(client *http.Client) (Model, error) {
				return NewOpenAICompatibleModel(OpenAICompatibleConfig{
					BaseURL:    "https://transport.example.test/v1?api_key=query-secret",
					APIKey:     "test-key",
					Model:      "test-model",
					HTTPClient: client,
				})
			},
		},
		{
			name:     "openai-responses",
			provider: "openai-responses",
			newModel: func(client *http.Client) (Model, error) {
				return NewOpenAIResponsesModel(OpenAIResponsesConfig{
					BaseURL:    "https://transport.example.test/v1?api_key=query-secret",
					APIKey:     "test-key",
					Model:      "test-model",
					HTTPClient: client,
				})
			},
		},
		{
			name:     "anthropic-messages",
			provider: "anthropic-messages",
			newModel: func(client *http.Client) (Model, error) {
				return NewAnthropicMessagesModel(AnthropicMessagesConfig{
					BaseURL:    "https://transport.example.test/v1?api_key=query-secret",
					APIKey:     "test-key",
					Model:      "claude-test-model",
					HTTPClient: client,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, transportErr
			})}
			model, err := tt.newModel(client)
			if err != nil {
				t.Fatal(err)
			}

			_, err = model.Generate(context.Background(), ModelRequest{})
			if !errors.Is(err, transportErr) {
				t.Fatalf("err = %v, want transport cause compatibility", err)
			}
			want := ProviderDiagnostics{
				Provider:     tt.provider,
				EndpointHost: "transport.example.test",
			}
			got, ok := ProviderDiagnosticsFromError(err)
			if !ok || got != want {
				t.Fatalf("ProviderDiagnosticsFromError = %#v/%t, want %#v/true", got, ok, want)
			}
			assertProviderErrorStringIsSafe(t, err, "transport-secret", "test-key", "query-secret", "api_key=query-secret", "https://transport.example.test")
		})
	}
}

func TestBuiltInProviderDecodeErrorsCarrySafeDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		newModel func(string, *http.Client) (Model, error)
	}{
		{
			name:     "openai-compatible",
			provider: "openai-compatible",
			newModel: func(baseURL string, client *http.Client) (Model, error) {
				return NewOpenAICompatibleModel(OpenAICompatibleConfig{
					BaseURL:    baseURL,
					Model:      "test-model",
					HTTPClient: client,
				})
			},
		},
		{
			name:     "openai-responses",
			provider: "openai-responses",
			newModel: func(baseURL string, client *http.Client) (Model, error) {
				return NewOpenAIResponsesModel(OpenAIResponsesConfig{
					BaseURL:    baseURL,
					Model:      "test-model",
					HTTPClient: client,
				})
			},
		},
		{
			name:     "anthropic-messages",
			provider: "anthropic-messages",
			newModel: func(baseURL string, client *http.Client) (Model, error) {
				return NewAnthropicMessagesModel(AnthropicMessagesConfig{
					BaseURL:    baseURL,
					Model:      "claude-test-model",
					HTTPClient: client,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{not-json secret-prompt}`)
			}))
			defer server.Close()

			model, err := tt.newModel(server.URL+"?api_key=query-secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}

			_, err = model.Generate(context.Background(), ModelRequest{})
			var syntaxErr *json.SyntaxError
			if !errors.As(err, &syntaxErr) {
				t.Fatalf("err = %v, want JSON syntax error compatibility", err)
			}
			want := ProviderDiagnostics{
				Provider:     tt.provider,
				EndpointHost: server.Listener.Addr().String(),
			}
			got, ok := ProviderDiagnosticsFromError(err)
			if !ok || got != want {
				t.Fatalf("ProviderDiagnosticsFromError = %#v/%t, want %#v/true", got, ok, want)
			}
			assertProviderErrorStringIsSafe(t, err, "secret-prompt", "query-secret", "api_key=query-secret")
		})
	}
}

func TestAgentModelErrorsExposeProviderDiagnosticsToErrorsEventsAndObservations(t *testing.T) {
	const requestID = "provider-request-456"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", requestID)
		http.Error(w, "provider rejected secret-prompt test-key query-secret", http.StatusTooManyRequests)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleModel(OpenAICompatibleConfig{
		BaseURL:    server.URL + "?api_key=query-secret",
		APIKey:     "test-key",
		Model:      "test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	recorder := &recordingObserver{}
	var events []Event
	bot, err := New(Config{ID: "provider-diagnostics-agent"}, model,
		WithObserver(recorder),
		WithHook(func(ctx context.Context, event Event) error {
			events = append(events, event)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = bot.Run(context.Background(), "secret-prompt")
	if err == nil {
		t.Fatal("Run returned nil error, want provider error")
	}
	want := ProviderDiagnostics{
		Provider:     "openai-compatible",
		HTTPStatus:   http.StatusTooManyRequests,
		EndpointHost: server.Listener.Addr().String(),
		RequestID:    requestID,
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("err = %T, want *AgentError", err)
	}
	if agentErr.ProviderDiagnostics != want {
		t.Fatalf("agent error provider diagnostics = %#v, want %#v", agentErr.ProviderDiagnostics, want)
	}
	afterModel := firstEventOfType(t, events, EventAfterModel)
	if afterModel.ProviderDiagnostics != want {
		t.Fatalf("after model provider diagnostics = %#v, want %#v", afterModel.ProviderDiagnostics, want)
	}
	afterObservation := firstObservationOfType(t, recorder.Observations(), EventAfterModel)
	if afterObservation.ProviderDiagnostics != want {
		t.Fatalf("after model observation provider diagnostics = %#v, want %#v", afterObservation.ProviderDiagnostics, want)
	}
	assertProviderErrorStringIsSafe(t, err, "provider rejected", "secret-prompt", "test-key", "query-secret", "api_key=query-secret")
	assertObservationDoesNotContain(t, afterObservation, "secret-prompt")
	assertObservationDoesNotContain(t, afterObservation, "test-key")
	assertObservationDoesNotContain(t, afterObservation, "query-secret")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func assertProviderErrorStringIsSafe(t *testing.T, err error, unsafeValues ...string) {
	t.Helper()
	text := err.Error()
	for _, unsafe := range unsafeValues {
		if strings.Contains(text, unsafe) {
			t.Fatalf("error string = %q, want no unsafe value %q", text, unsafe)
		}
	}
}
