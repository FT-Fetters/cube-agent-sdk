package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/cubence/cube-agent-sdk/internal/schema"
	"github.com/cubence/cube-agent-sdk/internal/skills"
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
	ActiveSkills []skills.Skill
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

// CompactConfig controls when and how the agent compacts conversation context.
type CompactConfig struct {
	MaxTokens int
	Threshold float64
	KeepLast  int
}

// TokenCounter estimates token usage for threshold checks.
type TokenCounter interface {
	Count(Message) int
}

// TokenCounterFunc adapts a function into a TokenCounter.
type TokenCounterFunc func(Message) int

func (f TokenCounterFunc) Count(message Message) int {
	return f(message)
}

// ApproxTokenCounter is a dependency-free token estimator suitable for threshold triggers.
type ApproxTokenCounter struct{}

func (ApproxTokenCounter) Count(message Message) int {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return 0
	}
	words := len(strings.Fields(content))
	chars := int(math.Ceil(float64(len(content)) / 4.0))
	if chars > words {
		return chars
	}
	return words
}

// Compactor replaces a long message history with a shorter one.
type Compactor interface {
	Compact(context.Context, []Message) ([]Message, error)
}

// SummaryCompactor creates a deterministic local summary when no model-backed compactor is provided.
type SummaryCompactor struct {
	KeepLast int
}

func (c SummaryCompactor) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	keep := c.KeepLast
	if keep <= 0 {
		keep = 4
	}
	if len(messages) <= keep {
		return CloneMessages(messages), nil
	}
	summary := Message{
		Role:    RoleSystem,
		Content: fmt.Sprintf("Conversation compacted: %d earlier messages were summarized by the SDK.", len(messages)-keep),
	}
	result := []Message{summary}
	result = append(result, CloneMessages(messages[len(messages)-keep:])...)
	return result, nil
}

// ModelCompactor asks a model to summarize older context and keeps recent messages intact.
type ModelCompactor struct {
	Model        Model
	SystemPrompt string
	KeepLast     int
}

func (c ModelCompactor) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	if c.Model == nil {
		return nil, errors.New("agent: compactor model is required")
	}
	keep := c.KeepLast
	if keep <= 0 {
		keep = 4
	}
	if len(messages) <= keep {
		return CloneMessages(messages), nil
	}

	older := CloneMessages(messages[:len(messages)-keep])
	recent := CloneMessages(messages[len(messages)-keep:])
	prompt := strings.TrimSpace(c.SystemPrompt)
	if prompt == "" {
		prompt = "Summarize the conversation context for a future agent turn. Preserve decisions, constraints, open tasks, and user preferences."
	}
	response, err := c.Model.Generate(ctx, ModelRequest{
		SystemPrompt: prompt,
		Messages:     older,
	})
	if err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(response.Message.Content)
	if summary == "" {
		summary = "Earlier context was compacted, but the summarizer returned an empty summary."
	}
	result := []Message{{
		Role:    RoleSystem,
		Content: "Context summary:\n" + summary,
	}}
	result = append(result, recent...)
	return result, nil
}

// MCPTransport identifies how an MCP server is reached.
type MCPTransport string

const (
	MCPTransportStdio MCPTransport = "stdio"
	MCPTransportSSE   MCPTransport = "sse"
	MCPTransportHTTP  MCPTransport = "http"
)

// MCPServerConfig describes an MCP server available to an agent.
type MCPServerConfig struct {
	Name      string
	Command   string
	Args      []string
	Env       map[string]string
	URL       string
	Transport MCPTransport
}

// ToolDescriptor is the model-facing description of an available tool.
type ToolDescriptor struct {
	Name        string
	Description string
	Parameters  *schema.ToolParametersSchema
	Risk        ToolRisk
}

// ToolRisk labels the expected side-effect profile of a tool.
type ToolRisk string

const (
	ToolRiskUnspecified ToolRisk = ""
	ToolRiskRead        ToolRisk = "read"
	ToolRiskWrite       ToolRisk = "write"
	ToolRiskDestructive ToolRisk = "destructive"
)

// ToolCall is a model request to execute a tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult is the output of a tool call.
type ToolResult struct {
	CallID   string
	Name     string
	Content  string
	Metadata map[string]any
}

// Tool is implemented by callable agent tools.
type Tool interface {
	Name() string
	Description() string
	Call(context.Context, ToolCall) (ToolResult, error)
}

