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

审批事件和 observation 包含结果、原因、工具名和风险。它们有意不暴露工具参数。
