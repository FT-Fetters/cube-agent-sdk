package agent

import (
	"context"

	"github.com/cubence/cube-agent-sdk/internal/core"
)

// TraceContext carries caller-provided trace correlation metadata. Values should
// be non-sensitive identifiers because the SDK surfaces them in lifecycle
// events, observations, and structured errors.
type TraceContext = core.TraceContext

// WithTraceContext attaches caller-provided trace correlation metadata to ctx.
// The SDK surfaces only this typed metadata; it does not inspect arbitrary
// context values.
func WithTraceContext(ctx context.Context, trace TraceContext) context.Context {
	return core.WithTraceContext(ctx, trace)
}

// TraceContextFromContext returns trace correlation metadata previously attached
// with WithTraceContext.
func TraceContextFromContext(ctx context.Context) (TraceContext, bool) {
	return core.TraceContextFromContext(ctx)
}