// ToolParametersSchemaProvider is an optional extension for tools with JSON Schema parameters.
type ToolParametersSchemaProvider interface {
	ParametersSchema() *schema.ToolParametersSchema
}

// ToolRiskProvider is an optional extension for tools that declare side-effect risk.
type ToolRiskProvider interface {
	Risk() ToolRisk
}

// ToolFunc adapts a function into a Tool.
type ToolFunc struct {
	ToolName        string
	ToolDescription string
	Parameters      *schema.ToolParametersSchema
	ToolRisk        ToolRisk
	Fn              func(context.Context, ToolCall) (ToolResult, error)
}

func (t ToolFunc) Name() string {
	return t.ToolName
}

func (t ToolFunc) Description() string {
	return t.ToolDescription
}

func (t ToolFunc) ParametersSchema() *schema.ToolParametersSchema {
	return schema.Clone(t.Parameters)
}

func (t ToolFunc) Risk() ToolRisk {
	return t.ToolRisk
}

func (t ToolFunc) Call(ctx context.Context, call ToolCall) (ToolResult, error) {
	if t.Fn == nil {
		return ToolResult{}, errors.New("agent: tool function is nil")
	}
	return t.Fn(ctx, call)
}

// ApprovalRequest describes a tool call awaiting permission.
type ApprovalRequest struct {
	AgentID  string
	ToolName string
	Risk     ToolRisk
	ToolCall ToolCall
}

// ApprovalDecision is the result of a permission check.
type ApprovalDecision struct {
	Approved bool
	Reason   string
}

// ApprovalPolicy decides whether a tool call may execute.
type ApprovalPolicy interface {
	ApproveTool(context.Context, ApprovalRequest) (ApprovalDecision, error)
}

// ApprovalFunc adapts a function into an ApprovalPolicy.
type ApprovalFunc func(context.Context, ApprovalRequest) (ApprovalDecision, error)

func (f ApprovalFunc) ApproveTool(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	return f(ctx, request)
}

// AllowAllApproval approves every tool call.
type AllowAllApproval struct{}

func (AllowAllApproval) ApproveTool(context.Context, ApprovalRequest) (ApprovalDecision, error) {
	return ApprovalDecision{Approved: true, Reason: "allowed"}, nil
}

// ErrorCategory groups operational failures so callers can audit and branch
// without relying on provider-specific error text.
type ErrorCategory string

const (
	ErrorCategoryModel     ErrorCategory = "model"
	ErrorCategoryTool      ErrorCategory = "tool"
	ErrorCategoryApproval  ErrorCategory = "approval"
	ErrorCategorySchema    ErrorCategory = "schema"
	ErrorCategoryMCP       ErrorCategory = "mcp"
	ErrorCategoryCompact   ErrorCategory = "compact"
	ErrorCategorySubagent  ErrorCategory = "subagent"
	ErrorCategoryStreaming ErrorCategory = "streaming"
	ErrorCategoryHook      ErrorCategory = "hook"
	ErrorCategoryConfig    ErrorCategory = "config"
)

// AgentError adds stable SDK context around an underlying error. Unwrap keeps
// existing sentinel checks such as errors.Is(err, ErrApprovalDenied) working.
type AgentError struct {
	Category  ErrorCategory
	Operation string
	AgentID   string
	RunID     string
	RequestID string
	// ParentRequestID links nested failures to the request that caused them.
	ParentRequestID string
	ToolName        string
	SubagentID      string
	Round           int
	Cause           error
}

func (e *AgentError) Error() string {
	if e == nil {
		return ""
	}
	var parts []string
	if e.Category != "" {
		parts = append(parts, string(e.Category))
	}
	if e.Operation != "" {
		parts = append(parts, e.Operation)
	}
	if len(parts) == 0 {
		parts = append(parts, "operation")
	}
	message := "agent: " + strings.Join(parts, " ")
	if e.Cause == nil {
		return message
	}
	return fmt.Sprintf("%s: %v", message, e.Cause)
}

