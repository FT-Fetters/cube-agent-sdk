package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var nextAgentID uint64

// Agent manages prompt assembly, context, tools, skills, MCP configuration, and subagents.
type Agent struct {
	// runSlot serializes whole Run and RunStream lifecycles so shared context stays ordered.
	runSlot chan struct{}

	mu sync.Mutex

	id     string
	parent *Agent
	model  Model
	config Config

	messages     []Message
	instructions []instructionFile

	skills       map[string]Skill
	skillOrder   []string
	activeSkills map[string]struct{}

	tools           map[string]Tool
	toolOrder       []string
	toolConcurrency map[string]int

	mcpServers []MCPServerConfig

	approval           ApprovalPolicy
	hooks              []Hook
	observer           Observer
	compactor          Compactor
	tokenCount         TokenCounter
	requestIDGenerator RequestIDGenerator
	runSeq             uint64
	requestSeq         uint64
	subagents          map[string]*Agent
	parentInbox        map[string][]SubagentMessage
}

// Option customizes an Agent during construction.
type Option func(*Agent) error

// RunOption customizes a single Agent run.
type RunOption func(*runConfig)

// RequestIDGenerator builds request correlation IDs from safe lifecycle metadata.
// Returning an empty ID falls back to the SDK's default ID format.
type RequestIDGenerator func(RequestIDContext) string

// RequestIDContext contains safe metadata available to custom request ID generators.
// It intentionally excludes prompts, messages, tool arguments, tool results, and raw errors.
type RequestIDContext struct {
	// AgentID is the agent emitting the lifecycle request.
	AgentID string
	// RunID is the correlation ID for the current Run or RunStream call.
	RunID string
	// TraceID, SpanID, and TraceState come from WithTraceContext when present.
	TraceID    string
	SpanID     string
	TraceState string
	// EventType is the first lifecycle event that will use the generated ID.
	EventType EventType
	// Operation describes the logical request, such as model.generate or tool.call.
	Operation string
	// Sequence is the SDK-local request sequence for default-compatible ordering.
	Sequence uint64
	// Round is the model/tool round when the request belongs to a run loop.
	Round int
	// ParentRequestID links nested requests to the request that caused them.
	ParentRequestID string
	// ToolName is set for tool and approval request IDs.
	ToolName string
	// SubagentID is set for subagent lifecycle request IDs.
	SubagentID string
}

type runConfig struct {
	skillNames             []string
	runID                  string
	observeStreamLifecycle bool
}

const (
	requestOperationModelGenerate     = "model.generate"
	requestOperationModelStream       = "model.stream"
	requestOperationStreamUnsupported = "stream.unsupported"
	requestOperationToolCall          = "tool.call"
	requestOperationCompactContext    = "compact.context"
	requestOperationSubagentSpawn     = "subagent.spawn"
	requestOperationSubagentRun       = "subagent.run"
	requestOperationSubagentLookup    = "subagent.lookup"
	requestOperationSubagentParent    = "subagent.parent"
)

type runIDContextKey struct{}
type activeRunAgentContextKey struct{}

var errAgentRunActive = errors.New("agent: run already active")

// New constructs an Agent with the provided model and optional capabilities.
func New(config Config, model Model, options ...Option) (*Agent, error) {
	if model == nil {
		return nil, errors.New("agent: model is required")
	}
	if config.ID == "" {
		config.ID = fmt.Sprintf("agent-%d", atomic.AddUint64(&nextAgentID, 1))
	}
	if config.MaxToolRounds == 0 {
		config.MaxToolRounds = 4
	}

	agent := &Agent{
		id:              config.ID,
		runSlot:         make(chan struct{}, 1),
		model:           model,
		config:          config,
		skills:          make(map[string]Skill),
		activeSkills:    make(map[string]struct{}),
		tools:           make(map[string]Tool),
		toolConcurrency: make(map[string]int),
		approval:        AllowAllApproval{},
		observer:        NoopObserver{},
		tokenCount:      ApproxTokenCounter{},
		subagents:       make(map[string]*Agent),
		parentInbox:     make(map[string][]SubagentMessage),
	}
	if config.Compact.MaxTokens > 0 {
		agent.compactor = SummaryCompactor{KeepLast: config.Compact.KeepLast}
	}

	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(agent); err != nil {
			return nil, err
		}
	}
	return agent, nil
}

// ID returns the stable identifier for the agent.
func (a *Agent) ID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id
}

// WithInstructionFiles loads additional instruction files into the system prompt.
func WithInstructionFiles(paths ...string) Option {
	return func(agent *Agent) error {
		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("agent: read instruction file %q: %w", path, err)
			}
			agent.instructions = append(agent.instructions, instructionFile{
				path:    path,
				content: strings.TrimSpace(string(content)),
			})
		}
		return nil
	}
}

// WithSkills makes the provided skills available for explicit or implicit activation.
func WithSkills(skills ...Skill) Option {
	return func(agent *Agent) error {
		for _, skill := range skills {
			if err := agent.registerSkill(skill); err != nil {
				return err
			}
		}
		return nil
	}
}

// WithMCPServers configures MCP servers exposed to model requests.
func WithMCPServers(servers ...MCPServerConfig) Option {
	return func(agent *Agent) error {
		agent.mcpServers = append(agent.mcpServers, cloneMCPServers(servers)...)
		return nil
	}
}

// WithTools makes tools callable by model tool requests.
func WithTools(tools ...Tool) Option {
	return func(agent *Agent) error {
		for _, tool := range tools {
			if tool == nil {
				return errors.New("agent: nil tool")
			}
			name := strings.TrimSpace(tool.Name())
			if name == "" {
				return errors.New("agent: tool name is required")
			}
			if _, exists := agent.tools[name]; !exists {
				agent.toolOrder = append(agent.toolOrder, name)
			}
			agent.tools[name] = tool
		}
		return nil
	}
}

// WithApprovalPolicy sets the approval policy used before every tool call.
func WithApprovalPolicy(policy ApprovalPolicy) Option {
	return func(agent *Agent) error {
		if policy == nil {
			return errors.New("agent: approval policy is nil")
		}
		agent.approval = policy
		return nil
	}
}

