# 工具

工具是模型可以请求执行的本地 Go 函数。SDK 会向模型描述工具，在提供 schema 时
校验参数，执行审批，然后调用本地函数。

## ToolFunc

```go
lookup := agent.ToolFunc{
	ToolName:        "lookup_account",
	ToolDescription: "Read account status",
	ToolRisk:        agent.ToolRiskRead,
	Parameters: &agent.ToolParametersSchema{
		Type:     agent.SchemaTypeObject,
		Required: []string{"account_id"},
		Properties: map[string]agent.ToolParametersSchema{
			"account_id": {
				Type:        agent.SchemaTypeString,
				Description: "Application account identifier",
			},
		},
	},
	Fn: func(ctx context.Context, call agent.ToolCall) (agent.ToolResult, error) {
		accountID, _ := call.Arguments["account_id"].(string)
		return agent.ToolResult{
			CallID:  call.ID,
			Name:    call.Name,
			Content: "account " + accountID + " is active",
		}, nil
	},
}
```

在构造时注册工具：

```go
bot, err := agent.New(cfg, model, agent.WithTools(lookup))
```

## 工具接口

自定义工具实现：

- `Name() string`
- `Description() string`
- `Call(context.Context, ToolCall) (ToolResult, error)`

可选扩展：

- `ParametersSchema() *ToolParametersSchema`
- `Risk() ToolRisk`

## Schema 支持

`ToolParametersSchema` 是面向 function calling 参数的轻量 JSON Schema 子集。它
支持 string、number、integer、boolean、object 和 array 类型，也支持 object
properties、required 字段和 array item schema。

如果工具没有 schema，SDK 会保留兼容路径，不做执行前参数校验。

Schema 校验失败会包装 `ErrToolValidation`，并且不会调用工具函数。

## 风险标签

使用风险标签让审批策略显式可控：

- `ToolRiskRead`
- `ToolRiskWrite`
- `ToolRiskDestructive`
- `ToolRiskUnspecified`

生产工具应该声明 schema 和风险标签。业务逻辑、数据访问、副作用和结果内容仍由
应用负责。
