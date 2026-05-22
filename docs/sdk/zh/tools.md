# 工具

工具是模型可以请求执行的本地 Go 函数。SDK 会向模型描述工具，在提供 schema 时
校验参数，执行审批，然后调用本地函数。

## ToolFunc

```go
lookup := agent.ToolFunc{
	ToolName:        "lookup_account",
	ToolDescription: "Read account status",
	ToolRisk:        agent.ToolRiskRead,
	Safety: agent.ToolSafety{
		Timeout:        2 * time.Second,
		MaxConcurrency: 8,
		MaxResultBytes: 4096,
	},
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
- `ToolSafety() ToolSafety`

## Schema 支持

`ToolParametersSchema` 是面向 function calling 参数的轻量 JSON Schema 子集。它
支持 string、number、integer、boolean、object 和 array 类型，也支持 object
properties、required 字段、array item schema、`enum`、`default`、数字
`minimum`/`maximum`、字符串 `minLength`/`maxLength`、数组
`minItems`/`maxItems`、`pattern` 和布尔型 `additionalProperties`。

`default` 会输出到 provider schema，但不会被注入到工具参数中。如果工具没有
schema，SDK 会保留兼容路径，不做执行前参数校验。

Schema 校验失败会包装 `ErrToolValidation`，错误中包含精确参数路径，不包含被拒绝的
参数值，并且不会调用工具函数。

## 结构体 Schema 生成

可以使用 `ToolParametersSchemaFromStruct` 从导出的结构体字段生成 schema，不引入额外
依赖：

```go
type LookupArgs struct {
	AccountID string   `json:"account_id" description:"Application account identifier" required:"true" pattern:"^acct_[a-z0-9]+$"`
	Tier      string   `json:"tier,omitempty" enum:"free,pro,enterprise" default:"pro"`
	Limit     int      `json:"limit,omitempty" min:"1" max:"50" default:"10"`
	Tags      []string `json:"tags,omitempty" minItems:"1" maxItems:"5"`
}

parameters, err := agent.ToolParametersSchemaFromStruct(LookupArgs{})
```

支持的 tag 包括 `json`、`description`、`required`、`enum`、`default`、`min`、
`max`、`minLength`、`maxLength`、`minItems`、`maxItems`、`pattern` 和
`additionalProperties`。生成器支持嵌套结构体、指针、切片、数组、基础标量类型，
以及 `json:"-"` 忽略字段。map、interface、函数、channel、完整 JSON Schema 组合和
默认参数注入不在这个轻量子集内。

## 工具安全边界

`ToolSafety` 让每个工具声明由 SDK 执行的 guardrails 和审批上下文：

- `Risk`：read、write、destructive 或 unspecified 风险。`ToolFunc.ToolRisk` 继续可用；如果希望把安全配置放在一起，可使用 `Safety.Risk`。
- `Timeout`：单次工具调用的最长耗时。SDK 会传入带 deadline 的 context；超时返回 `context.DeadlineExceeded`。
- `MaxConcurrency`：同一个 `Agent` 上该工具的最大并发执行数；超过限制会返回 `ErrToolConcurrencyLimitExceeded`。
- `MaxResultBytes`：`ToolResult.Content` 最大字节数；成功但过大的结果会以 `ErrToolResultTooLarge` 失败，且不会进入 agent context。
- `Scopes`：应用定义的安全边界，例如 tenant、文件根目录或下游服务 scope。scope value 会传给审批策略，但遥测只包含数量和 hash。
- `BusinessReason`：应用定义的副作用审批原因或工单标识。Observation 只包含 hash。

对于 MCP client 或第三方库返回的工具，可以包一层而不是重写工具：

```go
for i, tool := range tools {
	tools[i] = agent.ToolWithSafety(tool, agent.ToolSafety{
		Risk:           agent.ToolRiskRead,
		Timeout:        2 * time.Second,
		MaxConcurrency: 4,
		MaxResultBytes: 8192,
		Scopes:         []agent.ToolScope{{Kind: "mcp_server", Value: "filesystem-readonly"}},
	})
}
```

## 风险标签

使用风险标签让审批策略显式可控：

- `ToolRiskRead`
- `ToolRiskWrite`
- `ToolRiskDestructive`
- `ToolRiskUnspecified`

生产工具应该声明 schema 和风险标签。业务逻辑、数据访问、副作用和结果内容仍由
应用负责。