// WithHook registers an event hook.
func WithHook(hook Hook) Option {
	return func(agent *Agent) error {
		if hook == nil {
			return errors.New("agent: hook is nil")
		}
		agent.hooks = append(agent.hooks, hook)
		return nil
	}
}

// WithObserver installs a no-fail telemetry observer for sanitized lifecycle metadata.
func WithObserver(observer Observer) Option {
	return func(agent *Agent) error {
		if observer == nil {
			return errors.New("agent: observer is nil")
		}
		agent.observer = observer
		return nil
	}
}

// WithRequestIDGenerator installs a custom request correlation ID generator.
// The generator receives only sanitized lifecycle metadata. If it returns an
// empty ID or panics, the SDK uses its default request ID for that request.
// Custom IDs are not de-duplicated; generators should enforce any uniqueness
// requirement required by the application.
func WithRequestIDGenerator(generator RequestIDGenerator) Option {
	return func(agent *Agent) error {
		if generator == nil {
			return errors.New("agent: request ID generator is nil")
		}
		agent.requestIDGenerator = generator
		return nil
	}
}

// WithCompactor sets the compactor used when context exceeds the configured threshold.
func WithCompactor(compactor Compactor) Option {
	return func(agent *Agent) error {
		if compactor == nil {
			return errors.New("agent: compactor is nil")
		}
		agent.compactor = compactor
		return nil
	}
}

// WithTokenCounter sets the token counter used for auto compact threshold checks.
func WithTokenCounter(counter TokenCounter) Option {
	return func(agent *Agent) error {
		if counter == nil {
			return errors.New("agent: token counter is nil")
		}
		agent.tokenCount = counter
		return nil
	}
}

// WithRunSkills explicitly activates skills for a single Run call.
func WithRunSkills(names ...string) RunOption {
	return func(config *runConfig) {
		config.skillNames = append(config.skillNames, names...)
	}
}

// WithRunID sets a caller-provided correlation ID for a single Run or RunStream call.
func WithRunID(id string) RunOption {
	return func(config *runConfig) {
		config.runID = strings.TrimSpace(id)
	}
}

// WithStreamObservations enables sanitized stream lifecycle observations for a
// single RunStream call. It emits stream start, first delta, done, and error
// observations without logging delta text or message content.
func WithStreamObservations() RunOption {
	return func(config *runConfig) {
		config.observeStreamLifecycle = true
	}
}

// AppendMessage adds an existing message to the managed context.
func (a *Agent) AppendMessage(message Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, cloneMessage(message))
}

// Messages returns a snapshot of the managed conversation context.
func (a *Agent) Messages() []Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneMessages(a.messages)
}

// ActiveSkills returns persistently activated skills.
func (a *Agent) ActiveSkills() []Skill {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeSkillsLocked()
}

// ActivateSkill persistently activates a skill for future runs.
func (a *Agent) ActivateSkill(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	name = strings.TrimSpace(name)
	if _, ok := a.skills[name]; !ok {
		return fmt.Errorf("agent: unknown skill %q", name)
	}
	a.activeSkills[name] = struct{}{}
	return nil
}

// DeactivateSkill removes a persistent skill activation.
func (a *Agent) DeactivateSkill(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.activeSkills, name)
}

// HasSkill reports whether the agent can activate a named skill.
func (a *Agent) HasSkill(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.skills[name]
	return ok
}

// HasTool reports whether the agent can execute a named tool.
func (a *Agent) HasTool(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.tools[name]
	return ok
}

// MCPServers returns a snapshot of configured MCP servers.
func (a *Agent) MCPServers() []MCPServerConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneMCPServers(a.mcpServers)
}

// Run appends user input to context, resolves skills, compacts if needed, and calls the model.
func (a *Agent) Run(ctx context.Context, input string, options ...RunOption) (Message, error) {
	var config runConfig
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	ctx = withRunID(ctx, a.runID(config))
	ctx, releaseRun, err := a.acquireRun(ctx)
	if err != nil {
		return Message{}, err
	}
	defer releaseRun()

	if err := a.checkModelCapabilities(ctx, false); err != nil {
		return Message{}, err
	}

	a.mu.Lock()
	a.messages = append(a.messages, Message{Role: RoleUser, Content: input})
	a.mu.Unlock()

	activeSkills, err := a.resolveActiveSkills(input, config.skillNames)
	if err != nil {
		return Message{}, err
	}
	for _, skill := range activeSkills {
		if err := a.emit(ctx, Event{Type: EventSkillActivated, SkillName: skill.Name}); err != nil {
			return Message{}, err
		}
	}
	if err := a.maybeCompact(ctx, 1, ""); err != nil {
		return Message{}, err
	}

	maxRounds := a.maxToolRounds()
	var previousModelRequestID string
	for round := 0; ; round++ {
		roundNumber := round + 1
		request := a.buildModelRequest(activeSkills)
		parentRequestID := previousModelRequestID
		requestID := a.nextRequestID(ctx, RequestIDContext{
			EventType:       EventBeforeModel,
			Operation:       requestOperationModelGenerate,
			Round:           roundNumber,
			ParentRequestID: parentRequestID,
		})
		estimatedTokens := a.estimatedTokens(request.Messages)
		if err := a.emit(ctx, Event{
			Type:            EventBeforeModel,
			RequestID:       requestID,
			ParentRequestID: parentRequestID,
			Round:           roundNumber,
			EstimatedTokens: estimatedTokens,
		}); err != nil {
			return Message{}, err
		}
		started := time.Now()
		response, err := a.model.Generate(ctx, request)
		duration := eventDurationSince(started)
		if err != nil {
			wrapped := agentError(ErrorCategoryModel, "model.generate", err)
			wrapped.AgentID = request.AgentID
			wrapped.RequestID = requestID
			wrapped.ParentRequestID = parentRequestID
			wrapped.Round = roundNumber
			if emitErr := a.emit(ctx, Event{
				Type:            EventAfterModel,
				RequestID:       requestID,
				ParentRequestID: parentRequestID,
				Round:           roundNumber,
				Duration:        duration,
				EstimatedTokens: estimatedTokens,
				Error:           wrapped,
			}); emitErr != nil {
				return Message{}, emitErr
			}
			return Message{}, wrapped
		}
		if err := a.emit(ctx, Event{
			Type:            EventAfterModel,
			RequestID:       requestID,
			ParentRequestID: parentRequestID,
			Round:           roundNumber,
			Duration:        duration,
			EstimatedTokens: estimatedTokens,
			TokenUsage:      response.Usage,
			Message:         response.Message,
		}); err != nil {
			return Message{}, err
		}

		if len(response.ToolCalls) == 0 {
			message := response.Message
			if message.Role == "" {
				message.Role = RoleAssistant
			}
			a.appendMessage(message)
			return cloneMessage(message), nil
		}
		if round >= maxRounds {
			wrapped := agentError(ErrorCategoryTool, "tool.rounds", ErrMaxToolRoundsExceeded)
			wrapped.AgentID = request.AgentID
			wrapped.RunID = runIDFromContext(ctx)
			setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
			wrapped.RequestID = requestID
			wrapped.ParentRequestID = parentRequestID
			wrapped.Round = roundNumber
			return Message{}, wrapped
		}
		assistantMessage := response.Message
		if assistantMessage.Role == "" {
			assistantMessage.Role = RoleAssistant
		}
		assistantMessage.ToolCalls = cloneToolCalls(response.ToolCalls)
		a.appendMessage(assistantMessage)

		for _, call := range response.ToolCalls {
			result, err := a.executeTool(ctx, call, roundNumber, requestID)
			if err != nil {
				return Message{}, err
			}
			a.appendMessage(Message{
				Role:       RoleTool,
				Name:       result.Name,
				ToolCallID: result.CallID,
				Content:    result.Content,
				Metadata:   result.Metadata,
			})
		}
		if err := a.maybeCompact(ctx, roundNumber, requestID); err != nil {
			return Message{}, err
		}
		previousModelRequestID = requestID
	}
}

