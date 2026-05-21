# Observability Optimization Todo

> Status rules: `[ ]` pending, `[~]` in progress, `[x]` implemented, reviewed, and committed.
> Each implementation item is handled by one implementation subagent and one independent review subagent.
> After review approval, mark the item complete and create a local commit. Do not push.

## Process

- Implement one checklist item at a time.
- Use a dedicated implementation subagent for the item.
- Use a separate review subagent for acceptance.
- If review fails, send the review findings back to the implementation subagent for repair.
- Repeat review and repair until the item is accepted.
- Run deterministic verification before each completion claim and commit.
- Keep telemetry safe by default; never expose prompts, tool arguments, tool results, credentials, raw provider errors, full URLs with query strings, or MCP environment values.

## Checklist

- [x] **1. Add run-level correlation IDs.**
  Add a `RunID` concept so all events emitted during one `Run` or `RunStream` can be correlated across model calls, tool calls, approvals, compaction, skill activation, and subagent messages.

- [x] **2. Add parent correlation for nested events.**
  Add a parent request or run correlation field to observations so a model call, its tool calls, and follow-up model calls can be reconstructed in logs and traces.

- [x] **3. Support trace metadata from context.**
  Allow applications to put external trace metadata in `context.Context` and have the SDK surface safe correlation metadata in observations without depending on a tracing library.

- [x] **4. Add model usage to responses.**
  Extend `ModelResponse` with usage fields for input, output, and total tokens while preserving estimated tokens as a fallback.

- [x] **5. Parse provider token usage.**
  Parse token usage from OpenAI-compatible chat completions, OpenAI Responses, and Anthropic Messages provider responses.

- [x] **6. Surface real token usage in observations.**
  Expose real token usage on observations separately from `EstimatedTokens`, using estimate values only when provider usage is unavailable.

- [x] **7. Add safe provider diagnostics to model errors.**
  Include safe provider diagnostics such as provider name, HTTP status, endpoint host, and provider request ID without exposing sensitive request or response content.

- [ ] **8. Capture common diagnostic response headers.**
  Capture safe response headers such as request IDs, rate-limit hints, and retry-after values for provider diagnostics.

- [ ] **9. Standardize provider error classification.**
  Add model error subcategories such as timeout, rate limited, authentication, server error, bad request, and decode error for logs and alert grouping.

- [ ] **10. Add a standard-library slog observer.**
  Provide a `log/slog` observer that emits structured, sanitized lifecycle telemetry with no third-party dependencies.

- [ ] **11. Add a lightweight metrics observer interface or example.**
  Provide a metrics integration path for counters and latency histograms without forcing a Prometheus dependency.

- [ ] **12. Add observer fan-out composition.**
  Add a way to compose multiple observers while isolating observer panics independently.

- [ ] **13. Add sampling support for observations.**
  Provide configurable observation sampling by event type, failure status, or sampling ratio.

- [ ] **14. Add streaming first-token and throughput telemetry.**
  Track time-to-first-token, stream duration, and optional throughput metadata for streaming runs.

- [ ] **15. Add optional streaming lifecycle observations.**
  Emit optional stream start, first delta, done, and error observations without emitting every delta by default.

- [ ] **16. Align streaming errors with non-streaming observability.**
  Ensure streaming errors consistently include run correlation, request IDs, duration, usage where available, and error category.

- [ ] **17. Add tool schema version or hash telemetry.**
  Add a safe schema version or hash for tool observations so schema drift can be diagnosed without exposing arguments.

- [ ] **18. Add safe tool result metadata.**
  Add safe result metadata such as result size, metadata keys, and MCP error status without exposing result content.

- [ ] **19. Split tool lifecycle timing.**
  Make validation, approval, and execution timing easy to distinguish in observations.

- [ ] **20. Add OpenTelemetry integration example.**
  Document or provide an example mapping observations to spans, events, and attributes without making OpenTelemetry a core dependency.

- [ ] **21. Define telemetry attribute naming conventions.**
  Document stable attribute names for logs, metrics, and traces, such as `agent.id`, `agent.run_id`, `agent.request_id`, and `agent.error.category`.

- [ ] **22. Allow custom request ID generation.**
  Let applications override the default request ID generator for deterministic tests and cross-service correlation.

- [ ] **23. Add production observability documentation.**
  Add production guidance for logs, metrics, tracing, safe redaction, and recommended dashboards or alerts.

- [ ] **24. Add redaction regression coverage.**
  Add tests covering provider errors, response headers, MCP environment values, tool arguments, stream errors, and raw errors to prevent telemetry leaks.

- [ ] **25. Add observation contract tests.**
  Add tests that lock down event ordering and field semantics so future changes do not break integrations.

## Implementation Log

- Baseline note: `go test ./...` currently fails when live-provider environment variables are configured because the opt-in live API test reaches a provider error. Deterministic verification should unset live-provider variables unless a task specifically targets live-provider behavior.
