package agent

import (
	"context"
	"errors"
	"reflect"
)

// Role identifies who produced a message in the agent context.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one item in the conversation context managed by an Agent.
type Message struct {
	Role       Role
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
	Metadata   map[string]any
}

// Model is the LLM adapter used by an Agent.
type Model interface {
	Generate(context.Context, ModelRequest) (ModelResponse, error)
}

// StreamModel is an optional Model extension for adapters that can emit text
// incrementally. Agents fall back to ErrStreamingUnsupported when it is absent.
type StreamModel interface {
	Model
	Stream(context.Context, ModelRequest) (<-chan StreamEvent, error)
}

var (
	// ErrStreamingUnsupported marks models that only implement non-streaming generation.
	ErrStreamingUnsupported = errors.New("agent: streaming unsupported")
	// ErrStreamingToolCallsUnsupported marks streamed tool calls, which are not executed yet.
	ErrStreamingToolCallsUnsupported = errors.New("agent: streaming tool calls unsupported")
)

// StreamEventType identifies the kind of event produced by a streaming run.
type StreamEventType string

const (
	StreamEventDelta StreamEventType = "delta"
	StreamEventDone  StreamEventType = "done"
	StreamEventError StreamEventType = "error"
)

// StreamEvent carries one item from a streaming run. Delta contains incremental
// assistant text; Message is populated on done; Error is populated on error.
type StreamEvent struct {
	Type    StreamEventType
	AgentID string
	Delta   string
	Message Message
	Error   error
}

// ModelRequest is the fully assembled prompt and capability set for one model call.
type ModelRequest struct {
	AgentID      string
	SystemPrompt string
	Messages     []Message
	Tools        []ToolDescriptor
	MCPServers   []MCPServerConfig
	ActiveSkills []Skill
}

// ModelResponse is the model output. It may return a final assistant message or tool calls.
type ModelResponse struct {
	Message   Message
	ToolCalls []ToolCall
}

// Config defines the stable behavior of an Agent.
type Config struct {
	ID            string
	SystemPrompt  string
	Compact       CompactConfig
	MaxToolRounds int
}

type instructionFile struct {
	path    string
	content string
}

func cloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]Message, len(messages))
	for i, message := range messages {
		cloned[i] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message Message) Message {
	message.ToolCalls = cloneToolCalls(message.ToolCalls)
	if len(message.Metadata) > 0 {
		message.Metadata = cloneAnyMap(message.Metadata)
	}
	return message
}

func cloneAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneAny(value)
	}
	return cloned
}

func cloneAny(value any) any {
	if value == nil {
		return nil
	}
	cloned := cloneAnyValue(reflect.ValueOf(value))
	if !cloned.IsValid() {
		return nil
	}
	return cloned.Interface()
}

// cloneAnyValue isolates container values commonly stored in metadata and tool arguments.
func cloneAnyValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return cloneAnyValue(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneAssignableValue(value.Elem(), value.Type().Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(iter.Key(), cloneAssignableValue(iter.Value(), value.Type().Elem()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneAssignableValue(value.Index(i), value.Type().Elem()))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneAssignableValue(value.Index(i), value.Type().Elem()))
		}
		return cloned
	default:
		return value
	}
}

func cloneAssignableValue(value reflect.Value, target reflect.Type) reflect.Value {
	cloned := cloneAnyValue(value)
	if !cloned.IsValid() {
		return reflect.Zero(target)
	}
	if cloned.Type().AssignableTo(target) {
		return cloned
	}
	if cloned.Type().ConvertibleTo(target) {
		return cloned.Convert(target)
	}
	if target.Kind() == reflect.Interface && cloned.Type().Implements(target) {
		return cloned
	}
	return value
}