// RunStream starts a streaming model call and returns events as the model emits
// them. Callers must either drain the returned channel or cancel the context.
// The final assistant message is written only after a done event is forwarded.
func (a *Agent) RunStream(ctx context.Context, input string, options ...RunOption) (<-chan StreamEvent, error) {
	var config runConfig
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	ctx = withRunID(ctx, a.runID(config))
	ctx, releaseRun, err := a.acquireRun(ctx)
	if err != nil {
		return nil, err
	}
	releaseRunOnReturn := true
	defer func() {
		if releaseRunOnReturn {
			releaseRun()
		}
	}()
	started := time.Now()
	if err := a.checkModelCapabilities(ctx, true); err != nil {
		return nil, err
	}

	streamModel, ok := a.model.(StreamModel)
	if !ok {
		cause := fmt.Errorf("%w: %T", ErrStreamingUnsupported, a.model)
		wrapped := agentError(ErrorCategoryStreaming, "stream.unsupported", cause)
		wrapped.AgentID = a.agentID()
		wrapped.RunID = runIDFromContext(ctx)
		setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
		wrapped.Round = 1
		wrapped.RequestID = a.nextRequestID(ctx, RequestIDContext{
			EventType: EventAfterModel,
			Operation: requestOperationStreamUnsupported,
			Round:     wrapped.Round,
		})
		estimatedTokens := a.estimatedInputTokens(input)
		duration := eventDurationSince(started)
		// Unsupported streaming returns before normal model hooks can run, so
		// record the failure through observer-only telemetry without mutating context.
		a.observeEvent(ctx, Event{
			Type:            EventAfterModel,
			RequestID:       wrapped.RequestID,
			Round:           1,
			Duration:        duration,
			EstimatedTokens: estimatedTokens,
			Error:           wrapped,
		})
		a.observeStreamLifecycle(ctx, config.observeStreamLifecycle, Event{
			Type:            EventStreamError,
			RequestID:       wrapped.RequestID,
			Round:           1,
			Duration:        duration,
			EstimatedTokens: estimatedTokens,
			Error:           wrapped,
		})
		return nil, wrapped
	}

	a.mu.Lock()
	a.messages = append(a.messages, Message{Role: RoleUser, Content: input})
	a.mu.Unlock()

	activeSkills, err := a.resolveActiveSkills(input, config.skillNames)
	if err != nil {
		return nil, err
	}
	for _, skill := range activeSkills {
		if err := a.emit(ctx, Event{Type: EventSkillActivated, SkillName: skill.Name}); err != nil {
			return nil, err
		}
	}
	if err := a.maybeCompact(ctx, 1, ""); err != nil {
		return nil, err
	}

	// A child context lets the forwarding goroutine release provider streams when
	// the caller cancels or when the agent stops consuming a provider stream early.
	streamCtx, cancelStream := context.WithCancel(ctx)
	firstRound, _, err := a.startStreamRound(streamCtx, streamModel, activeSkills, 1, "", config.observeStreamLifecycle)
	if err != nil {
		cancelStream()
		return nil, err
	}

	out := make(chan StreamEvent)
	releaseRunOnReturn = false
	go a.forwardStreamEvents(streamCtx, cancelStream, streamModel, activeSkills, firstRound, out, config.observeStreamLifecycle, releaseRun)
	return out, nil
}

type activeStreamRound struct {
	events          <-chan StreamEvent
	agentID         string
	requestID       string
	parentRequestID string
	round           int
	estimatedTokens int
	started         time.Time
}

type streamRoundOutcome struct {
	message   Message
	toolCalls []ToolCall
	telemetry streamTelemetryTracker
}

