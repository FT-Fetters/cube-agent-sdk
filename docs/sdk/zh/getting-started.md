# 快速开始

## 安装

```bash
go get github.com/cubence/cube-agent-sdk
```

该模块没有第三方 Go 依赖。

## 验证仓库

运行默认本地测试并编译示例：

```bash
go test ./...
```

运行一个不需要真实凭证的本地示例：

```bash
go run ./examples/tool_schema
```

## 最小 Agent

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

## 后续步骤

1. 选择模型适配器，或实现 `Model`。
2. 只有当 agent 需要本地能力时才注册工具。
3. 为每个生产工具补充 schema 和风险标签。
4. 在真实用户可以触发工具前安装显式审批策略。
5. 如果会话需要跨进程恢复，接入 observer 做遥测，并持久化
   `SessionSnapshot`。
