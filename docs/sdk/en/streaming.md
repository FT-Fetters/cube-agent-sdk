# Streaming

Streaming models implement `StreamModel` in addition to `Model`. The built-in
OpenAI-compatible chat completions, OpenAI Responses, and Anthropic Messages
adapters support `RunStream` with provider-native streaming. Use `RunStream`
when callers need incremental assistant text.

```go
events, err := bot.RunStream(ctx, "Write a short summary.")
if err != nil {
	if errors.Is(err, agent.ErrStreamingUnsupported) {
		// Fall back to Run or use a streaming-capable adapter.
	}
	return err
}

for event := range events {
	switch event.Type {
	case agent.StreamEventDelta:
		fmt.Print(event.Delta)
	case agent.StreamEventDone:
		fmt.Println(event.Message.Content)
		_ = event.Usage // Final provider token usage when the stream reports it.
	case agent.StreamEventError:
		return event.Error
	}
}
```

Callers must either drain the returned channel until it closes or cancel the
context passed to `RunStream` when they stop reading early. Canceling the
context releases the provider stream and closes the returned channel; abandoning
the channel without cancellation can leave forwarding blocked.

## Event Types

- `StreamEventDelta`: incremental assistant text.
- `StreamEventDone`: final assistant message and provider token usage when available.
- `StreamEventError`: stream failure.

The SDK commits the final assistant message only after a done event is
forwarded to the caller. Interrupted delta streams and canceled abandoned streams
do not persist partial or undelivered assistant text.

Final streaming `EventAfterModel` events and observations include total stream
duration through `Duration`. When a model reports usage on the done event, the
same token counts are copied to `TokenUsage`. When at least one delta is
received, they also include sanitized `StreamTelemetry` with time to first token,
delta count, streamed delta byte count, and throughput. Stream telemetry never
contains the streamed text.

Use `WithStreamObservations()` on a `RunStream` call when you need observer-only
stream lifecycle telemetry for start, first delta, done, and error. The option
does not emit per-delta observations beyond the first delta.

## Current Limitations

Streamed tool calls are not executed yet. If a streaming model emits tool calls,
the SDK reports `ErrStreamingToolCallsUnsupported`.

If a provider rejects the initial streaming HTTP request, `RunStream` returns a
structured provider error immediately. If the provider stream starts and then
emits an error or invalid event, callers receive `StreamEventError` with safe
provider diagnostics when available.
