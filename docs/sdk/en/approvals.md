# Approvals

Every tool call passes through an `ApprovalPolicy`. The default policy is
`AllowAllApproval` for compatibility, but production agents should install an
explicit policy.

## Built-In Policies

- `DenyAllApproval`: rejects every tool call.
- `AllowToolsApproval`: approves only named tools.
- `DenyToolsApproval`: blocks selected tools and approves the rest.
- `AllowRisksApproval`: approves only selected risk classes.
- `RequireAllApprovals`: composes policies with AND semantics.
- `ApprovalFunc`: adapts application logic into an approval policy.

## Deny-by-Default Example

```go
policy := agent.RequireAllApprovals(
	agent.AllowToolsApproval("lookup_account"),
	agent.AllowRisksApproval(agent.ToolRiskRead),
)

bot, err := agent.New(cfg, model,
	agent.WithTools(lookup),
	agent.WithApprovalPolicy(policy),
)
```

If a policy denies a tool call, the SDK returns an error compatible with
`errors.Is(err, agent.ErrApprovalDenied)`.

## Scope-Aware Approval

Write and destructive tools can bind scopes and business reasons through `ToolSafety`. The approval policy receives those values in `ApprovalRequest.ToolSafety`; lifecycle telemetry receives only counts and hashes.

```go
writeLedger := agent.ToolFunc{
	ToolName: "write_ledger",
	Safety: agent.ToolSafety{
		Risk:           agent.ToolRiskWrite,
		Timeout:        2 * time.Second,
		MaxConcurrency: 2,
		MaxResultBytes: 4096,
		Scopes:         []agent.ToolScope{{Kind: "tenant", Value: tenantID}},
		BusinessReason: changeTicketID,
	},
	Fn: writeLedgerFn,
}

policy := agent.ApprovalFunc(func(ctx context.Context, request agent.ApprovalRequest) (agent.ApprovalDecision, error) {
	if request.Risk != agent.ToolRiskRead && len(request.ToolSafety.Scopes) == 0 {
		return agent.ApprovalDecision{Approved: false, Reason: "side-effecting tools require a scope"}, nil
	}
	if request.Risk == agent.ToolRiskDestructive && request.ToolSafety.BusinessReason == "" {
		return agent.ApprovalDecision{Approved: false, Reason: "destructive tools require a business reason"}, nil
	}
	return agent.ApprovalDecision{Approved: true, Reason: "scope policy approved"}, nil
})
```

Do not log raw `ApprovalRequest` values: `ToolCall` contains model arguments, and `ToolSafety.Scopes` or `BusinessReason` can contain application-sensitive identifiers.

## Human or Business Approval

```go
policy := agent.ApprovalFunc(func(ctx context.Context, request agent.ApprovalRequest) (agent.ApprovalDecision, error) {
	if request.Risk == agent.ToolRiskDestructive {
		return agent.ApprovalDecision{
			Approved: false,
			Reason:   "destructive tools require a separate workflow",
		}, nil
	}
	return agent.ApprovalDecision{Approved: true, Reason: "business policy approved"}, nil
})
```

Approval events and observations include the result, decision reason, tool name, risk, tool limits, scope count, scope hash, and business-reason hash. They intentionally omit tool arguments, raw scope values, and raw tool business reasons.
