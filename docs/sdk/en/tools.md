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
types, including object properties, required fields, array item schemas,
`enum`, `default`, numeric `minimum`/`maximum`, string
`minLength`/`maxLength`, array `minItems`/`maxItems`, `pattern`, and boolean
`additionalProperties`.

Defaults are emitted in the provider schema but are not injected into tool
arguments. If a tool has no schema, the SDK keeps the compatibility path and
executes it without preflight argument validation.

Schema validation failures wrap `ErrToolValidation`, include the exact parameter
path, do not include rejected argument values, and prevent the tool function
from being called.

## Struct Schema Generation

Use `ToolParametersSchemaFromStruct` to derive a schema from exported struct
fields without adding dependencies:

```go
type LookupArgs struct {
	AccountID string   `json:"account_id" description:"Application account identifier" required:"true" pattern:"^acct_[a-z0-9]+$"`
	Tier      string   `json:"tier,omitempty" enum:"free,pro,enterprise" default:"pro"`
	Limit     int      `json:"limit,omitempty" min:"1" max:"50" default:"10"`
	Tags      []string `json:"tags,omitempty" minItems:"1" maxItems:"5"`
}

parameters, err := agent.ToolParametersSchemaFromStruct(LookupArgs{})
```

Supported tags are `json`, `description`, `required`, `enum`, `default`, `min`,
`max`, `minLength`, `maxLength`, `minItems`, `maxItems`, `pattern`, and
`additionalProperties`. The generator supports nested structs, pointers,
slices, arrays, primitive scalar types, and omitted fields with `json:"-"`.
Maps, interfaces, functions, channels, full JSON Schema composition, and default
argument injection are intentionally outside this subset.

## Risk Labels

Use risk labels to make approval policy decisions explicit:

- `ToolRiskRead`
- `ToolRiskWrite`
- `ToolRiskDestructive`
- `ToolRiskUnspecified`

Production tools should declare schemas and risk labels. Applications still own
business logic, data access, side effects, and result content.