func (a *Agent) startStreamRound(ctx context.Context, streamModel StreamModel, activeSkills []Skill, round int, parentRequestID string, observeStreamLifecycle bool) (activeStreamRound, bool, error) {
	request := a.buildModelRequest(activeSkills)
	requestID := a.nextRequestID(ctx, RequestIDContext{
		EventType:       EventBeforeModel,
		Operation:       requestOperationModelStream,
		Round:           round,
		ParentRequestID: parentRequestID,
	})
	estimatedTokens := a.estimatedTokens(request.Messages)
	state := activeStreamRound{
		agentID:         request.AgentID,
		requestID:       requestID,
		parentRequestID: parentRequestID,
		round:           round,
		estimatedTokens: estimatedTokens,
		started:         time.Now(),
	}
	if err := a.emit(ctx, Event{
		Type:            EventBeforeModel,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		EstimatedTokens: estimatedTokens,
	}); err != nil {
		return state, false, err
	}
	a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
		Type:            EventStreamStart,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		EstimatedTokens: estimatedTokens,
	})
	modelEvents, err := streamModel.Stream(ctx, request)
	if err != nil {
		return state, true, a.handleStreamStartError(ctx, state, err, observeStreamLifecycle)
	}
	if modelEvents == nil {
		err := errors.New("agent: stream model returned nil event channel")
		return state, true, a.handleStreamStartError(ctx, state, err, observeStreamLifecycle)
	}
	state.events = modelEvents
	return state, false, nil
}

func (a *Agent) handleStreamStartError(ctx context.Context, state activeStreamRound, err error, observeStreamLifecycle bool) error {
	wrapped := agentError(ErrorCategoryModel, "model.stream", err)
	wrapped.AgentID = state.agentID
	wrapped.RequestID = state.requestID
	wrapped.ParentRequestID = state.parentRequestID
	wrapped.Round = state.round
	afterEvent := Event{
		Type:            EventAfterModel,
		RequestID:       state.requestID,
		ParentRequestID: state.parentRequestID,
		Round:           state.round,
		Duration:        eventDurationSince(state.started),
		EstimatedTokens: state.estimatedTokens,
		Error:           wrapped,
	}
	if emitErr := a.emitStreamAfterModel(ctx, afterEvent); emitErr != nil {
		a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
			Type:            EventStreamError,
			RequestID:       state.requestID,
			ParentRequestID: state.parentRequestID,
			Round:           state.round,
			Duration:        afterEvent.Duration,
			EstimatedTokens: state.estimatedTokens,
			Error:           emitErr,
		})
		return emitErr
	}
	a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
		Type:            EventStreamError,
		RequestID:       state.requestID,
		ParentRequestID: state.parentRequestID,
		Round:           state.round,
		Duration:        afterEvent.Duration,
		EstimatedTokens: state.estimatedTokens,
		Error:           wrapped,
	})
	return wrapped
}

func (a *Agent) forwardStreamEvents(ctx context.Context, cancelStream context.CancelFunc, streamModel StreamModel, activeSkills []Skill, firstRound activeStreamRound, out chan<- StreamEvent, observeStreamLifecycle bool, releaseRun func()) {
	defer func() {
		cancelStream()
		close(out)
		if releaseRun != nil {
			releaseRun()
		}
	}()

	agentID := a.agentID()
	maxRounds := a.maxToolRounds()
	round := firstRound
	for {
		outcome, ok := a.forwardStreamRound(ctx, round, out, observeStreamLifecycle)
		if !ok || len(outcome.toolCalls) == 0 {
			return
		}
		if round.round > maxRounds {
			err := a.streamMaxToolRoundsError(ctx, agentID, round)
			a.sendStreamRuntimeError(ctx, out, agentID, err, round, outcome.telemetry, observeStreamLifecycle)
			return
		}

		// A tool-call done event is a control point, not the user's final streamed answer.
		if ctx.Err() != nil {
			return
		}
		a.appendMessage(outcome.message)
		for _, call := range outcome.toolCalls {
			result, err := a.executeTool(ctx, call, round.round, round.requestID)
			if err != nil {
				a.sendStreamRuntimeError(ctx, out, agentID, err, round, outcome.telemetry, observeStreamLifecycle)
				return
			}
			if ctx.Err() != nil {
				return
			}
			a.appendMessage(Message{
				Role:       RoleTool,
				Name:       result.Name,
				ToolCallID: result.CallID,
				Content:    result.Content,
				Metadata:   result.Metadata,
			})
		}
		if err := a.maybeCompact(ctx, round.round, round.requestID); err != nil {
			a.sendStreamRuntimeError(ctx, out, agentID, err, round, outcome.telemetry, observeStreamLifecycle)
			return
		}

		nextRound, errorObserved, err := a.startStreamRound(ctx, streamModel, activeSkills, round.round+1, round.requestID, observeStreamLifecycle)
		if err != nil {
			if errorObserved {
				_ = sendStreamErrorEvent(ctx, out, agentID, err)
				return
			}
			a.sendStreamRuntimeError(ctx, out, agentID, err, nextRound, streamTelemetryTracker{started: nextRound.started}, observeStreamLifecycle)
			return
		}
		round = nextRound
	}
}

