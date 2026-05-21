# Getting Started

## Install

```bash
go get github.com/cubence/cube-agent-sdk
```

The module has no third-party Go dependencies.

## Verify the Repository

Run the default local test suite and compile examples:

```bash
go test ./...
```

Run a local example that does not require real credentials:

```bash
go run ./examples/tool_schema
```

## Minimal Agent

```go
package main

import (
	"context"
	"fmt"
	"log"

	agent "github.com/cubence/cube-agent-sdk"
)

type model struct{}

func (model) Generate(context.Context, agent.ModelRequest) (agent.ModelResponse, error) {
	return agent.ModelResponse{
		Message: agent.Message{Role: agent.RoleAssistant, Content: "ok"},
	}, nil
}

func main() {
	bot, err := agent.New(agent.Config{
		SystemPrompt: "You are a focused coding agent.",
	}, model{})
	if err != nil {
		log.Fatal(err)
	}

	reply, err := bot.Run(context.Background(), "Say hello.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reply.Content)
}
```

## Next Steps

1. Choose a model adapter or implement `Model`.
2. Register tools only when the agent needs local capabilities.
3. Add schemas and risk labels to every production tool.
4. Install an explicit approval policy before exposing tools to real users.
5. Attach observers for telemetry and persist `SessionSnapshot` values if
   conversations must survive process restarts.
