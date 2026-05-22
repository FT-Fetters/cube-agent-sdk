# Changelog

All notable changes to this project will be documented in this file.

This project follows a simple source-level changelog until the first tagged
release. Dates use ISO 8601 format.

## Unreleased

### Added

- Initial SDK runtime for managed agent conversations, tools, approvals, hooks,
  observers, skills, compaction, session state, MCP metadata, and subagents.
- OpenAI-compatible chat completions adapter with local HTTP test coverage.
- OpenAI Responses API adapter with text output, function tools, tool-call
  parsing, and raw output replay for tool loops.
- Unified model factory for selecting provider API type from configuration.
- Anthropic Messages adapter for `/v1/messages` endpoints.
- Structured tool schemas and validation before tool execution.
- Streaming model interface and agent streaming API.
- Provider-native streaming for OpenAI-compatible chat completions, OpenAI
  Responses, and Anthropic Messages adapters, including final usage telemetry
  when providers report it.
- Composable reliable model wrapper for retries, timeouts, backoff, rate limits,
  circuit breaking, token/cost budgets, and safe reliability events.
- Minimal MCP stdio client and tool bridge.
- Session snapshot, restore, reset, and fork APIs.
- Dependency-free session persistence contracts, append-only session event logs,
  in-memory store/log implementation, schema/version validation, and session
  persistence error sentinels.
- Structured errors and enriched lifecycle event metadata.
- Production-oriented observer interface with sanitized event details.
- Safer approval policy helpers for deny-by-default, allowlists, and tool risk
  levels.
- Runnable local examples and README coverage for core SDK workflows.
- CI workflow for tests, race tests, and vet.

### Documentation

- Added MIT license, contribution guide, changelog, and release quality gates.