func (a *Agent) forwardStreamRound(ctx context.Context, state activeStreamRound, out chan<- StreamEvent, observeStreamLifecycle bool) (streamRoundOutcome, bool) {
	var content strings.Builder
	telemetry := streamTelemetryTracker{started: state.started}
	for event := range state.events {
		event.AgentID = state.agentID

		switch event.Type {
		case StreamEventDelta:
			if len(event.Message.ToolCalls) > 0 {
				a.sendStreamError(ctx, out, state.agentID, ErrStreamingToolCallsUnsupported, state.requestID, state.parentRequestID, state.round, state.estimatedTokens, state.started, telemetry, observeStreamLifecycle)
				return streamRoundOutcome{}, false
			}
			telemetry.recordDelta(event.Delta)
			if telemetry.deltaCount == 1 {
				duration := eventDurationSince(state.started)
				a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
					Type:            EventStreamFirstDelta,
					RequestID:       state.requestID,
					ParentRequestID: state.parentRequestID,
					Round:           state.round,
					Duration:        duration,
					EstimatedTokens: state.estimatedTokens,
					StreamTelemetry: telemetry.telemetry(duration),
				})
			}
			content.WriteString(event.Delta)
			if !sendStreamEvent(ctx, out, event) {
				return streamRoundOutcome{}, false
			}
		case StreamEventThinkingDelta:
			// Thinking deltas are caller-visible provider reasoning text, but they are
			// not part of the assistant message committed to conversation context.
			if !sendStreamEvent(ctx, out, StreamEvent{Type: event.Type, AgentID: event.AgentID, Delta: event.Delta}) {
				return streamRoundOutcome{}, false
			}
		case StreamEventDone:
			message := event.Message
			if message.Role == "" {
				message.Role = RoleAssistant
			}
			if message.Content == "" {
				message.Content = content.String()
			}
			message.ToolCalls = cloneToolCalls(message.ToolCalls)
			event.Message = cloneMessage(message)

			duration := eventDurationSince(state.started)
			if err := a.emitStreamAfterModel(ctx, Event{
				Type:            EventAfterModel,
				RequestID:       state.requestID,
				ParentRequestID: state.parentRequestID,
				Round:           state.round,
				Duration:        duration,
				EstimatedTokens: state.estimatedTokens,
				TokenUsage:      event.Usage,
				StreamTelemetry: telemetry.telemetry(duration),
				Message:         message,
			}); err != nil {
				a.sendStreamError(ctx, out, state.agentID, err, state.requestID, state.parentRequestID, state.round, state.estimatedTokens, state.started, telemetry, observeStreamLifecycle)
				return streamRoundOutcome{}, false
			}
			a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
				Type:            EventStreamDone,
				RequestID:       state.requestID,
				ParentRequestID: state.parentRequestID,
				Round:           state.round,
				Duration:        duration,
				EstimatedTokens: state.estimatedTokens,
				TokenUsage:      event.Usage,
				StreamTelemetry: telemetry.telemetry(duration),
			})
			if ctx.Err() != nil {
				return streamRoundOutcome{}, false
			}
			if len(message.ToolCalls) > 0 {
				return streamRoundOutcome{message: message, toolCalls: cloneToolCalls(message.ToolCalls), telemetry: telemetry}, true
			}
			if !sendStreamEvent(ctx, out, event) {
				return streamRoundOutcome{}, false
			}
			// Commit only after the caller receives final done so cancellation cannot persist an abandoned answer.
			a.appendMessage(message)
			return streamRoundOutcome{message: message, telemetry: telemetry}, true
		case StreamEventToolCallStart, StreamEventToolCallDone:
			// Boundary events are safe UI metadata only; discard any accidental
			// payload fields from custom stream models before forwarding.
			if !sendStreamEvent(ctx, out, sanitizeStreamToolCallBoundary(event)) {
				return streamRoundOutcome{}, false
			}
		case StreamEventError:
			if event.Error == nil {
				event.Error = errors.New("agent: stream error")
			}
			wrapped := agentError(ErrorCategoryModel, "model.stream", event.Error)
			wrapped.AgentID = state.agentID
			wrapped.RequestID = state.requestID
			wrapped.ParentRequestID = state.parentRequestID
			wrapped.Round = state.round
			event.Error = wrapped
			duration := eventDurationSince(state.started)
			if emitErr := a.emitStreamAfterModel(ctx, Event{
				Type:            EventAfterModel,
				RequestID:       state.requestID,
				ParentRequestID: state.parentRequestID,
				Round:           state.round,
				Duration:        duration,
				EstimatedTokens: state.estimatedTokens,
				StreamTelemetry: telemetry.telemetry(duration),
				Error:           wrapped,
			}); emitErr != nil {
				event.Error = emitErr
			}
			a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
				Type:            EventStreamError,
				RequestID:       state.requestID,
				ParentRequestID: state.parentRequestID,
				Round:           state.round,
				Duration:        duration,
				EstimatedTokens: state.estimatedTokens,
				StreamTelemetry: telemetry.telemetry(duration),
				Error:           event.Error,
			})
			_ = sendStreamEvent(ctx, out, event)
			return streamRoundOutcome{}, false
		default:
			err := fmt.Errorf("agent: unknown stream event type %q", event.Type)
			a.sendStreamError(ctx, out, state.agentID, err, state.requestID, state.parentRequestID, state.round, state.estimatedTokens, state.started, telemetry, observeStreamLifecycle)
			return streamRoundOutcome{}, false
		}
	}

	a.sendStreamError(ctx, out, state.agentID, errors.New("agent: stream ended without done event"), state.requestID, state.parentRequestID, state.round, state.estimatedTokens, state.started, telemetry, observeStreamLifecycle)
	return streamRoundOutcome{}, false
}

func (a *Agent) streamMaxToolRoundsError(ctx context.Context, agentID string, state activeStreamRound) error {
	wrapped := agentError(ErrorCategoryTool, "tool.rounds", ErrMaxToolRoundsExceeded)
	wrapped.AgentID = agentID
	wrapped.RunID = runIDFromContext(ctx)
	setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
	wrapped.RequestID = state.requestID
	wrapped.ParentRequestID = state.parentRequestID
	wrapped.Round = state.round
	return wrapped
}

func (a *Agent) sendStreamRuntimeError(ctx context.Context, out chan<- StreamEvent, agentID string, err error, state activeStreamRound, telemetry streamTelemetryTracker, observeStreamLifecycle bool) {
	err = streamAgentError(err)
	runID := runIDFromContext(ctx)
	requestID := state.requestID
	parentRequestID := state.parentRequestID
	round := state.round
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		if agentErr.AgentID == "" {
			agentErr.AgentID = agentID
		}
		if agentErr.RunID == "" {
			agentErr.RunID = runID
		}
		setAgentErrorTraceContext(agentErr, traceContextFromContext(ctx))
		if agentErr.RequestID == "" {
			agentErr.RequestID = requestID
		}
		if agentErr.ParentRequestID == "" {
			agentErr.ParentRequestID = parentRequestID
		}
		if agentErr.Round == 0 {
			agentErr.Round = round
		}
		requestID = agentErr.RequestID
		parentRequestID = agentErr.ParentRequestID
		round = agentErr.Round
	}
	duration := eventDurationSince(state.started)
	a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
		Type:            EventStreamError,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		Duration:        duration,
		EstimatedTokens: state.estimatedTokens,
		StreamTelemetry: telemetry.telemetry(duration),
		Error:           err,
	})
	_ = sendStreamErrorEvent(ctx, out, agentID, err)
}

