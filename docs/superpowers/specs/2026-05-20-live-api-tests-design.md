# Live API Tests Design

## Goal

Add opt-in live provider coverage for the SDK without weakening the current
fast, deterministic test suite. Live tests should run automatically when a local
root `.env` provides a complete model configuration, and should skip otherwise.

## Confirmed Behavior

- The root `.env` file is the local source of live provider settings.
- `.env` remains ignored by git and must never be committed.
- Live tests run when all required variables are available:
  - `MODEL_API_TYPE`
  - `MODEL_BASE_URL`
  - `MODEL_API_KEY`
  - `MODEL_NAME`
- If any required variable is missing, live tests call `t.Skip` with the missing
  variable names.
- No extra `LIVE_API_TESTS=1` gate is required.
- Existing fake and `httptest` tests stay as the normal deterministic coverage.

## Architecture

Add a small test-only helper in the root package. The helper will:

- Locate the repository root from the test working directory.
- Parse root `.env` with a minimal standard-library parser.
- Set parsed variables only when the process environment does not already define
  them, so explicit shell-provided values take precedence.
- Build `ModelConfig` from the existing `MODEL_*` variables.
- Return a skip reason when the live configuration is incomplete.

The parser should support the practical `.env` shape needed for credentials:

- Blank lines and `#` comments.
- `KEY=value` pairs.
- Optional surrounding single or double quotes.
- No variable expansion or advanced shell syntax.

This keeps the project dependency-light and avoids treating `.env` as executable
shell code.

## Live Test Shape

Add focused live tests for real model execution through the public SDK surface:

- Construct a model with `NewModel`.
- Construct an agent with a short system prompt.
- Run a short, low-cost prompt.
- Require a non-empty assistant response.
- Capture safe observer metadata when useful.

Tests must avoid printing or asserting secrets. Any output should include only
safe values such as API type, model name, response text, event type, duration,
round, token estimate, request ID, and error category.

## Output and Targeted Runs

Use Go's existing test runner instead of adding a custom runner:

- Run all tests with output: `go test -v ./...`
- Run one test with output: `go test -v -run '^TestName$' ./...`
- Run one package test with output: `go test -v -run '^TestName$' .`

The live tests should use `t.Logf` for their visible output. Go only shows those
logs for passing tests when `-v` is set, which matches the requirement to show
the corresponding output for targeted runs.

## Error Handling

- Incomplete config skips rather than fails.
- Invalid `.env` lines fail the live test helper with a clear message because a
  malformed local credential file would otherwise be hard to diagnose.
- Provider/API errors fail the live test and include the safe SDK error metadata
  already exposed by `AgentError`.
- Context timeouts should be bounded to keep live test failures finite.

## Documentation

Update developer-facing docs with:

- The supported `.env` keys.
- A minimal `.env` example with placeholder credentials.
- Commands for running all tests, running only live tests, and running a single
  named test with verbose output.

## Out of Scope

- Recording or replaying live responses.
- Supporting multiple live providers in one `.env`.
- Adding third-party dotenv dependencies.
- Sending prompts that require tool use or high token budgets.
- Changing the deterministic fake-provider unit tests into live tests.
