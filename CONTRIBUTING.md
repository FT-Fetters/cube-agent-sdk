# Contributing

Thanks for improving Cube Agent SDK. This project is intentionally small and
keeps provider credentials, external services, persistent storage, and telemetry
exporters outside the core runtime.

## Development Setup

Prerequisites:

- Go 1.22 or newer.
- No real API keys or live model providers are required for the test suite.

Run the local quality gate before sending changes:

```bash
go test ./...
go test -race ./...
go vet ./...
go test -count=1 ./...
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