func (a *Agent) sendStreamError(ctx context.Context, out chan<- StreamEvent, agentID string, err error, requestID string, parentRequestID string, round int, estimatedTokens int, started time.Time, telemetry streamTelemetryTracker, observeStreamLifecycle bool) {
	err = streamAgentError(err)
	runID := runIDFromContext(ctx)
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		if agentErr.AgentID == "" {
			agentErr.AgentID = agentID
		}
		if agentErr.RunID == "" {
			agentErr.RunID = runID
		}
		setAgentErrorTraceContext(agentErr, traceContextFromContext(ctx))
		if agentErr.RequestID == "" {
			agentErr.RequestID = requestID
		}
		if agentErr.ParentRequestID == "" {
			agentErr.ParentRequestID = parentRequestID
		}
		if agentErr.Round == 0 {
			agentErr.Round = round
		}
	}
	if classifyError(err) == ErrorCategoryHook {
		duration := eventDurationSince(started)
		a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
			Type:            EventStreamError,
			RequestID:       requestID,
			ParentRequestID: parentRequestID,
			Round:           round,
			Duration:        duration,
			EstimatedTokens: estimatedTokens,
			StreamTelemetry: telemetry.telemetry(duration),
			Error:           err,
		})
		_ = sendStreamErrorEvent(ctx, out, agentID, err)
		return
	}
	duration := eventDurationSince(started)
	afterEvent := Event{
		Type:            EventAfterModel,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		Duration:        duration,
		EstimatedTokens: estimatedTokens,
		StreamTelemetry: telemetry.telemetry(duration),
		Error:           err,
	}
	if emitErr := a.emitStreamAfterModel(ctx, afterEvent); emitErr != nil {
		err = emitErr
	}
	a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
		Type:            EventStreamError,
		RequestID:       requestID,
		ParentRequestID: parentRequestID,
		Round:           round,
		Duration:        duration,
		EstimatedTokens: estimatedTokens,
		StreamTelemetry: telemetry.telemetry(duration),
		Error:           err,
	})
	_ = sendStreamErrorEvent(ctx, out, agentID, err)
}

func sanitizeStreamToolCallBoundary(event StreamEvent) StreamEvent {
	return StreamEvent{Type: event.Type, AgentID: event.AgentID, ToolCall: event.ToolCall}
}

func sendStreamErrorEvent(ctx context.Context, out chan<- StreamEvent, agentID string, err error) bool {
	return sendStreamEvent(ctx, out, StreamEvent{
		Type:    StreamEventError,
		AgentID: agentID,
		Error:   err,
	})
}

type streamTelemetryTracker struct {
	started          time.Time
	timeToFirstToken time.Duration
	deltaCount       int
	byteCount        int
}

func (t *streamTelemetryTracker) recordDelta(delta string) {
	if t.deltaCount == 0 {
		t.timeToFirstToken = eventDurationSince(t.started)
	}
	t.deltaCount++
	t.byteCount += len(delta)
}

func (t streamTelemetryTracker) telemetry(duration time.Duration) StreamTelemetry {
	if t.deltaCount == 0 {
		return StreamTelemetry{}
	}
	streamTelemetry := StreamTelemetry{
		TimeToFirstToken: t.timeToFirstToken,
		DeltaCount:       t.deltaCount,
		ByteCount:        t.byteCount,
	}
	if duration > 0 && t.byteCount > 0 {
		streamTelemetry.ThroughputBytesPerSecond = float64(t.byteCount) / duration.Seconds()
	}
	return streamTelemetry
}

func streamAgentError(err error) error {
	if err == nil {
		err = errors.New("agent: stream error")
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return err
	}
	category := classifyError(err)
	if category == "" {
		category = ErrorCategoryStreaming
	}
	operation := "stream.forward"
	if errors.Is(err, ErrStreamingToolCallsUnsupported) {
		operation = "stream.tool_calls"
	}
	return agentError(category, operation, err)
}

func sendStreamEvent(ctx context.Context, out chan<- StreamEvent, event StreamEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- event:
		return true
	}
}

func (a *Agent) registerSkill(skill Skill) error {
	skill.Name = strings.TrimSpace(skill.Name)
	if skill.Name == "" {
		return errors.New("agent: skill name is required")
	}
	if _, exists := a.skills[skill.Name]; !exists {
		a.skillOrder = append(a.skillOrder, skill.Name)
	}
	a.skills[skill.Name] = cloneSkill(skill)
	return nil
}

func (a *Agent) appendMessage(message Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, cloneMessage(message))
}

func (a *Agent) activeSkillsLocked() []Skill {
	var names []string
	for _, name := range a.skillOrder {
		if _, active := a.activeSkills[name]; active {
			names = append(names, name)
		}
	}
	skills := make([]Skill, 0, len(names))
	for _, name := range names {
		skills = append(skills, cloneSkill(a.skills[name]))
	}
	return skills
}

