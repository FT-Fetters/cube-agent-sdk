package agent

import "github.com/cubence/cube-agent-sdk/internal/core"

type instructionFile struct {
	path    string
	content string
}

func cloneMessages(messages []Message) []Message {
	return core.CloneMessages(messages)
}

func cloneMessage(message Message) Message {
	return core.CloneMessage(message)
}

func cloneAnyMap(source map[string]any) map[string]any {
	return core.CloneAnyMap(source)
}

func cloneAny(value any) any {
	return core.CloneAny(value)
}
