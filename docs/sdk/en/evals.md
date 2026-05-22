# Evals and Replay

Eval helpers are public, dependency-free building blocks for Go tests. They let
you script model behavior, capture a stable transcript, replay saved artifacts,
and assert agent behavior without a live provider.

## Scripted Models

Use `ScriptedModel` for deterministic `Run` tests.

```go
recorder := agent.NewEvalRecorder()
model := agent.NewScriptedModel(
	agent.ScriptedResponse(agent.ModelResponse{
		ToolCalls: []agent.ToolCall{{
			ID:        "call-1",
			Name:      "lookup_account",
			Arguments: map[string]any{"account_id": "acct_123"},
		}},
	}),
	agent.ScriptedResponse(agent.ModelResponse{
		Message: agent.Message{Role: agent.RoleAssistant, Content: "active"},
	}),
).RecordWith(recorder)

bot, err := agent.New(agent.Config{ID: "eval-agent"}, model,
	agent.WithHook(recorder.Hook()),
	agent.WithObserver(recorder),
	agent.WithTools(lookupTool),
)
if err != nil {
	t.Fatal(err)
}

reply, runErr := bot.Run(ctx, "check account")
transcript := recorder.RecordRun("check account", reply, runErr)

agent.AssertToolCall(t, transcript, agent.ToolCallExpectation{
	Name:      "lookup_account",
	Arguments: map[string]any{"account_id": "acct_123"},
})
agent.AssertFinalMessage(t, transcript, "active")
```

`ScriptedModel.Requests()` returns isolated copies of the model requests. If the
script is exhausted, the model returns a deterministic error instead of silently
inventing a response.

Use `ScriptedError` for model failure paths:

```go
model := agent.NewScriptedModel(
	agent.ScriptedError(errors.New("fixture model unavailable")),
).RecordWith(recorder)

reply, runErr := bot.Run(ctx, "hello")
transcript := recorder.RecordRun("hello", reply, runErr)

agent.AssertFinalError(t, transcript, "fixture model unavailable")
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type:          agent.EventAfterModel,
	Failed:        true,
	ErrorCategory: agent.ErrorCategoryModel,
})
```

## Streaming Failures

Use `ScriptedStreamModel` for `RunStream` tests. Stream scripts can emit deltas,
done events, stream errors, or startup errors.

```go
model := agent.NewScriptedStreamModel(agent.ScriptedStreamEvents(
	agent.StreamEvent{Type: agent.StreamEventDelta, Delta: "partial"},
	agent.StreamEvent{Type: agent.StreamEventError, Error: errors.New("stream failed")},
)).RecordWith(recorder)

events, err := bot.RunStream(ctx, "stream", agent.WithStreamObservations())
if err != nil {
	t.Fatal(err)
}

var streamErr error
for event := range events {
	if event.Type == agent.StreamEventError {
		streamErr = event.Error
	}
}
transcript := recorder.RecordRun("stream", agent.Message{}, streamErr)

agent.AssertFinalError(t, transcript, "stream failed")
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type:          agent.EventStreamError,
	Failed:        true,
	ErrorCategory: agent.ErrorCategoryModel,
})
```

## Golden Transcripts

`EvalRecorder` records lifecycle hooks, sanitized observations, model exchanges
from scripted models, stream exchanges, inputs, tool calls/results, and final
outcomes.

```go
payload, err := transcript.StableJSON()
if err != nil {
	t.Fatal(err)
}

replayed, err := agent.ReplayRunTranscript(payload)
if err != nil {
	t.Fatal(err)
}
agent.AssertEventOrder(t, replayed, agent.EventBeforeModel, agent.EventAfterModel)
```

`StableJSON` omits nondeterministic timing fields from transcript views so the
output is suitable for CI and golden-file comparison.

## Common Eval Use Cases

Tool selection:

```go
agent.AssertToolCalled(t, transcript, "lookup_account")
agent.AssertToolCall(t, transcript, agent.ToolCallExpectation{
	Name:          "lookup_account",
	ResultContent: "active",
})
```

Approval refusal:

```go
bot, err := agent.New(cfg, model,
	agent.WithApprovalPolicy(agent.DenyAllApproval{}),
	agent.WithTools(deleteTool),
	agent.WithHook(recorder.Hook()),
	agent.WithObserver(recorder),
)

reply, runErr := bot.Run(ctx, "delete account")
transcript := recorder.RecordRun("delete account", reply, runErr)

agent.AssertApprovalDenied(t, transcript, "delete_account")
agent.AssertFinalError(t, transcript, "approval denied")
```

Context compaction:

```go
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type: agent.EventBeforeCompact,
})
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type: agent.EventAfterCompact,
})
```

Model errors and streaming failures should assert both the final error and the
failed observation category so tests cover user-visible behavior and telemetry.

## Session Event Replay

Use `ReplaySessionEvents` to load saved append-only `SessionEvent` logs into a
transcript and assert ordering.

```go
events, err := store.ListSessionEvents(ctx, "session-123", 0)
if err != nil {
	t.Fatal(err)
}

transcript, err := agent.ReplaySessionEvents(events)
if err != nil {
	t.Fatal(err)
}
agent.AssertSessionEventOrder(t, transcript,
	agent.SessionEventRunStarted,
	agent.SessionEventRunCompleted,
)
```

Replay validates event schema metadata and monotonic order. Session event
metadata is represented as sorted key/value pairs for stable JSON.

## Storage and Redaction

Eval transcripts intentionally contain the scripted inputs, model messages, tool
arguments, tool results, and error strings your test chooses to record. Store
them like test fixtures that may contain user content. Applications control
redaction, encryption, access control, and retention.

Transcript model request views omit MCP command paths, arguments, environment
variables, URLs, and provider credentials. They keep only safe request metadata
such as MCP server name and transport.
