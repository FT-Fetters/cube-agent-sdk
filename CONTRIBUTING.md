# Contributing

Thanks for improving Cube Agent SDK. This project is intentionally small and
keeps provider credentials, external services, persistent storage, and telemetry
exporters outside the core runtime.

## Development Setup

Prerequisites:

- Go 1.22 or newer.
- No real API keys or live model providers are required for the deterministic
  test suite. The process environment or a local root `.env` can enable optional
  live API tests.

Run the local quality gate before sending changes:

```bash
go test ./...
go test -race ./...
go vet ./...
go test -count=1 ./...
```

Optional live API tests:

Provide a complete model configuration in the process environment or a root
`.env` file to run the live provider test automatically:

```bash
MODEL_API_TYPE=anthropic-messages
MODEL_BASE_URL=https://api.anthropic.com
MODEL_API_KEY=<your-api-key>
MODEL_NAME=claude-sonnet-4-6
```

When these variables are present in the process environment or root .env as a
complete configuration, the live API test runs automatically. The live test
skips when any required variable is missing. Do not commit real credentials; use
local `.env` files or secret-managed environment variables. Use verbose mode to
show the provider response and safe observer metadata:

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

If the live provider call fails, enable safe diagnostics:

```bash
LIVE_API_DEBUG=1 go test -v -run '^TestLiveAPIModelRun$' .
```

Debug output includes only sanitized metadata such as status code and a redacted,
bounded error summary.

Run a specific test with:

```bash
go test -v -run '^TestName$' ./...
```

## Change Guidelines

- Keep the SDK dependency-light and prefer the Go standard library.
- Preserve compatibility for existing public types and sentinel errors unless a
  breaking change is intentional and documented.
- Use local fakes, `httptest`, or fake stdio processes instead of real network
  services.
- Add focused tests for new behavior and regression fixes.
- Keep examples runnable without credentials unless the example clearly states
  which environment variables an application must provide.
- Avoid logging or exposing tool arguments, credentials, prompts, or other
  sensitive values in diagnostics.

## Documentation

Update README examples or API notes when a user-facing behavior changes. Update
the changelog for notable additions, fixes, or compatibility changes.

## License

By contributing, you agree that your contributions are licensed under the MIT
License used by this project.
