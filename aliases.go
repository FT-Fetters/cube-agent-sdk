package agent

import (
	"github.com/cubence/cube-agent-sdk/internal/core"
	"github.com/cubence/cube-agent-sdk/internal/schema"
	"github.com/cubence/cube-agent-sdk/internal/skills"
)

type Role = core.Role

const (
	RoleSystem    = core.RoleSystem
	RoleUser      = core.RoleUser
	RoleAssistant = core.RoleAssistant
	RoleTool      = core.RoleTool
)

type Message = core.Message
type Model = core.Model
type StreamModel = core.StreamModel

var (
	ErrStreamingUnsupported          = core.ErrStreamingUnsupported
	ErrStreamingToolCallsUnsupported = core.ErrStreamingToolCallsUnsupported
)

type StreamEventType = core.StreamEventType

const (
	StreamEventDelta = core.StreamEventDelta
	StreamEventDone  = core.StreamEventDone
	StreamEventError = core.StreamEventError
)

type StreamEvent = core.StreamEvent
type ModelRequest = core.ModelRequest
type TokenUsage = core.TokenUsage
type ProviderDiagnostics = core.ProviderDiagnostics
type ProviderDiagnosticsError = core.ProviderDiagnosticsError
type ProviderError = core.ProviderError
type ModelResponse = core.ModelResponse
type Config = core.Config

type CompactConfig = core.CompactConfig
type TokenCounter = core.TokenCounter
type TokenCounterFunc = core.TokenCounterFunc
type ApproxTokenCounter = core.ApproxTokenCounter
type Compactor = core.Compactor
type SummaryCompactor = core.SummaryCompactor
type ModelCompactor = core.ModelCompactor

type MCPTransport = core.MCPTransport

const (
	MCPTransportStdio = core.MCPTransportStdio
	MCPTransportSSE   = core.MCPTransportSSE
	MCPTransportHTTP  = core.MCPTransportHTTP
)

type MCPServerConfig = core.MCPServerConfig

type SchemaType = schema.SchemaType

const (
	SchemaTypeString  = schema.SchemaTypeString
	SchemaTypeNumber  = schema.SchemaTypeNumber
	SchemaTypeInteger = schema.SchemaTypeInteger
	SchemaTypeBoolean = schema.SchemaTypeBoolean
	SchemaTypeObject  = schema.SchemaTypeObject
	SchemaTypeArray   = schema.SchemaTypeArray
)

type ToolParametersSchema = schema.ToolParametersSchema
type ToolValidationError = schema.ToolValidationError

var ErrToolValidation = schema.ErrToolValidation

type ToolDescriptor = core.ToolDescriptor
type ToolRisk = core.ToolRisk

const (
	ToolRiskUnspecified = core.ToolRiskUnspecified
	ToolRiskRead        = core.ToolRiskRead
	ToolRiskWrite       = core.ToolRiskWrite
	ToolRiskDestructive = core.ToolRiskDestructive
)

type ToolCall = core.ToolCall
type ToolResult = core.ToolResult
type Tool = core.Tool
type ToolParametersSchemaProvider = core.ToolParametersSchemaProvider
type ToolRiskProvider = core.ToolRiskProvider
type ToolFunc = core.ToolFunc

type ApprovalRequest = core.ApprovalRequest
type ApprovalDecision = core.ApprovalDecision
type ApprovalPolicy = core.ApprovalPolicy
type ApprovalFunc = core.ApprovalFunc
type AllowAllApproval = core.AllowAllApproval

type ErrorCategory = core.ErrorCategory

const (
	ErrorCategoryModel     = core.ErrorCategoryModel
	ErrorCategoryTool      = core.ErrorCategoryTool
	ErrorCategoryApproval  = core.ErrorCategoryApproval
	ErrorCategorySchema    = core.ErrorCategorySchema
	ErrorCategoryMCP       = core.ErrorCategoryMCP
	ErrorCategoryCompact   = core.ErrorCategoryCompact
	ErrorCategorySubagent  = core.ErrorCategorySubagent
	ErrorCategoryStreaming = core.ErrorCategoryStreaming
	ErrorCategoryHook      = core.ErrorCategoryHook
	ErrorCategoryConfig    = core.ErrorCategoryConfig
)

type AgentError = core.AgentError

var (
	NewProviderError             = core.NewProviderError
	ProviderDiagnosticsFromError = core.ProviderDiagnosticsFromError
)

type EventType = core.EventType

const (
	EventBeforeModel     = core.EventBeforeModel
	EventAfterModel      = core.EventAfterModel
	EventBeforeApproval  = core.EventBeforeApproval
	EventAfterApproval   = core.EventAfterApproval
	EventBeforeTool      = core.EventBeforeTool
	EventAfterTool       = core.EventAfterTool
	EventBeforeCompact   = core.EventBeforeCompact
	EventAfterCompact    = core.EventAfterCompact
	EventSkillActivated  = core.EventSkillActivated
	EventSubagentMessage = core.EventSubagentMessage
)

type Event = core.Event
type Hook = core.Hook
type Observer = core.Observer
type ObserverFunc = core.ObserverFunc
type NoopObserver = core.NoopObserver
type Observation = core.Observation

var ObservationFromEvent = core.ObservationFromEvent

type SkillMatcher = skills.SkillMatcher
type Skill = skills.Skill