func (a *Agent) resolveActiveSkills(input string, explicit []string) ([]Skill, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var names []string
	seen := make(map[string]struct{})
	add := func(name string) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil
		}
		if _, exists := seen[name]; exists {
			return nil
		}
		skill, ok := a.skills[name]
		if !ok {
			return fmt.Errorf("agent: unknown skill %q", name)
		}
		names = append(names, skill.Name)
		seen[skill.Name] = struct{}{}
		return nil
	}

	for _, name := range a.skillOrder {
		if _, active := a.activeSkills[name]; active {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range explicit {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	for _, name := range inlineSkillNames(input) {
		if _, ok := a.skills[name]; ok {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range a.skillOrder {
		skill := a.skills[name]
		if _, exists := seen[name]; exists {
			continue
		}
		if skillMatches(skill, input) {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}

	skills := make([]Skill, 0, len(names))
	for _, name := range names {
		skill := cloneSkill(a.skills[name])
		skills = append(skills, skill)
	}
	return skills, nil
}

func (a *Agent) buildModelRequest(activeSkills []Skill) ModelRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return ModelRequest{
		AgentID:      a.id,
		SystemPrompt: a.systemPromptLocked(activeSkills),
		Messages:     cloneMessages(a.messages),
		Tools:        a.toolDescriptorsLocked(),
		MCPServers:   cloneMCPServers(a.mcpServers),
		ActiveSkills: cloneSkills(activeSkills),
	}
}

func (a *Agent) systemPromptLocked(activeSkills []Skill) string {
	var sections []string
	if strings.TrimSpace(a.config.SystemPrompt) != "" {
		sections = append(sections, strings.TrimSpace(a.config.SystemPrompt))
	}
	for _, instruction := range a.instructions {
		if instruction.content == "" {
			continue
		}
		sections = append(sections, "Additional instructions:\n"+instruction.content)
	}
	if len(activeSkills) > 0 {
		var builder strings.Builder
		builder.WriteString("Active skills:")
		for _, skill := range activeSkills {
			builder.WriteString("\n\n## ")
			builder.WriteString(skill.Name)
			if skill.Description != "" {
				builder.WriteString("\n")
				builder.WriteString(skill.Description)
			}
			if skill.Instructions != "" {
				builder.WriteString("\n")
				builder.WriteString(skill.Instructions)
			}
		}
		sections = append(sections, builder.String())
	}
	return strings.Join(sections, "\n\n")
}

func (a *Agent) toolDescriptorsLocked() []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0, len(a.toolOrder))
	for _, name := range a.toolOrder {
		tool := a.tools[name]
		descriptors = append(descriptors, ToolDescriptor{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  toolParametersSchema(tool),
			Risk:        toolRisk(tool),
		})
	}
	return descriptors
}

func (a *Agent) maxToolRounds() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.config.MaxToolRounds
}

func (a *Agent) agentID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id
}

func (a *Agent) nextRequestID(ctx context.Context, metadata RequestIDContext) string {
	sequence := atomic.AddUint64(&a.requestSeq, 1)
	a.mu.Lock()
	agentID := a.id
	generator := a.requestIDGenerator
	a.mu.Unlock()

	defaultID := defaultRequestID(agentID, sequence)
	if generator == nil {
		return defaultID
	}
	metadata = normalizeRequestIDContext(ctx, metadata, agentID, sequence)
	return generatedRequestID(generator, metadata, defaultID)
}

func defaultRequestID(agentID string, sequence uint64) string {
	return fmt.Sprintf("%s-request-%d", agentID, sequence)
}

func normalizeRequestIDContext(ctx context.Context, metadata RequestIDContext, agentID string, sequence uint64) RequestIDContext {
	if metadata.AgentID == "" {
		metadata.AgentID = agentID
	}
	if metadata.RunID == "" {
		metadata.RunID = runIDFromContext(ctx)
	}
	trace := traceContextFromContext(ctx)
	if metadata.TraceID == "" {
		metadata.TraceID = trace.TraceID
	}
	if metadata.SpanID == "" {
		metadata.SpanID = trace.SpanID
	}
	if metadata.TraceState == "" {
		metadata.TraceState = trace.TraceState
	}
	metadata.Sequence = sequence
	return metadata
}

func generatedRequestID(generator RequestIDGenerator, metadata RequestIDContext, defaultID string) (requestID string) {
	defer func() {
		if recover() != nil {
			requestID = defaultID
		}
	}()
	requestID = strings.TrimSpace(generator(metadata))
	if requestID == "" {
		return defaultID
	}
	return requestID
}

func (a *Agent) runID(config runConfig) string {
	if config.runID != "" {
		return config.runID
	}
	sequence := atomic.AddUint64(&a.runSeq, 1)
	return fmt.Sprintf("%s-run-%d", a.agentID(), sequence)
}

func (a *Agent) acquireRun(ctx context.Context) (context.Context, func(), error) {
	if activeRunAgentInContext(ctx, a) {
		return ctx, nil, a.runSlotError(ctx, "run.active", errAgentRunActive)
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, a.runSlotError(ctx, "run.acquire", err)
	}

	select {
	case a.runSlot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-a.runSlot
			return ctx, nil, a.runSlotError(ctx, "run.acquire", err)
		}
		return withActiveRunAgent(ctx, a), func() { <-a.runSlot }, nil
	case <-ctx.Done():
		return ctx, nil, a.runSlotError(ctx, "run.acquire", ctx.Err())
	}
}

func (a *Agent) runSlotError(ctx context.Context, operation string, cause error) error {
	wrapped := agentError(ErrorCategoryConfig, operation, cause)
	wrapped.AgentID = a.agentID()
	wrapped.RunID = runIDFromContext(ctx)
	setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
	return wrapped
}

func withActiveRunAgent(ctx context.Context, agent *Agent) context.Context {
	activeAgents := activeRunAgentsFromContext(ctx)
	activeAgents = append(activeAgents, agent)
	return context.WithValue(ctx, activeRunAgentContextKey{}, activeAgents)
}

func activeRunAgentInContext(ctx context.Context, agent *Agent) bool {
	for _, activeAgent := range activeRunAgentsFromContext(ctx) {
		if activeAgent == agent {
			return true
		}
	}
	return false
}

func activeRunAgentsFromContext(ctx context.Context) []*Agent {
	if ctx == nil {
		return nil
	}
	activeAgents, _ := ctx.Value(activeRunAgentContextKey{}).([]*Agent)
	return append([]*Agent(nil), activeAgents...)
}

func withRunID(ctx context.Context, runID string) context.Context {
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

func runIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	runID, _ := ctx.Value(runIDContextKey{}).(string)
	return runID
}

