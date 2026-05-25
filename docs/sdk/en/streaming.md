# Streaming

Streaming models implement `StreamModel` in addition to `Model`. The built-in
OpenAI-compatible chat completions, OpenAI Responses, and Anthropic Messages
adapters support `RunStream` with provider-native streaming. Use `RunStream`
when callers need incremental assistant text or provider thinking/reasoning text.

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
	case agent.StreamEventThinkingDelta:
		fmt.Printf("thinking: %s", event.Delta)
	case agent.StreamEventToolCallStart:
		fmt.Printf("tool starting: %s\n", event.ToolCall.Name)
	case agent.StreamEventToolCallDone:
		fmt.Printf("tool ready: %s\n", event.ToolCall.Name)
	case agent.StreamEventDone:
		fmt.Println(event.Message.Content)
		_ = event.Usage  // Final provider token usage when the stream reports it.
		_ = event.Finish // Final safe finish metadata when the stream reports it.
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
- `StreamEventThinkingDelta`: incremental provider thinking or reasoning text. It is delivered to the caller in real time and is not appended to the final assistant message content.
- `StreamEventToolCallStart`: safe tool-call boundary metadata such as tool call ID, name, and provider stream index. It does not include tool arguments.
- `StreamEventToolCallDone`: safe tool-call boundary metadata emitted when the streamed tool call is complete enough for the provider adapter to reconstruct it. It does not include tool arguments.
- `StreamEventDone`: final assistant message, provider token usage, and safe finish metadata when available.
- `StreamEventError`: stream failure.

The SDK commits the final assistant message only after a done event is
forwarded to the caller. Interrupted delta streams and canceled abandoned streams
do not persist partial or undelivered assistant text. Thinking deltas are caller
visible only; provider-specific reasoning metadata may still be preserved on the
final done message for continuation when the adapter supports it.

Final streaming `EventAfterModel` events and observations include total stream
duration through `Duration`. When a model reports usage on the done event, the
same token counts are copied to `TokenUsage`. Final done events may also carry
`Finish.Reason`, such as a provider stop or tool-call finish reason. When at
least one delta is received, lifecycle telemetry includes sanitized
`StreamTelemetry` with time to first assistant-text token, assistant-text delta
count, streamed assistant-text byte count, and throughput. Stream telemetry never
contains streamed text, thinking text, or tool arguments.

Use `WithStreamObservations()` on a `RunStream` call when you need observer-only
stream lifecycle telemetry for start, first delta, done, and error. The option
does not emit per-delta observations beyond the first delta.

## Tool Calls

`RunStream` executes tool calls that arrive on final done messages. Built-in
OpenAI-compatible, OpenAI Responses, and Anthropic Messages adapters normalize
supported streamed tool-call shapes into those done messages and may also emit
safe `StreamEventToolCallStart` and `StreamEventToolCallDone` boundaries for UI
state. Tool-call arguments remain on `event.Message.ToolCalls` in the final done
event so the agent can execute tools; boundary events intentionally carry only
safe metadata.

Existing callers that switch only on delta, done, and error can keep doing so.
They should ignore unknown stream event types to remain forward compatible with
additional metadata events.

If a provider rejects the initial streaming HTTP request, `RunStream` returns a
structured provider error immediately. If the provider stream starts and then
emits an error or invalid event, callers receive `StreamEventError` with safe
provider diagnostics when available.
