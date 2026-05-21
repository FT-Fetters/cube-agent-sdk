# Streaming

Streaming models implement `StreamModel` in addition to `Model`. Use
`RunStream` when callers need incremental assistant text.

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
	case agent.StreamEventError:
		return event.Error
	}
}
```

## Event Types

- `StreamEventDelta`: incremental assistant text.
- `StreamEventDone`: final assistant message.
- `StreamEventError`: stream failure.

The SDK commits the final assistant message only after a done event. Interrupted
delta streams do not persist partial assistant text.

Final streaming `EventAfterModel` events and observations include total stream
duration through `Duration`. When at least one delta is received, they also
include sanitized `StreamTelemetry` with time to first token, delta count,
streamed delta byte count, and throughput. Stream telemetry never contains the
streamed text.

## Current Limitations

Streamed tool calls are not executed yet. If a streaming model emits tool calls,
the SDK reports `ErrStreamingToolCallsUnsupported`.
