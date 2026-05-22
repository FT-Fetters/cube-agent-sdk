# 审批

每个 tool call 都会经过 `ApprovalPolicy`。默认策略是 `AllowAllApproval`，用于
兼容；生产 agent 应该安装显式策略。

## 内置策略

- `DenyAllApproval`：拒绝所有 tool call。
- `AllowToolsApproval`：只批准指定名称的工具。
- `DenyToolsApproval`：阻止指定工具，批准其他工具。
- `AllowRisksApproval`：只批准指定风险类别。
- `RequireAllApprovals`：用 AND 语义组合策略。
- `ApprovalFunc`：把应用逻辑适配为审批策略。

## 默认拒绝示例

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

如果策略拒绝工具调用，SDK 返回的错误可通过
`errors.Is(err, agent.ErrApprovalDenied)` 识别。

## 基于作用域的审批

Write 和 destructive 工具可以通过 `ToolSafety` 绑定作用域和业务原因。审批策略会在 `ApprovalRequest.ToolSafety` 中收到这些值；生命周期遥测只会收到数量和 hash。

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

不要记录原始 `ApprovalRequest`：`ToolCall` 包含模型参数，`ToolSafety.Scopes` 或 `BusinessReason` 也可能包含应用敏感标识。

## 人类或业务审批

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

审批事件和 observation 包含结果、决策原因、工具名、风险、工具限制、scope 数量、scope hash 和 business-reason hash。它们有意不暴露工具参数、原始 scope value 或原始工具业务原因。
