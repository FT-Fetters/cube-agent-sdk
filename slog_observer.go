package agent

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

const defaultSlogObserverMessage = "agent observation"

// SlogObserverOptions configures a standard-library slog observer.
type SlogObserverOptions struct {
	// Logger receives structured observation records. If nil, slog.Default is used.
	Logger *slog.Logger
	// Level is the slog level used for every observation. The zero value is info.
	Level slog.Level
	// Message is the slog record message. If empty, "agent observation" is used.
	Message string
}

// SlogObserver logs sanitized lifecycle telemetry with Go's standard log/slog
// package. It emits event and failed for every observation, and omits other
// zero-value attributes.
type SlogObserver struct {
	logger  *slog.Logger
	level   slog.Level
	message string
}

// NewSlogObserver returns an Observer backed by Go's standard log/slog package.
// The observer only logs fields present on Observation and does not log prompts,
// message content, tool arguments, tool results, raw errors, API keys, full
// URLs, or MCP environment values.
func NewSlogObserver(options SlogObserverOptions) SlogObserver {
	message := options.Message
	if message == "" {
		message = defaultSlogObserverMessage
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return SlogObserver{
		logger:  logger,
		level:   options.Level,
		message: message,
	}
}

func (o SlogObserver) Observe(ctx context.Context, observation Observation) {
	logger := o.logger
	if logger == nil {
		logger = slog.Default()
	}
	message := o.message
	if message == "" {
		message = defaultSlogObserverMessage
	}
	logger.LogAttrs(ctx, o.level, message, slogObservationAttrs(observation)...)
}

func slogObservationAttrs(observation Observation) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event", string(observation.Type)),
		slog.Bool("failed", observation.Failed),
	}
	attrs = appendSlogString(attrs, "agent_id", observation.AgentID)
	attrs = appendSlogString(attrs, "run_id", observation.RunID)
	attrs = appendSlogString(attrs, "subagent_id", observation.SubagentID)
	attrs = appendSlogString(attrs, "trace_id", observation.TraceID)
	attrs = appendSlogString(attrs, "span_id", observation.SpanID)
	attrs = appendSlogString(attrs, "trace_state", observation.TraceState)
	attrs = appendSlogString(attrs, "request_id", observation.RequestID)
	attrs = appendSlogString(attrs, "parent_request_id", observation.ParentRequestID)
	if observation.Round > 0 {
		attrs = append(attrs, slog.Int("round", observation.Round))
	}
	if observation.Duration > 0 {
		durationMS := float64(observation.Duration) / float64(time.Millisecond)
		attrs = append(attrs, slog.Float64("duration_ms", durationMS))
	}
	if observation.EstimatedTokens > 0 {
		attrs = append(attrs, slog.Int("estimated_tokens", observation.EstimatedTokens))
	}
	if tokenAttrs := tokenUsageSlogAttrs(observation.TokenUsage); tokenAttrs != nil {
		attrs = append(attrs, slogGroupAttr("token_usage", tokenAttrs))
	}
	if toolAttrs := toolSlogAttrs(observation); toolAttrs != nil {
		attrs = append(attrs, slogGroupAttr("tool", toolAttrs))
	}
	attrs = appendSlogString(attrs, "skill_name", observation.SkillName)
	if approvalAttrs := approvalSlogAttrs(observation); approvalAttrs != nil {
		attrs = append(attrs, slogGroupAttr("approval", approvalAttrs))
	}
	attrs = appendSlogString(attrs, "error_category", string(observation.ErrorCategory))
	attrs = appendSlogString(attrs, "model_error_subcategory", string(observation.ModelErrorSubcategory))
	if providerAttrs := providerDiagnosticsSlogAttrs(observation.ProviderDiagnostics); providerAttrs != nil {
		attrs = append(attrs, slogGroupAttr("provider_diagnostics", providerAttrs))
	}
	return attrs
}

func slogGroupAttr(key string, attrs []slog.Attr) slog.Attr {
	return slog.Attr{Key: key, Value: slog.GroupValue(attrs...)}
}

func appendSlogString(attrs []slog.Attr, key string, value string) []slog.Attr {
	if value == "" {
		return attrs
	}
	return append(attrs, slog.String(key, value))
}

func tokenUsageSlogAttrs(usage TokenUsage) []slog.Attr {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return []slog.Attr{
		slog.Int("input_tokens", usage.InputTokens),
		slog.Int("output_tokens", usage.OutputTokens),
		slog.Int("total_tokens", usage.TotalTokens),
	}
}

func toolSlogAttrs(observation Observation) []slog.Attr {
	var attrs []slog.Attr
	attrs = appendSlogString(attrs, "name", observation.ToolName)
	attrs = appendSlogString(attrs, "risk", string(observation.ToolRisk))
	return attrs
}

func approvalSlogAttrs(observation Observation) []slog.Attr {
	if observation.Type != EventBeforeApproval && observation.Type != EventAfterApproval && !observation.Approved && observation.ApprovalReason == "" {
		return nil
	}
	attrs := []slog.Attr{slog.Bool("approved", observation.Approved)}
	attrs = appendSlogString(attrs, "reason", observation.ApprovalReason)
	return attrs
}

func providerDiagnosticsSlogAttrs(diagnostics ProviderDiagnostics) []slog.Attr {
	if diagnostics.IsZero() {
		return nil
	}
	var attrs []slog.Attr
	attrs = appendSlogString(attrs, "provider", diagnostics.Provider)
	if diagnostics.HTTPStatus > 0 {
		attrs = append(attrs, slog.Int("http_status", diagnostics.HTTPStatus))
	}
	attrs = appendSlogString(attrs, "endpoint_host", safeSlogEndpointHost(diagnostics.EndpointHost))
	attrs = appendSlogString(attrs, "request_id", diagnostics.RequestID)
	attrs = appendSlogString(attrs, "retry_after", diagnostics.RetryAfter)
	attrs = appendSlogString(attrs, "rate_limit_limit", diagnostics.RateLimitLimit)
	attrs = appendSlogString(attrs, "rate_limit_remaining", diagnostics.RateLimitRemaining)
	attrs = appendSlogString(attrs, "rate_limit_reset", diagnostics.RateLimitReset)
	return attrs
}

func safeSlogEndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		return stripSlogUserInfo(parsed.Host)
	}
	host := endpoint
	for _, separator := range []string{"/", "?", "#"} {
		if index := strings.Index(host, separator); index >= 0 {
			host = host[:index]
		}
	}
	return stripSlogUserInfo(strings.TrimSpace(host))
}

func stripSlogUserInfo(host string) string {
	if index := strings.LastIndex(host, "@"); index >= 0 {
		return host[index+1:]
	}
	return host
}
