# Examples

Local examples live under the repository `examples/` directory and avoid real
credentials or external services unless stated otherwise.

## Local Examples

```bash
go run ./examples/openai_compatible
go run ./examples/model_factory
go run ./examples/tool_schema
go run ./examples/streaming
go run ./examples/mcp_stdio
go run ./examples/session_state
go run ./examples/approval_observer
```

The test suite compiles the examples:

```bash
go test ./...
```

## Live API Example

The live API example is intended for real provider endpoints and reads
credentials from environment variables.

```bash
MODEL_API_TYPE=anthropic-messages \
MODEL_BASE_URL=https://api.anthropic.com \
MODEL_API_KEY="<your-api-key>" \
MODEL_NAME=claude-sonnet-4-6 \
go run ./examples/live_api
```

Use `MODEL_API_TYPE=openai-compatible` with an OpenAI-compatible base URL, or
`MODEL_API_TYPE=openai-responses` with `MODEL_BASE_URL=https://api.openai.com`.

## Optional Live API Test

When `MODEL_API_TYPE`, `MODEL_BASE_URL`, `MODEL_API_KEY`, and `MODEL_NAME` are
present in the process environment or a root `.env` file, the live test runs
automatically. When any required variable is missing, it is skipped.

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

Do not commit real credentials. Keep local `.env` files out of version control.