func setAgentErrorRunID(err error, runID string) {
	if err == nil || runID == "" {
		return
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr.RunID == "" {
		agentErr.RunID = runID
	}
}

func setAgentErrorParentRequestID(err error, parentRequestID string) {
	if err == nil || parentRequestID == "" {
		return
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr.ParentRequestID == "" {
		agentErr.ParentRequestID = parentRequestID
	}
}

func setAgentErrorTraceContext(err error, trace TraceContext) {
	if err == nil || trace == (TraceContext{}) {
		return
	}
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		return
	}
	if agentErr.TraceID == "" {
		agentErr.TraceID = trace.TraceID
	}
	if agentErr.SpanID == "" {
		agentErr.SpanID = trace.SpanID
	}
	if agentErr.TraceState == "" {
		agentErr.TraceState = trace.TraceState
	}
}

func traceContextFromContext(ctx context.Context) TraceContext {
	trace, _ := TraceContextFromContext(ctx)
	return trace
}

func applyTraceContextToEvent(ctx context.Context, event *Event) TraceContext {
	if event == nil {
		return TraceContext{}
	}
	trace := traceContextFromContext(ctx)
	if event.TraceID == "" {
		event.TraceID = trace.TraceID
	}
	if event.SpanID == "" {
		event.SpanID = trace.SpanID
	}
	if event.TraceState == "" {
		event.TraceState = trace.TraceState
	}
	return TraceContext{
		TraceID:    event.TraceID,
		SpanID:     event.SpanID,
		TraceState: event.TraceState,
	}
}

func (a *Agent) estimatedTokens(messages []Message) int {
	a.mu.Lock()
	counter := a.tokenCount
	a.mu.Unlock()
	return estimateMessagesTokens(counter, messages)
}

func (a *Agent) estimatedInputTokens(input string) int {
	a.mu.Lock()
	messages := cloneMessages(a.messages)
	counter := a.tokenCount
	a.mu.Unlock()
	messages = append(messages, Message{Role: RoleUser, Content: input})
	return estimateMessagesTokens(counter, messages)
}

func (a *Agent) emit(ctx context.Context, event Event) error {
	a.mu.Lock()
	event.AgentID = a.id
	observer := a.observer
	hooks := append([]Hook(nil), a.hooks...)
	a.mu.Unlock()
	trace := prepareEventForTelemetry(ctx, &event)

	notifyObserver(ctx, observer, event)

	for _, hook := range hooks {
		if err := hook(ctx, event); err != nil {
			cause := err
			if event.Error != nil {
				cause = errors.Join(err, event.Error)
			}
			wrapped := agentError(ErrorCategoryHook, "hook."+string(event.Type), cause)
			wrapped.AgentID = event.AgentID
			wrapped.RunID = event.RunID
			wrapped.RequestID = event.RequestID
			wrapped.ParentRequestID = event.ParentRequestID
			wrapped.ToolName = event.ToolName
			wrapped.SubagentID = event.SubagentID
			wrapped.Round = event.Round
			setAgentErrorTraceContext(wrapped, trace)
			return wrapped
		}
	}
	return nil
}

func (a *Agent) emitStreamAfterModel(ctx context.Context, event Event) error {
	if err := a.emit(ctx, event); err != nil {
		// Hook failures cannot be emitted through hooks again, but they should
		// still produce the same failed after-model observation shape.
		event.Error = err
		event.ErrorCategory = ""
		event.ProviderDiagnostics = ProviderDiagnostics{}
		event.ModelErrorSubcategory = ""
		a.observeEvent(ctx, event)
		return err
	}
	return nil
}

func (a *Agent) observeEvent(ctx context.Context, event Event) {
	a.mu.Lock()
	event.AgentID = a.id
	observer := a.observer
	a.mu.Unlock()
	prepareEventForTelemetry(ctx, &event)
	notifyObserver(ctx, observer, event)
}

func (a *Agent) observeStreamLifecycle(ctx context.Context, enabled bool, event Event) {
	if !enabled {
		return
	}
	// Stream lifecycle telemetry is observer-only so opt-in observations cannot
	// add hook rejection paths to streaming delivery.
	a.observeEvent(ctx, event)
}

func prepareEventForTelemetry(ctx context.Context, event *Event) TraceContext {
	if event.RunID == "" {
		event.RunID = runIDFromContext(ctx)
	}
	trace := applyTraceContextToEvent(ctx, event)
	if event.Error != nil && event.ErrorCategory == "" {
		event.ErrorCategory = classifyError(event.Error)
	}
	if event.Error != nil && event.ErrorCategory == ErrorCategoryModel && event.ModelErrorSubcategory == "" {
		if subcategory, ok := ModelErrorSubcategoryFromError(event.Error); ok {
			event.ModelErrorSubcategory = subcategory
		}
	}
	if event.Error != nil && event.ProviderDiagnostics.IsZero() {
		if diagnostics, ok := ProviderDiagnosticsFromError(event.Error); ok {
			event.ProviderDiagnostics = diagnostics
		}
	}
	setAgentErrorRunID(event.Error, event.RunID)
	setAgentErrorTraceContext(event.Error, trace)
	setAgentErrorParentRequestID(event.Error, event.ParentRequestID)
	return trace
}

func eventDurationSince(started time.Time) time.Duration {
	if started.IsZero() {
		return 0
	}
	duration := time.Since(started)
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}

func cloneSkills(skills []Skill) []Skill {
	if len(skills) == 0 {
		return nil
	}
	cloned := make([]Skill, len(skills))
	for i, skill := range skills {
		cloned[i] = cloneSkill(skill)
	}
	return cloned
}

func inlineSkillNames(input string) []string {
	fields := strings.Fields(input)
	var names []string
	for _, field := range fields {
		field = strings.Trim(field, " \t\r\n.,;:!?()[]{}")
		switch {
		case strings.HasPrefix(field, "+skill:"):
			names = append(names, strings.TrimPrefix(field, "+skill:"))
		case strings.HasPrefix(field, "@skill:"):
			names = append(names, strings.TrimPrefix(field, "@skill:"))
		case strings.HasPrefix(field, "+") && len(field) > 1:
			names = append(names, strings.TrimPrefix(field, "+"))
		}
	}
	return names
}