func (e *AgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// EventType identifies a lifecycle hook event.
type EventType string

const (
	EventBeforeModel     EventType = "before_model"
	EventAfterModel      EventType = "after_model"
	EventBeforeApproval  EventType = "before_approval"
	EventAfterApproval   EventType = "after_approval"
	EventBeforeTool      EventType = "before_tool"
	EventAfterTool       EventType = "after_tool"
	EventBeforeCompact   EventType = "before_compact"
	EventAfterCompact    EventType = "after_compact"
	EventSkillActivated  EventType = "skill_activated"
	EventSubagentMessage EventType = "subagent_message"
)

// Event carries lifecycle data to hooks.
type Event struct {
	Type       EventType
	AgentID    string
	RunID      string
	SubagentID string
	ToolName   string
	ToolRisk   ToolRisk
	SkillName  string
	RequestID  string
	// ParentRequestID links nested lifecycle events to the request that caused them.
	ParentRequestID string
	Round           int
	Duration        time.Duration
	EstimatedTokens int
	Approved        bool
	ApprovalReason  string
	ErrorCategory   ErrorCategory
	Message         Message
	ToolCall        ToolCall
	ToolResult      ToolResult
	Error           error
}

// Hook observes or rejects lifecycle events.
type Hook func(context.Context, Event) error

// Observer receives sanitized lifecycle metadata for production telemetry.
type Observer interface {
	Observe(context.Context, Observation)
}

// ObserverFunc adapts a function into an Observer.
type ObserverFunc func(context.Context, Observation)

func (f ObserverFunc) Observe(ctx context.Context, observation Observation) {
	if f != nil {
		f(ctx, observation)
	}
}

// NoopObserver drops every observation without output or dependencies.
type NoopObserver struct{}

func (NoopObserver) Observe(context.Context, Observation) {}

// Observation is a safe telemetry view of an Event. It intentionally omits
// message content, tool arguments, tool results, raw errors, and MCP settings.
type Observation struct {
	Type       EventType
	AgentID    string
	RunID      string
	SubagentID string
	ToolName   string
	ToolRisk   ToolRisk
	SkillName  string
	RequestID  string
	// ParentRequestID links nested telemetry records to the request that caused them.
	ParentRequestID string
	Round           int
	Duration        time.Duration
	EstimatedTokens int
	Approved        bool
	ApprovalReason  string
	ErrorCategory   ErrorCategory
	Failed          bool
}

// ObservationFromEvent converts a lifecycle event into safe telemetry metadata.
func ObservationFromEvent(event Event) Observation {
	return Observation{
		Type:            event.Type,
		AgentID:         event.AgentID,
		RunID:           event.RunID,
		SubagentID:      event.SubagentID,
		ToolName:        event.ToolName,
		ToolRisk:        event.ToolRisk,
		SkillName:       event.SkillName,
		RequestID:       event.RequestID,
		ParentRequestID: event.ParentRequestID,
		Round:           event.Round,
		Duration:        event.Duration,
		EstimatedTokens: event.EstimatedTokens,
		Approved:        event.Approved,
		ApprovalReason:  event.ApprovalReason,
		ErrorCategory:   event.ErrorCategory,
		Failed:          event.Error != nil || event.ErrorCategory != "",
	}
}

// CloneMessages returns a deep copy of a message slice.
func CloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]Message, len(messages))
	for i, message := range messages {
		cloned[i] = CloneMessage(message)
	}
	return cloned
}

// CloneMessage returns a deep copy of one message.
func CloneMessage(message Message) Message {
	message.ToolCalls = CloneToolCalls(message.ToolCalls)
	if len(message.Metadata) > 0 {
		message.Metadata = CloneAnyMap(message.Metadata)
	}
	return message
}

// CloneToolCalls returns a deep copy of tool calls.
func CloneToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	cloned := make([]ToolCall, len(calls))
	for i, call := range calls {
		cloned[i] = CloneToolCall(call)
	}
	return cloned
}

// CloneToolCall returns a deep copy of one tool call.
func CloneToolCall(call ToolCall) ToolCall {
	call.Arguments = CloneAnyMap(call.Arguments)
	return call
}

// CloneToolResult returns a deep copy of one tool result.
func CloneToolResult(result ToolResult) ToolResult {
	result.Metadata = CloneAnyMap(result.Metadata)
	return result
}

// CloneAnyMap deep-copies common container values used in metadata and arguments.
func CloneAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = CloneAny(value)
	}
	return cloned
}

// CloneAny isolates container values commonly stored in metadata and tool arguments.
func CloneAny(value any) any {
	if value == nil {
		return nil
	}
	cloned := cloneAnyValue(reflect.ValueOf(value))
	if !cloned.IsValid() {
		return nil
	}
	return cloned.Interface()
}

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
