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

	tools     map[string]Tool
	toolOrder []string

	mcpServers []MCPServerConfig

	approval    ApprovalPolicy
	hooks       []Hook
	observer    Observer
	compactor   Compactor
	tokenCount  TokenCounter
	runSeq      uint64
	requestSeq  uint64
	subagents   map[string]*Agent
	parentInbox map[string][]SubagentMessage
}

// Option customizes an Agent during construction.
type Option func(*Agent) error

// RunOption customizes a single Agent run.
type RunOption func(*runConfig)

type runConfig struct {
	skillNames             []string
	runID                  string
	observeStreamLifecycle bool
}

type runIDContextKey struct{}

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
		id:           config.ID,
		model:        model,
		config:       config,
		skills:       make(map[string]Skill),
		activeSkills: make(map[string]struct{}),
		tools:        make(map[string]Tool),
		approval:     AllowAllApproval{},
		observer:     NoopObserver{},
		tokenCount:   ApproxTokenCounter{},
		subagents:    make(map[string]*Agent),
		parentInbox:  make(map[string][]SubagentMessage),
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
		requestID := a.nextRequestID()
		parentRequestID := previousModelRequestID
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
// them. The final assistant message is written only after a done event arrives.
func (a *Agent) RunStream(ctx context.Context, input string, options ...RunOption) (<-chan StreamEvent, error) {
	var config runConfig
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	ctx = withRunID(ctx, a.runID(config))

	streamModel, ok := a.model.(StreamModel)
	if !ok {
		cause := fmt.Errorf("%w: %T", ErrStreamingUnsupported, a.model)
		wrapped := agentError(ErrorCategoryStreaming, "stream.unsupported", cause)
		wrapped.AgentID = a.agentID()
		wrapped.RunID = runIDFromContext(ctx)
		setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
		wrapped.RequestID = a.nextRequestID()
		wrapped.Round = 1
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

	request := a.buildModelRequest(activeSkills)
	requestID := a.nextRequestID()
	estimatedTokens := a.estimatedTokens(request.Messages)
	if err := a.emit(ctx, Event{
		Type:            EventBeforeModel,
		RequestID:       requestID,
		Round:           1,
		EstimatedTokens: estimatedTokens,
	}); err != nil {
		return nil, err
	}
	started := time.Now()
	a.observeStreamLifecycle(ctx, config.observeStreamLifecycle, Event{
		Type:            EventStreamStart,
		RequestID:       requestID,
		Round:           1,
		EstimatedTokens: estimatedTokens,
	})
	modelEvents, err := streamModel.Stream(ctx, request)
	if err != nil {
		wrapped := agentError(ErrorCategoryModel, "model.stream", err)
		wrapped.AgentID = request.AgentID
		wrapped.RequestID = requestID
		wrapped.Round = 1
		afterEvent := Event{
			Type:            EventAfterModel,
			RequestID:       requestID,
			Round:           1,
			Duration:        eventDurationSince(started),
			EstimatedTokens: estimatedTokens,
			Error:           wrapped,
		}
		if emitErr := a.emit(ctx, afterEvent); emitErr != nil {
			a.observeStreamLifecycle(ctx, config.observeStreamLifecycle, Event{
				Type:            EventStreamError,
				RequestID:       requestID,
				Round:           1,
				Duration:        afterEvent.Duration,
				EstimatedTokens: estimatedTokens,
				Error:           emitErr,
			})
			return nil, emitErr
		}
		a.observeStreamLifecycle(ctx, config.observeStreamLifecycle, Event{
			Type:            EventStreamError,
			RequestID:       requestID,
			Round:           1,
			Duration:        afterEvent.Duration,
			EstimatedTokens: estimatedTokens,
			Error:           wrapped,
		})
		return nil, wrapped
	}
	if modelEvents == nil {
		err := errors.New("agent: stream model returned nil event channel")
		wrapped := agentError(ErrorCategoryModel, "model.stream", err)
		wrapped.AgentID = request.AgentID
		wrapped.RequestID = requestID
		wrapped.Round = 1
		afterEvent := Event{
			Type:            EventAfterModel,
			RequestID:       requestID,
			Round:           1,
			Duration:        eventDurationSince(started),
			EstimatedTokens: estimatedTokens,
			Error:           wrapped,
		}
		if emitErr := a.emit(ctx, afterEvent); emitErr != nil {
			a.observeStreamLifecycle(ctx, config.observeStreamLifecycle, Event{
				Type:            EventStreamError,
				RequestID:       requestID,
				Round:           1,
				Duration:        afterEvent.Duration,
				EstimatedTokens: estimatedTokens,
				Error:           emitErr,
			})
			return nil, emitErr
		}
		a.observeStreamLifecycle(ctx, config.observeStreamLifecycle, Event{
			Type:            EventStreamError,
			RequestID:       requestID,
			Round:           1,
			Duration:        afterEvent.Duration,
			EstimatedTokens: estimatedTokens,
			Error:           wrapped,
		})
		return nil, wrapped
	}

	out := make(chan StreamEvent)
	go a.forwardStreamEvents(ctx, modelEvents, out, requestID, 1, estimatedTokens, started, config.observeStreamLifecycle)
	return out, nil
}

func (a *Agent) forwardStreamEvents(ctx context.Context, modelEvents <-chan StreamEvent, out chan<- StreamEvent, requestID string, round int, estimatedTokens int, started time.Time, observeStreamLifecycle bool) {
	defer close(out)

	a.mu.Lock()
	agentID := a.id
	a.mu.Unlock()

	var content strings.Builder
	telemetry := streamTelemetryTracker{started: started}
	done := false
	for event := range modelEvents {
		event.AgentID = agentID

		switch event.Type {
		case StreamEventDelta:
			if len(event.Message.ToolCalls) > 0 {
				a.sendStreamError(ctx, out, agentID, ErrStreamingToolCallsUnsupported, requestID, round, estimatedTokens, started, telemetry, observeStreamLifecycle)
				return
			}
			telemetry.recordDelta(event.Delta)
			if telemetry.deltaCount == 1 {
				duration := eventDurationSince(started)
				a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
					Type:            EventStreamFirstDelta,
					RequestID:       requestID,
					Round:           round,
					Duration:        duration,
					EstimatedTokens: estimatedTokens,
					StreamTelemetry: telemetry.telemetry(duration),
				})
			}
			content.WriteString(event.Delta)
			if !sendStreamEvent(ctx, out, event) {
				return
			}
		case StreamEventDone:
			if len(event.Message.ToolCalls) > 0 {
				a.sendStreamError(ctx, out, agentID, ErrStreamingToolCallsUnsupported, requestID, round, estimatedTokens, started, telemetry, observeStreamLifecycle)
				return
			}
			message := event.Message
			if message.Role == "" {
				message.Role = RoleAssistant
			}
			if message.Content == "" {
				message.Content = content.String()
			}
			event.Message = cloneMessage(message)

			duration := eventDurationSince(started)
			if err := a.emit(ctx, Event{
				Type:            EventAfterModel,
				RequestID:       requestID,
				Round:           round,
				Duration:        duration,
				EstimatedTokens: estimatedTokens,
				StreamTelemetry: telemetry.telemetry(duration),
				Message:         message,
			}); err != nil {
				a.sendStreamError(ctx, out, agentID, err, requestID, round, estimatedTokens, started, telemetry, observeStreamLifecycle)
				return
			}
			a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
				Type:            EventStreamDone,
				RequestID:       requestID,
				Round:           round,
				Duration:        duration,
				EstimatedTokens: estimatedTokens,
				StreamTelemetry: telemetry.telemetry(duration),
			})
			// Commit only after done so interrupted delta streams do not persist partial assistant text.
			a.appendMessage(message)
			done = true
			if !sendStreamEvent(ctx, out, event) {
				return
			}
		case StreamEventError:
			if event.Error == nil {
				event.Error = errors.New("agent: stream error")
			}
			wrapped := agentError(ErrorCategoryModel, "model.stream", event.Error)
			wrapped.AgentID = agentID
			wrapped.RequestID = requestID
			wrapped.Round = round
			event.Error = wrapped
			duration := eventDurationSince(started)
			if emitErr := a.emit(ctx, Event{
				Type:            EventAfterModel,
				RequestID:       requestID,
				Round:           round,
				Duration:        duration,
				EstimatedTokens: estimatedTokens,
				StreamTelemetry: telemetry.telemetry(duration),
				Error:           wrapped,
			}); emitErr != nil {
				event.Error = emitErr
			}
			a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
				Type:            EventStreamError,
				RequestID:       requestID,
				Round:           round,
				Duration:        duration,
				EstimatedTokens: estimatedTokens,
				StreamTelemetry: telemetry.telemetry(duration),
				Error:           event.Error,
			})
			_ = sendStreamEvent(ctx, out, event)
			return
		default:
			err := fmt.Errorf("agent: unknown stream event type %q", event.Type)
			a.sendStreamError(ctx, out, agentID, err, requestID, round, estimatedTokens, started, telemetry, observeStreamLifecycle)
			return
		}
	}

	if !done {
		a.sendStreamError(ctx, out, agentID, errors.New("agent: stream ended without done event"), requestID, round, estimatedTokens, started, telemetry, observeStreamLifecycle)
	}
}

