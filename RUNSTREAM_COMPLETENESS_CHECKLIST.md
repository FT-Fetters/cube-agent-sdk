# RunStream Completeness Checklist

> Status values: `[ ]` pending, `[~]` in progress, `[x]` accepted and committed.
> Each implementation item must be completed with tests, independent review, main-agent verification, and a focused commit before the next item starts.

## Checklist

- [x] **1. Streamed tool-call execution loop**
  - Add full `RunStream` support for streamed tool calls instead of returning `ErrStreamingToolCallsUnsupported`.
  - Acceptance criteria:
    - A stream can emit tool calls, the SDK executes the requested tools, appends assistant/tool messages, and continues streaming the follow-up model round.
    - Tool validation, approval, execution, max-round handling, request IDs, parent request IDs, hooks, and observer metadata match the non-streaming `Run` path.
    - Partial assistant text before a tool call is not exposed as a committed final assistant message unless it is part of the tool-call assistant turn.
    - Existing text-only stream behavior, interrupted-stream behavior, unsupported-stream errors, and provider diagnostics remain compatible.
    - Tests cover successful streamed tool call continuation, tool errors or approval denial, and max tool round exhaustion.

- [ ] **2. Stream cancellation and drain semantics**
  - Clarify and harden caller cancellation behavior for returned stream channels.
  - Acceptance criteria:
    - Documentation states callers must drain the stream or cancel the context.
    - Tests cover cancellation while forwarding events and verify no final assistant message is committed after cancellation.
    - Provider stream contexts are canceled when the returned stream is abandoned via context cancellation.

- [ ] **3. Same-agent concurrent run semantics**
  - Define safe behavior for overlapping `Run` and `RunStream` calls on the same agent.
  - Acceptance criteria:
    - Documentation clearly recommends one active run per agent unless callers isolate state with forked/session agents.
    - If serialization is added, tests cover overlapping runs and deterministic context ordering.
    - Existing single-run behavior is unchanged.

- [ ] **4. Provider streamed tool-call normalization**
  - Normalize built-in provider adapters so streamed tool-call signals become SDK tool-call events instead of provider-specific unsupported errors.
  - Acceptance criteria:
    - OpenAI-compatible stream chunks can accumulate streamed function-call IDs, names, and JSON arguments.
    - OpenAI Responses streamed function-call output can be mapped to SDK tool calls when the provider emits it.
    - Anthropic Messages streamed `tool_use` content blocks can be mapped to SDK tool calls, including partial JSON input.
    - Invalid streamed tool-call arguments produce safe decode errors without leaking raw provider payloads.

- [ ] **5. Richer stream event model**
  - Extend stream events for UI and observability without exposing unsafe payloads by default.
  - Acceptance criteria:
    - The public API can represent tool-call start/done boundaries or equivalent metadata.
    - Token usage and finish metadata remain available on final done events.
    - Stream lifecycle observations stay sanitized and do not emit per-token text by default.
    - Backward compatibility is preserved for callers that only handle delta/done/error.

## Implementation Notes

- Follow TDD for behavior changes: write a focused failing test, confirm the failure, then implement the minimum code needed.
- Keep provider and agent-runtime changes separate where possible.
- Do not revert unrelated workspace changes.
- Prefer standard-library Go and existing SDK patterns.
