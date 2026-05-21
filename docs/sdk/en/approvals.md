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

Approval events and observations include the result, reason, tool name, and
risk. They intentionally omit tool arguments.
