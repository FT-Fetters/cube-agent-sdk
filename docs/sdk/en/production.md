# Production

The SDK intentionally keeps production infrastructure outside the core runtime.
Use the SDK primitives, but make deployment, security, telemetry, and storage
decisions in the application.

## Integration Checklist

1. Create a model adapter with explicit timeouts, retries, request logging, and
   provider-specific error mapping.
2. Load credentials, base URLs, MCP command paths, and secrets from the
   deployment environment.
3. Register only the tools the agent needs, with schemas and risk labels.
4. Install a deny-by-default approval policy and connect `ApprovalFunc` to the
   human or business approval workflow.
5. Attach an `Observer` that exports sanitized metadata to logging, metrics, or
   tracing systems.
6. Persist `SessionSnapshot` payloads in application-owned storage with access
   controls and retention policy.
7. Run external MCP servers under application process supervision and least
   privilege.
8. Add rate limiting, provider quotas, and rollout controls around agent entry
   points.
9. Keep raw tool arguments, tool results, model content, and provider errors out
   of general telemetry unless the product explicitly requires and protects
   them.

## Security Notes

- Prefer allowlists over denylists for production tool access.
- Treat destructive tools as a separate workflow requiring explicit approval.
- Use short-lived credentials where possible.
- Bound model and tool execution with contexts and timeouts.
- Review session snapshot retention because snapshots can contain user content.
