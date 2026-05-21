# Tools

Tools are local Go functions that a model can request. The SDK describes tools
to the model, validates arguments when a schema is provided, runs approval, and
executes the local function.

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

Register tools at construction:

```go
bot, err := agent.New(cfg, model, agent.WithTools(lookup))
```

## Tool Interfaces

A custom tool implements:

- `Name() string`
- `Description() string`
- `Call(context.Context, ToolCall) (ToolResult, error)`

Optional extensions:

- `ParametersSchema() *ToolParametersSchema`
- `Risk() ToolRisk`

## Schema Support

`ToolParametersSchema` is a lightweight JSON Schema subset for function calling
arguments. It supports string, number, integer, boolean, object, and array
types, including object properties, required fields, and array item schemas.

If a tool has no schema, the SDK keeps the compatibility path and executes it
without preflight argument validation.

Schema validation failures wrap `ErrToolValidation`, and the tool function is
not called.

## Risk Labels

Use risk labels to make approval policy decisions explicit:

- `ToolRiskRead`
- `ToolRiskWrite`
- `ToolRiskDestructive`
- `ToolRiskUnspecified`

Production tools should declare schemas and risk labels. Applications still own
business logic, data access, side effects, and result content.