func (a *Agent) sendStreamError(ctx context.Context, out chan<- StreamEvent, agentID string, err error, requestID string, round int, estimatedTokens int, started time.Time, telemetry streamTelemetryTracker, observeStreamLifecycle bool) {
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
		if agentErr.Round == 0 {
			agentErr.Round = round
		}
	}
	if classifyError(err) == ErrorCategoryHook {
		duration := eventDurationSince(started)
		a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
			Type:            EventStreamError,
			RequestID:       requestID,
			Round:           round,
			Duration:        duration,
			EstimatedTokens: estimatedTokens,
			StreamTelemetry: telemetry.telemetry(duration),
			Error:           err,
		})
		_ = sendStreamEvent(ctx, out, StreamEvent{
			Type:    StreamEventError,
			AgentID: agentID,
			Error:   err,
		})
		return
	}
	duration := eventDurationSince(started)
	if emitErr := a.emit(ctx, Event{
		Type:            EventAfterModel,
		RequestID:       requestID,
		Round:           round,
		Duration:        duration,
		EstimatedTokens: estimatedTokens,
		StreamTelemetry: telemetry.telemetry(duration),
		Error:           err,
	}); emitErr != nil {
		err = emitErr
	}
	a.observeStreamLifecycle(ctx, observeStreamLifecycle, Event{
		Type:            EventStreamError,
		RequestID:       requestID,
		Round:           round,
		Duration:        duration,
		EstimatedTokens: estimatedTokens,
		StreamTelemetry: telemetry.telemetry(duration),
		Error:           err,
	})
	_ = sendStreamEvent(ctx, out, StreamEvent{
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

func (a *Agent) nextRequestID() string {
	sequence := atomic.AddUint64(&a.requestSeq, 1)
	return fmt.Sprintf("%s-request-%d", a.agentID(), sequence)
}

func (a *Agent) runID(config runConfig) string {
	if config.runID != "" {
		return config.runID
	}
	sequence := atomic.AddUint64(&a.runSeq, 1)
	return fmt.Sprintf("%s-run-%d", a.agentID(), sequence)
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

func (a *Agent) observeStreamLifecycle(ctx context.Context, enabled bool, event Event) {
	if !enabled {
		return
	}
	// Stream lifecycle telemetry is observer-only so opt-in observations cannot
	// add hook rejection paths to streaming delivery.
	a.mu.Lock()
	event.AgentID = a.id
	observer := a.observer
	a.mu.Unlock()
	prepareEventForTelemetry(ctx, &event)
	notifyObserver(ctx, observer, event)
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
