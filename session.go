package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SessionSnapshot is a persistable, point-in-time copy of an agent's conversation context.
type SessionSnapshot struct {
	AgentID   string    `json:"agent_id,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`

	messages []Message
}

type sessionSnapshotJSON struct {
	AgentID   string    `json:"agent_id,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	Messages  []Message `json:"messages,omitempty"`
}

// NewSessionSnapshot builds a snapshot from externally persisted messages.
func NewSessionSnapshot(agentID string, messages []Message) SessionSnapshot {
	return SessionSnapshot{
		AgentID:   agentID,
		CreatedAt: time.Now().UTC(),
		messages:  cloneMessages(messages),
	}
}

// Messages returns a deep copy of the snapshot context.
func (s SessionSnapshot) Messages() []Message {
	return cloneMessages(s.messages)
}

// MarshalJSON exposes messages for persistence without making the in-memory slice mutable.
func (s SessionSnapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(sessionSnapshotJSON{
		AgentID:   s.AgentID,
		CreatedAt: s.CreatedAt,
		Messages:  cloneMessages(s.messages),
	})
}

// UnmarshalJSON restores a persisted snapshot while isolating it from decoder-owned slices.
func (s *SessionSnapshot) UnmarshalJSON(data []byte) error {
	if s == nil {
		return fmt.Errorf("agent: cannot unmarshal nil session snapshot")
	}
	var payload sessionSnapshotJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	s.AgentID = payload.AgentID
	s.CreatedAt = payload.CreatedAt
	s.messages = cloneMessages(payload.Messages)
	return nil
}

// Reset clears the managed conversation context without changing model or capabilities.
func (a *Agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = nil
}

// Snapshot returns an isolated copy of the current conversation context.
func (a *Agent) Snapshot() SessionSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return SessionSnapshot{
		AgentID:   a.id,
		CreatedAt: time.Now().UTC(),
		messages:  cloneMessages(a.messages),
	}
}

// Restore replaces only the managed conversation context from a snapshot.
func (a *Agent) Restore(snapshot SessionSnapshot) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = cloneMessages(snapshot.messages)
	return nil
}

// Fork creates an independent agent with the same capabilities and a copied context.
func (a *Agent) Fork(id string) (*Agent, error) {
	a.mu.Lock()
	config := a.config
	if id = strings.TrimSpace(id); id != "" {
		config.ID = id
	} else {
		config.ID = ""
	}
	model := a.model
	messages := cloneMessages(a.messages)
	instructions := append([]instructionFile(nil), a.instructions...)
	skills := cloneSkillMap(a.skills)
	skillOrder := append([]string(nil), a.skillOrder...)
	activeSkills := cloneActiveSkillSet(a.activeSkills)
	tools := cloneToolMap(a.tools)
	toolOrder := append([]string(nil), a.toolOrder...)
	mcpServers := cloneMCPServers(a.mcpServers)
	approval := a.approval
	hooks := append([]Hook(nil), a.hooks...)
	observer := a.observer
	compactor := a.compactor
	tokenCount := a.tokenCount
	requestIDGenerator := a.requestIDGenerator
	a.mu.Unlock()

	fork, err := New(config, model)
	if err != nil {
		return nil, err
	}
	fork.mu.Lock()
	defer fork.mu.Unlock()
	fork.messages = messages
	fork.instructions = instructions
	fork.skills = skills
	fork.skillOrder = skillOrder
	fork.activeSkills = activeSkills
	fork.tools = tools
	fork.toolOrder = toolOrder
	fork.mcpServers = mcpServers
	fork.approval = approval
	fork.hooks = hooks
	fork.observer = observer
	fork.compactor = compactor
	fork.tokenCount = tokenCount
	fork.requestIDGenerator = requestIDGenerator
	fork.parent = nil
	fork.subagents = make(map[string]*Agent)
	fork.parentInbox = make(map[string][]SubagentMessage)
	return fork, nil
}

func cloneSkillMap(skills map[string]Skill) map[string]Skill {
	cloned := make(map[string]Skill, len(skills))
	for name, skill := range skills {
		cloned[name] = cloneSkill(skill)
	}
	return cloned
}

func cloneActiveSkillSet(activeSkills map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(activeSkills))
	for name := range activeSkills {
		cloned[name] = struct{}{}
	}
	return cloned
}

func cloneToolMap(tools map[string]Tool) map[string]Tool {
	cloned := make(map[string]Tool, len(tools))
	for name, tool := range tools {
		cloned[name] = tool
	}
	return cloned
}
