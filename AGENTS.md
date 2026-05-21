# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go 1.22 module for `github.com/cubence/cube-agent-sdk`.
Public SDK types and behavior live in root-level `.go` files, with tests
co-located as `*_test.go`. Internal implementation details are under
`internal/`, including provider adapters, schema helpers, skills, MCP stdio, and
core types. Runnable examples live in `examples/`; keep examples credential-free
unless they are explicitly marked as live-provider examples. End-user
documentation lives under `docs/sdk/` with English and Chinese versions, while
CI configuration lives in `.github/workflows/`.

## Build, Test, and Development Commands

- `go test ./...` runs the deterministic unit and example coverage used by CI.
- `go test -race ./...` runs the race detector across all packages.
- `go vet ./...` performs static checks.
- `go test -count=1 ./...` bypasses the test cache for a clean local gate.
- `go run ./examples/tool_schema` runs a local example without provider
  credentials.
- `go test -v -run '^TestLiveAPIModelRun$' .` runs the optional live-provider
  test when the required model environment is configured.

## Coding Style & Naming Conventions

Use idiomatic Go formatted with `gofmt`. Keep the SDK dependency-light and
prefer the standard library. Exported identifiers should use clear Go names and
include helpful documentation when they are part of the public API. Test helpers
should stay close to the behavior they support. Add comments where they clarify
non-obvious behavior, safety constraints, or public contracts; avoid comments
that merely restate the code. If future JavaScript tooling is introduced, prefer
`pnpm` over `npm`.

## Testing Guidelines

Use Go's standard `testing` package. Name tests `Test...` and place them in
`*_test.go` files near the code under test. Prefer local fakes, `httptest`
servers, and fake stdio processes over real network services. Live API tests are
opt-in through `MODEL_API_TYPE`, `MODEL_BASE_URL`, `MODEL_API_KEY`, and
`MODEL_NAME`; they should skip safely when configuration is incomplete.

## Commit & Pull Request Guidelines

Recent history follows concise Conventional Commits-style messages such as
`feat(openai): add OpenAI Responses API adapter`, `test: add safe live api
diagnostics`, and `docs: document live api tests`. Keep commits focused. Pull
requests should describe behavior changes, list the verification commands run,
link relevant issues, and update README, SDK docs, or changelog entries for
user-facing changes.

## Security & Configuration Tips

Never commit real credentials. Keep local `.env` files private and rely on
environment variables or secret management for live tests. Diagnostic output
must redact credentials, prompts, tool arguments, and other sensitive values.
