package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrSubagentNotFound = errors.New("agent: subagent not found")

// SubagentOptions controls how a child agent is created and which capabilities it inherits.
type SubagentOptions struct {
	ID           string
	SystemPrompt string
	Model        Model

	InheritTools        bool
	InheritToolNames    []string
	InheritMCP          bool
	InheritMCPNames     []string
	InheritSkills       bool
	InheritSkillNames   []string
	InheritHooks        bool
	InheritInstructions bool

	Tools      []Tool
	Skills     []Skill
	MCPServers []MCPServerConfig
}

// SubagentMessage records a message sent from a subagent back to its parent.
type SubagentMessage struct {
	From    string
	To      string
	Message Message
}

// SpawnSubagent creates a child agent with selected inherited capabilities.
func (a *Agent) SpawnSubagent(ctx context.Context, options SubagentOptions) (*Agent, error) {
	a.mu.Lock()
	if options.ID == "" {
		options.ID = fmt.Sprintf("%s-subagent-%d", a.id, len(a.subagents)+1)
	}
	if _, exists := a.subagents[options.ID]; exists {
		parentID := a.id
		a.mu.Unlock()
		wrapped := agentError(ErrorCategorySubagent, "subagent.spawn", fmt.Errorf("agent: subagent %q already exists", options.ID))
		wrapped.AgentID = parentID
		wrapped.RunID = runIDFromContext(ctx)
		setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
		wrapped.SubagentID = options.ID
		wrapped.RequestID = a.nextRequestID()
		return nil, wrapped
	}
	model := options.Model
	if model == nil {
		model = a.model
	}
	childConfig := Config{
		ID:            options.ID,
		SystemPrompt:  options.SystemPrompt,
		Compact:       a.config.Compact,
		MaxToolRounds: a.config.MaxToolRounds,
	}
	if childConfig.SystemPrompt == "" {
		childConfig.SystemPrompt = a.config.SystemPrompt
	}
	parentTools := selectedTools(a, options)
	parentSkills := selectedSkills(a, options)
	parentMCP := selectedMCP(a, options)
	parentHooks := append([]Hook(nil), a.hooks...)
	parentObserver := a.observer
	parentInstructions := append([]instructionFile(nil), a.instructions...)
	parentCompactor := a.compactor
	parentCounter := a.tokenCount
	parentApproval := a.approval
	a.mu.Unlock()

	childOptions := []Option{
		WithTools(parentTools...),
		WithSkills(parentSkills...),
		WithMCPServers(parentMCP...),
		WithApprovalPolicy(parentApproval),
		WithTokenCounter(parentCounter),
		WithObserver(parentObserver),
	}
	if parentCompactor != nil {
		childOptions = append(childOptions, WithCompactor(parentCompactor))
	}
	child, err := New(childConfig, model, childOptions...)
	if err != nil {
		return nil, err
	}
	child.parent = a
	for _, tool := range options.Tools {
		if err := WithTools(tool)(child); err != nil {
			return nil, err
		}
	}
	for _, skill := range options.Skills {
		if err := child.registerSkill(skill); err != nil {
			return nil, err
		}
	}
	child.mcpServers = append(child.mcpServers, cloneMCPServers(options.MCPServers)...)
	if options.InheritHooks {
		child.hooks = append(child.hooks, parentHooks...)
	}
	if options.InheritInstructions {
		child.instructions = append(child.instructions, parentInstructions...)
	}

	a.mu.Lock()
	a.subagents[options.ID] = child
	a.mu.Unlock()

	if err := a.emit(ctx, Event{
		Type:       EventSubagentMessage,
		SubagentID: options.ID,
		RequestID:  a.nextRequestID(),
	}); err != nil {
		return nil, err
	}
	return child, nil
}

// SendMessageToSubagent runs a child agent with a parent-provided message.
func (a *Agent) SendMessageToSubagent(ctx context.Context, id string, content string, options ...RunOption) (Message, error) {
	a.mu.Lock()
	child := a.subagents[id]
	a.mu.Unlock()
	requestID := a.nextRequestID()
	if child == nil {
		cause := fmt.Errorf("%w: %s", ErrSubagentNotFound, id)
		wrapped := agentError(ErrorCategorySubagent, "subagent.lookup", cause)
		wrapped.AgentID = a.agentID()
		wrapped.SubagentID = id
		wrapped.RequestID = requestID
		if emitErr := a.emit(ctx, Event{
			Type:       EventSubagentMessage,
			SubagentID: id,
			RequestID:  requestID,
			Error:      wrapped,
		}); emitErr != nil {
			return Message{}, emitErr
		}
		return Message{}, wrapped
	}
	started := time.Now()
	response, err := child.Run(ctx, content, options...)
	if err != nil {
		wrapped := agentError(ErrorCategorySubagent, "subagent.run", err)
		wrapped.AgentID = a.agentID()
		wrapped.SubagentID = id
		wrapped.RequestID = requestID
		if emitErr := a.emit(ctx, Event{
			Type:       EventSubagentMessage,
			SubagentID: id,
			RequestID:  requestID,
			Duration:   eventDurationSince(started),
			Error:      wrapped,
		}); emitErr != nil {
			return Message{}, emitErr
		}
		return Message{}, wrapped
	}
	return response, a.emit(ctx, Event{
		Type:       EventSubagentMessage,
		SubagentID: id,
		RequestID:  requestID,
		Duration:   eventDurationSince(started),
		Message:    response,
	})
}

// SendToParent records a subagent-originated message in the parent inbox.
func (a *Agent) SendToParent(ctx context.Context, content string) error {
	a.mu.Lock()
	parent := a.parent
	from := a.id
	a.mu.Unlock()
	if parent == nil {
		wrapped := agentError(ErrorCategorySubagent, "subagent.parent", errors.New("agent: agent has no parent"))
		wrapped.AgentID = from
		wrapped.RunID = runIDFromContext(ctx)
		setAgentErrorTraceContext(wrapped, traceContextFromContext(ctx))
		return wrapped
	}
	message := SubagentMessage{
		From:    from,
		To:      parent.ID(),
		Message: Message{Role: RoleAssistant, Content: content},
	}
	parent.recordSubagentMessage(message)
	return parent.emit(ctx, Event{
		Type:       EventSubagentMessage,
		SubagentID: from,
		RequestID:  parent.nextRequestID(),
		Message:    message.Message,
	})
}

// DrainSubagentMessages returns and clears messages sent from a subagent to this agent.
func (a *Agent) DrainSubagentMessages(id string) []SubagentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	messages := append([]SubagentMessage(nil), a.parentInbox[id]...)
	delete(a.parentInbox, id)
	return messages
}

func (a *Agent) recordSubagentMessage(message SubagentMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.parentInbox[message.From] = append(a.parentInbox[message.From], message)
}

func selectedTools(a *Agent, options SubagentOptions) []Tool {
	var tools []Tool
	add := func(name string) {
		if tool := a.tools[name]; tool != nil {
			tools = append(tools, tool)
		}
	}
	if options.InheritTools {
		for _, name := range a.toolOrder {
			add(name)
		}
		return tools
	}
	for _, name := range options.InheritToolNames {
		add(name)
	}
	return tools
}

func selectedSkills(a *Agent, options SubagentOptions) []Skill {
	var skills []Skill
	add := func(name string) {
		if skill, ok := a.skills[name]; ok {
			skills = append(skills, cloneSkill(skill))
		}
	}
	if options.InheritSkills {
		for _, name := range a.skillOrder {
			add(name)
		}
		return skills
	}
	for _, name := range options.InheritSkillNames {
		add(name)
	}
	return skills
}

func selectedMCP(a *Agent, options SubagentOptions) []MCPServerConfig {
	if options.InheritMCP {
		return cloneMCPServers(a.mcpServers)
	}
	if len(options.InheritMCPNames) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(options.InheritMCPNames))
	for _, name := range options.InheritMCPNames {
		names[name] = struct{}{}
	}
	var servers []MCPServerConfig
	for _, server := range a.mcpServers {
		if _, ok := names[server.Name]; ok {
			servers = append(servers, server)
		}
	}
	return cloneMCPServers(servers)
}
