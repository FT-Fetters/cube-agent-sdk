package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// EvalTranscriptSchemaVersion is the JSON schema version for RunTranscript.
const EvalTranscriptSchemaVersion = 1

// ScriptedModelStep is one deterministic Generate result for ScriptedModel.
type ScriptedModelStep struct {
	Response ModelResponse
	Err      error
}

// ScriptedResponse returns a scripted successful model response.
func ScriptedResponse(response ModelResponse) ScriptedModelStep {
	return ScriptedModelStep{Response: cloneModelResponse(response)}
}

// ScriptedError returns a scripted model failure.
func ScriptedError(err error) ScriptedModelStep {
	if err == nil {
		err = errors.New("agent: scripted model error")
	}
	return ScriptedModelStep{Err: err}
}

// ScriptedModel is a dependency-free deterministic Model for Go tests.
type ScriptedModel struct {
	mu       sync.Mutex
	steps    []ScriptedModelStep
	requests []ModelRequest
	recorder *EvalRecorder
}

// NewScriptedModel constructs a model that consumes one step per Generate call.
func NewScriptedModel(steps ...ScriptedModelStep) *ScriptedModel {
	return &ScriptedModel{steps: cloneScriptedModelSteps(steps)}
}

// RecordWith records model exchanges into recorder in addition to local requests.
func (m *ScriptedModel) RecordWith(recorder *EvalRecorder) *ScriptedModel {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.recorder = recorder
	m.mu.Unlock()
	return m
}

// Generate records request and returns the next scripted response or error.
func (m *ScriptedModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m == nil {
		return ModelResponse{}, errors.New("agent: scripted model is nil")
	}
	if err := contextError(ctx); err != nil {
		return ModelResponse{}, err
	}

	recordedRequest := cloneModelRequestForEval(request)
	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest)
	requestIndex := len(m.requests)
	if len(m.steps) == 0 {
		recorder := m.recorder
		m.mu.Unlock()
		err := fmt.Errorf("agent: scripted model exhausted at request %d", requestIndex)
		if recorder != nil {
			recorder.recordModelExchange(recordedRequest, ModelResponse{}, err)
		}
		return ModelResponse{}, err
	}
	step := m.steps[0]
	m.steps = m.steps[1:]
	recorder := m.recorder
	m.mu.Unlock()

	response := cloneModelResponse(step.Response)
	if recorder != nil {
		recorder.recordModelExchange(recordedRequest, response, step.Err)
	}
	return response, step.Err
}

// Requests returns the model requests seen so far.
func (m *ScriptedModel) Requests() []ModelRequest {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneModelRequestsForEval(m.requests)
}

// ScriptedStreamStep is one deterministic Stream result for ScriptedStreamModel.
type ScriptedStreamStep struct {
	Events []StreamEvent
	Err    error
}

// ScriptedStreamEvents returns a scripted stream that emits events then closes.
func ScriptedStreamEvents(events ...StreamEvent) ScriptedStreamStep {
	return ScriptedStreamStep{Events: cloneStreamEventsForEval(events)}
}

// ScriptedStreamError returns a scripted stream-start failure.
func ScriptedStreamError(err error) ScriptedStreamStep {
	if err == nil {
		err = errors.New("agent: scripted stream error")
	}
	return ScriptedStreamStep{Err: err}
}

// ScriptedStreamModel is a deterministic StreamModel for Go tests.
type ScriptedStreamModel struct {
	mu             sync.Mutex
	generate       *ScriptedModel
	streamSteps    []ScriptedStreamStep
	streamRequests []ModelRequest
	recorder       *EvalRecorder
}

// NewScriptedStreamModel constructs a model that consumes one step per Stream call.
func NewScriptedStreamModel(steps ...ScriptedStreamStep) *ScriptedStreamModel {
	return &ScriptedStreamModel{
		generate:    NewScriptedModel(),
		streamSteps: cloneScriptedStreamSteps(steps),
	}
}

// SetGenerateSteps configures deterministic Generate responses for non-streaming calls.
func (m *ScriptedStreamModel) SetGenerateSteps(steps ...ScriptedModelStep) *ScriptedStreamModel {
	if m == nil {
		return nil
	}
	model := NewScriptedModel(steps...)
	m.mu.Lock()
	model.RecordWith(m.recorder)
	m.generate = model
	m.mu.Unlock()
	return m
}

// RecordWith records Generate and Stream exchanges into recorder.
func (m *ScriptedStreamModel) RecordWith(recorder *EvalRecorder) *ScriptedStreamModel {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.recorder = recorder
	generate := m.generate
	m.mu.Unlock()
	if generate != nil {
		generate.RecordWith(recorder)
	}
	return m
}

// Generate delegates to the configured scripted non-streaming model.
func (m *ScriptedStreamModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m == nil {
		return ModelResponse{}, errors.New("agent: scripted stream model is nil")
	}
	m.mu.Lock()
	generate := m.generate
	m.mu.Unlock()
	if generate == nil {
		return ModelResponse{}, errors.New("agent: scripted stream model has no Generate script")
	}
	return generate.Generate(ctx, request)
}

// Stream records request and emits the next scripted stream step.
func (m *ScriptedStreamModel) Stream(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
	if m == nil {
		return nil, errors.New("agent: scripted stream model is nil")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	recordedRequest := cloneModelRequestForEval(request)
	m.mu.Lock()
	m.streamRequests = append(m.streamRequests, recordedRequest)
	requestIndex := len(m.streamRequests)
	if len(m.streamSteps) == 0 {
		recorder := m.recorder
		m.mu.Unlock()
		err := fmt.Errorf("agent: scripted stream model exhausted at request %d", requestIndex)
		if recorder != nil {
			recorder.recordStreamExchange(recordedRequest, nil, err)
		}
		return nil, err
	}
	step := m.streamSteps[0]
	m.streamSteps = m.streamSteps[1:]
	recorder := m.recorder
	m.mu.Unlock()

	events := cloneStreamEventsForEval(step.Events)
	if recorder != nil {
		recorder.recordStreamExchange(recordedRequest, events, step.Err)
	}
	if step.Err != nil {
		return nil, step.Err
	}

	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		for _, event := range events {
			select {
			case <-ctx.Done():
				out <- StreamEvent{Type: StreamEventError, Error: ctx.Err()}
				return
			case out <- cloneStreamEventForEval(event):
			}
		}
	}()
	return out, nil
}

// StreamRequests returns the stream model requests seen so far.
func (m *ScriptedStreamModel) StreamRequests() []ModelRequest {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneModelRequestsForEval(m.streamRequests)
}

// EvalRecorder captures lifecycle events, observations, model exchanges, and outcomes.
type EvalRecorder struct {
	mu              sync.Mutex
	inputs          []string
	events          []Event
	observations    []Observation
	modelExchanges  []TranscriptModelExchange
	streamExchanges []TranscriptStreamExchange
	final           *TranscriptOutcome
}

// NewEvalRecorder constructs an empty eval recorder.
func NewEvalRecorder() *EvalRecorder {
	return &EvalRecorder{}
}

// Hook returns a lifecycle hook that records raw SDK events for transcript assertions.
func (r *EvalRecorder) Hook() Hook {
	return func(ctx context.Context, event Event) error {
		if r != nil {
			r.recordEvent(event)
		}
		return nil
	}
}

// Observe records sanitized observations.
func (r *EvalRecorder) Observe(ctx context.Context, observation Observation) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, cloneObservationForEval(observation))
}

// RecordInput appends an application-level run input to the transcript.
func (r *EvalRecorder) RecordInput(input string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.inputs = append(r.inputs, input)
	r.mu.Unlock()
}

// RecordOutcome records the latest run outcome without appending a new input.
func (r *EvalRecorder) RecordOutcome(message Message, err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.final = transcriptOutcome(message, err)
	r.mu.Unlock()
}

// RecordRun appends input, records the final outcome, and returns a transcript snapshot.
func (r *EvalRecorder) RecordRun(input string, message Message, err error) RunTranscript {
	if r == nil {
		return RunTranscript{SchemaVersion: EvalTranscriptSchemaVersion}
	}
	r.mu.Lock()
	r.inputs = append(r.inputs, input)
	r.final = transcriptOutcome(message, err)
	r.mu.Unlock()
	return r.Transcript()
}

// Events returns the raw lifecycle events observed so far.
func (r *EvalRecorder) Events() []Event {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneEventsForEval(r.events)
}

// Observations returns the sanitized observations observed so far.
func (r *EvalRecorder) Observations() []Observation {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneObservationsForEval(r.observations)
}

// Transcript returns a stable JSON-friendly snapshot of the recorded run.
func (r *EvalRecorder) Transcript() RunTranscript {
	if r == nil {
		return RunTranscript{SchemaVersion: EvalTranscriptSchemaVersion}
	}
	r.mu.Lock()
	inputs := append([]string(nil), r.inputs...)
	events := cloneEventsForEval(r.events)
	observations := cloneObservationsForEval(r.observations)
	modelExchanges := cloneTranscriptModelExchanges(r.modelExchanges)
	streamExchanges := cloneTranscriptStreamExchanges(r.streamExchanges)
	final := cloneTranscriptOutcome(r.final)
	r.mu.Unlock()

	return RunTranscript{
		SchemaVersion:   EvalTranscriptSchemaVersion,
		Inputs:          inputs,
		ModelExchanges:  modelExchanges,
		StreamExchanges: streamExchanges,
		Events:          transcriptEvents(events),
		ToolCalls:       transcriptToolCalls(events),
		Observations:    transcriptObservations(observations),
		Final:           final,
	}
}

// Reset clears all recorded eval state.
func (r *EvalRecorder) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = nil
	r.events = nil
	r.observations = nil
	r.modelExchanges = nil
	r.streamExchanges = nil
	r.final = nil
}

func (r *EvalRecorder) recordEvent(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, cloneEventForEval(event))
}

func (r *EvalRecorder) recordModelExchange(request ModelRequest, response ModelResponse, err error) {
	if r == nil {
		return
	}
	exchange := TranscriptModelExchange{
		Request: modelRequestTranscript(request),
		Error:   errorStringForEval(err),
	}
	if !modelResponseIsZero(response) || err == nil {
		transcriptResponse := modelResponseTranscript(response)
		exchange.Response = &transcriptResponse
	}
	r.mu.Lock()
	r.modelExchanges = append(r.modelExchanges, exchange)
	r.mu.Unlock()
}

func (r *EvalRecorder) recordStreamExchange(request ModelRequest, events []StreamEvent, err error) {
	if r == nil {
		return
	}
	exchange := TranscriptStreamExchange{
		Request: modelRequestTranscript(request),
		Events:  transcriptStreamEvents(events),
		Error:   errorStringForEval(err),
	}
	r.mu.Lock()
	r.streamExchanges = append(r.streamExchanges, exchange)
	r.mu.Unlock()
}

// RunTranscript is a stable JSON-friendly eval artifact for Go tests.
type RunTranscript struct {
	SchemaVersion   int                        `json:"schema_version"`
	Inputs          []string                   `json:"inputs,omitempty"`
	ModelExchanges  []TranscriptModelExchange  `json:"model_exchanges,omitempty"`
	StreamExchanges []TranscriptStreamExchange `json:"stream_exchanges,omitempty"`
	Events          []TranscriptEvent          `json:"events,omitempty"`
	ToolCalls       []TranscriptToolCall       `json:"tool_calls,omitempty"`
	Observations    []TranscriptObservation    `json:"observations,omitempty"`
	SessionEvents   []TranscriptSessionEvent   `json:"session_events,omitempty"`
	Final           *TranscriptOutcome         `json:"final,omitempty"`
}

// StableJSON returns deterministic indented JSON suitable for golden files.
func (t RunTranscript) StableJSON() ([]byte, error) {
	if t.SchemaVersion == 0 {
		t.SchemaVersion = EvalTranscriptSchemaVersion
	}
	payload, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

// ReplayRunTranscript decodes a saved transcript JSON artifact.
func ReplayRunTranscript(data []byte) (RunTranscript, error) {
	var transcript RunTranscript
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&transcript); err != nil {
		return RunTranscript{}, err
	}
	if transcript.SchemaVersion == 0 {
		transcript.SchemaVersion = EvalTranscriptSchemaVersion
	}
	if transcript.SchemaVersion != EvalTranscriptSchemaVersion {
		return RunTranscript{}, fmt.Errorf("agent: unsupported eval transcript schema version %d", transcript.SchemaVersion)
	}
	return transcript, nil
}

// ReplaySessionEvents converts saved SessionEvent logs into a transcript for assertions.
func ReplaySessionEvents(events []SessionEvent) (RunTranscript, error) {
	var lastSequence uint64
	transcriptEvents := make([]TranscriptSessionEvent, 0, len(events))
	for _, event := range events {
		normalized, err := normalizeSessionEvent(event)
		if err != nil {
			return RunTranscript{}, err
		}
		if normalized.Sequence != 0 && normalized.Sequence <= lastSequence {
			return RunTranscript{}, fmt.Errorf("agent: session events are not in replay order at sequence %d", normalized.Sequence)
		}
		lastSequence = normalized.Sequence
		transcriptEvents = append(transcriptEvents, transcriptSessionEvent(normalized))
	}
	return RunTranscript{
		SchemaVersion: EvalTranscriptSchemaVersion,
		SessionEvents: transcriptEvents,
	}, nil
}

// TranscriptModelExchange records one model request and response or error.
type TranscriptModelExchange struct {
	Request  TranscriptModelRequest   `json:"request"`
	Response *TranscriptModelResponse `json:"response,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

// TranscriptModelRequest is a sanitized, stable model request view.
type TranscriptModelRequest struct {
	AgentID      string                     `json:"agent_id,omitempty"`
	SystemPrompt string                     `json:"system_prompt,omitempty"`
	Messages     []Message                  `json:"messages,omitempty"`
	Tools        []TranscriptToolDescriptor `json:"tools,omitempty"`
	MCPServers   []TranscriptMCPServer      `json:"mcp_servers,omitempty"`
	ActiveSkills []TranscriptSkill          `json:"active_skills,omitempty"`
}

// TranscriptModelResponse records a model response in transcript form.
type TranscriptModelResponse struct {
	Message   Message    `json:"message,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     TokenUsage `json:"usage,omitempty"`
}

// TranscriptToolDescriptor is a stable tool descriptor view for evals.
type TranscriptToolDescriptor struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Risk        ToolRisk `json:"risk,omitempty"`
}

// TranscriptMCPServer records only non-secret MCP request metadata.
type TranscriptMCPServer struct {
	Name      string       `json:"name,omitempty"`
	Transport MCPTransport `json:"transport,omitempty"`
}

// TranscriptSkill records active skill metadata included in a model request.
type TranscriptSkill struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// TranscriptStreamExchange records one stream request and scripted stream result.
type TranscriptStreamExchange struct {
	Request TranscriptModelRequest  `json:"request"`
	Events  []TranscriptStreamEvent `json:"events,omitempty"`
	Error   string                  `json:"error,omitempty"`
}

// TranscriptStreamEvent is a JSON-friendly stream event.
type TranscriptStreamEvent struct {
	Type    StreamEventType `json:"type,omitempty"`
	Delta   string          `json:"delta,omitempty"`
	Message Message         `json:"message,omitempty"`
	Usage   TokenUsage      `json:"usage,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// TranscriptEvent is a stable lifecycle event view without timing fields.
type TranscriptEvent struct {
	Type                  EventType             `json:"type,omitempty"`
	AgentID               string                `json:"agent_id,omitempty"`
	RunID                 string                `json:"run_id,omitempty"`
	SubagentID            string                `json:"subagent_id,omitempty"`
	ToolName              string                `json:"tool_name,omitempty"`
	ToolRisk              ToolRisk              `json:"tool_risk,omitempty"`
	ToolSchemaHash        string                `json:"tool_schema_hash,omitempty"`
	SkillName             string                `json:"skill_name,omitempty"`
	TraceID               string                `json:"trace_id,omitempty"`
	SpanID                string                `json:"span_id,omitempty"`
	TraceState            string                `json:"trace_state,omitempty"`
	RequestID             string                `json:"request_id,omitempty"`
	ParentRequestID       string                `json:"parent_request_id,omitempty"`
	Round                 int                   `json:"round,omitempty"`
	EstimatedTokens       int                   `json:"estimated_tokens,omitempty"`
	TokenUsage            TokenUsage            `json:"token_usage,omitempty"`
	StreamDeltaCount      int                   `json:"stream_delta_count,omitempty"`
	StreamByteCount       int                   `json:"stream_byte_count,omitempty"`
	ProviderDiagnostics   ProviderDiagnostics   `json:"provider_diagnostics,omitempty"`
	ModelErrorSubcategory ModelErrorSubcategory `json:"model_error_subcategory,omitempty"`
	Approved              bool                  `json:"approved,omitempty"`
	ApprovalReason        string                `json:"approval_reason,omitempty"`
	ErrorCategory         ErrorCategory         `json:"error_category,omitempty"`
	Failed                bool                  `json:"failed,omitempty"`
	Error                 string                `json:"error,omitempty"`
	Message               *Message              `json:"message,omitempty"`
	ToolCall              *ToolCall             `json:"tool_call,omitempty"`
	ToolResult            *ToolResult           `json:"tool_result,omitempty"`
}

// TranscriptToolCall is a stable view of a completed or rejected tool call.
type TranscriptToolCall struct {
	RequestID       string         `json:"request_id,omitempty"`
	ParentRequestID string         `json:"parent_request_id,omitempty"`
	Round           int            `json:"round,omitempty"`
	ID              string         `json:"id,omitempty"`
	Name            string         `json:"name,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	Approved        *bool          `json:"approved,omitempty"`
	ApprovalReason  string         `json:"approval_reason,omitempty"`
	Result          *ToolResult    `json:"result,omitempty"`
	ErrorCategory   ErrorCategory  `json:"error_category,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// TranscriptObservation is a stable observation view without nondeterministic timings.
type TranscriptObservation struct {
	Type                   EventType             `json:"type,omitempty"`
	AgentID                string                `json:"agent_id,omitempty"`
	RunID                  string                `json:"run_id,omitempty"`
	SubagentID             string                `json:"subagent_id,omitempty"`
	ToolName               string                `json:"tool_name,omitempty"`
	ToolRisk               ToolRisk              `json:"tool_risk,omitempty"`
	ToolSchemaHash         string                `json:"tool_schema_hash,omitempty"`
	SkillName              string                `json:"skill_name,omitempty"`
	TraceID                string                `json:"trace_id,omitempty"`
	SpanID                 string                `json:"span_id,omitempty"`
	TraceState             string                `json:"trace_state,omitempty"`
	RequestID              string                `json:"request_id,omitempty"`
	ParentRequestID        string                `json:"parent_request_id,omitempty"`
	Round                  int                   `json:"round,omitempty"`
	EstimatedTokens        int                   `json:"estimated_tokens,omitempty"`
	TokenUsage             TokenUsage            `json:"token_usage,omitempty"`
	StreamDeltaCount       int                   `json:"stream_delta_count,omitempty"`
	StreamByteCount        int                   `json:"stream_byte_count,omitempty"`
	ProviderDiagnostics    ProviderDiagnostics   `json:"provider_diagnostics,omitempty"`
	ToolResultContentBytes int                   `json:"tool_result_content_bytes,omitempty"`
	ToolResultMetadataKeys []string              `json:"tool_result_metadata_keys,omitempty"`
	ToolResultMCPIsError   *bool                 `json:"tool_result_mcp_is_error,omitempty"`
	ModelErrorSubcategory  ModelErrorSubcategory `json:"model_error_subcategory,omitempty"`
	Approved               bool                  `json:"approved,omitempty"`
	ApprovalReason         string                `json:"approval_reason,omitempty"`
	ErrorCategory          ErrorCategory         `json:"error_category,omitempty"`
	Failed                 bool                  `json:"failed,omitempty"`
}

// TranscriptSessionEvent is a stable replay view of SessionEvent.
type TranscriptSessionEvent struct {
	ID            string               `json:"id,omitempty"`
	SessionID     string               `json:"session_id,omitempty"`
	SchemaVersion int                  `json:"schema_version,omitempty"`
	Sequence      uint64               `json:"sequence,omitempty"`
	Type          SessionEventType     `json:"type,omitempty"`
	RunID         string               `json:"run_id,omitempty"`
	Metadata      []TranscriptMetadata `json:"metadata,omitempty"`
}

// TranscriptMetadata is one sorted metadata key/value pair.
type TranscriptMetadata struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TranscriptOutcome records the final run result.
type TranscriptOutcome struct {
	Message       *Message      `json:"message,omitempty"`
	Error         string        `json:"error,omitempty"`
	ErrorCategory ErrorCategory `json:"error_category,omitempty"`
}

// ToolCallExpectation describes a tool call assertion.
type ToolCallExpectation struct {
	Name          string
	ID            string
	Arguments     map[string]any
	ResultContent string
	Approved      *bool
	ErrorContains string
}

// ObservationExpectation describes an observation assertion.
type ObservationExpectation struct {
	Type          EventType
	ToolName      string
	Failed        bool
	ErrorCategory ErrorCategory
}

// AssertToolCalled fails the test if transcript does not contain toolName.
func AssertToolCalled(t testing.TB, transcript RunTranscript, toolName string) TranscriptToolCall {
	t.Helper()
	return AssertToolCall(t, transcript, ToolCallExpectation{Name: toolName})
}

// AssertToolCall fails the test if no tool call matches expectation.
func AssertToolCall(t testing.TB, transcript RunTranscript, expectation ToolCallExpectation) TranscriptToolCall {
	t.Helper()
	for _, call := range transcript.ToolCalls {
		if !toolCallMatchesExpectation(call, expectation) {
			continue
		}
		return call
	}
	t.Fatalf("tool calls = %#v, want match %#v", transcript.ToolCalls, expectation)
	return TranscriptToolCall{}
}

// AssertApprovalDenied fails the test if toolName was not denied by approval.
func AssertApprovalDenied(t testing.TB, transcript RunTranscript, toolName string) TranscriptToolCall {
	t.Helper()
	for _, call := range transcript.ToolCalls {
		if call.Name != toolName {
			continue
		}
		if call.Approved != nil && !*call.Approved {
			return call
		}
		if call.ErrorCategory == ErrorCategoryApproval || strings.Contains(call.Error, ErrApprovalDenied.Error()) {
			return call
		}
	}
	t.Fatalf("tool calls = %#v, want approval denial for %q", transcript.ToolCalls, toolName)
	return TranscriptToolCall{}
}

// AssertObservation fails the test if no observation matches expectation.
func AssertObservation(t testing.TB, transcript RunTranscript, expectation ObservationExpectation) TranscriptObservation {
	t.Helper()
	for _, observation := range transcript.Observations {
		if expectation.Type != "" && observation.Type != expectation.Type {
			continue
		}
		if expectation.ToolName != "" && observation.ToolName != expectation.ToolName {
			continue
		}
		if expectation.Failed && !observation.Failed {
			continue
		}
		if expectation.ErrorCategory != "" && observation.ErrorCategory != expectation.ErrorCategory {
			continue
		}
		return observation
	}
	t.Fatalf("observations = %#v, want match %#v", transcript.Observations, expectation)
	return TranscriptObservation{}
}

// AssertFinalMessage fails the test unless the transcript final message matches content.
func AssertFinalMessage(t testing.TB, transcript RunTranscript, content string) {
	t.Helper()
	if transcript.Final == nil || transcript.Final.Message == nil {
		t.Fatalf("final outcome = %#v, want message %q", transcript.Final, content)
	}
	if transcript.Final.Message.Content != content {
		t.Fatalf("final message content = %q, want %q", transcript.Final.Message.Content, content)
	}
}

// AssertFinalError fails the test unless the transcript final error contains text.
func AssertFinalError(t testing.TB, transcript RunTranscript, text string) {
	t.Helper()
	if transcript.Final == nil || transcript.Final.Error == "" {
		t.Fatalf("final outcome = %#v, want error containing %q", transcript.Final, text)
	}
	if !strings.Contains(transcript.Final.Error, text) {
		t.Fatalf("final error = %q, want to contain %q", transcript.Final.Error, text)
	}
}

// AssertEventOrder fails the test unless lifecycle event types appear in order.
func AssertEventOrder(t testing.TB, transcript RunTranscript, types ...EventType) {
	t.Helper()
	index := 0
	for _, want := range types {
		found := false
		for index < len(transcript.Events) {
			if transcript.Events[index].Type == want {
				found = true
				index++
				break
			}
			index++
		}
		if !found {
			t.Fatalf("event order = %#v, missing %s after index %d", transcriptEventTypes(transcript.Events), want, index)
		}
	}
}

// AssertSessionEventOrder fails the test unless replayed session event types appear in order.
func AssertSessionEventOrder(t testing.TB, transcript RunTranscript, types ...SessionEventType) {
	t.Helper()
	index := 0
	for _, want := range types {
		found := false
		for index < len(transcript.SessionEvents) {
			if transcript.SessionEvents[index].Type == want {
				found = true
				index++
				break
			}
			index++
		}
		if !found {
			t.Fatalf("session event order = %#v, missing %s after index %d", transcriptSessionEventTypes(transcript.SessionEvents), want, index)
		}
	}
}

func toolCallMatchesExpectation(call TranscriptToolCall, expectation ToolCallExpectation) bool {
	if expectation.Name != "" && call.Name != expectation.Name {
		return false
	}
	if expectation.ID != "" && call.ID != expectation.ID {
		return false
	}
	if expectation.Arguments != nil && !evalValuesEqual(call.Arguments, expectation.Arguments) {
		return false
	}
	if expectation.ResultContent != "" {
		if call.Result == nil || call.Result.Content != expectation.ResultContent {
			return false
		}
	}
	if expectation.Approved != nil {
		if call.Approved == nil || *call.Approved != *expectation.Approved {
			return false
		}
	}
	if expectation.ErrorContains != "" && !strings.Contains(call.Error, expectation.ErrorContains) {
		return false
	}
	return true
}

func evalValuesEqual(got, want any) bool {
	if reflect.DeepEqual(got, want) {
		return true
	}
	gotJSON, gotErr := json.Marshal(got)
	wantJSON, wantErr := json.Marshal(want)
	return gotErr == nil && wantErr == nil && string(gotJSON) == string(wantJSON)
}

func transcriptOutcome(message Message, err error) *TranscriptOutcome {
	outcome := &TranscriptOutcome{
		Error:         errorStringForEval(err),
		ErrorCategory: classifyError(err),
	}
	if !messageIsZeroForEval(message) {
		cloned := cloneMessage(message)
		outcome.Message = &cloned
	}
	if outcome.Message == nil && outcome.Error == "" && outcome.ErrorCategory == "" {
		return nil
	}
	return outcome
}

func transcriptEvents(events []Event) []TranscriptEvent {
	if len(events) == 0 {
		return nil
	}
	transcript := make([]TranscriptEvent, len(events))
	for i, event := range events {
		transcript[i] = transcriptEvent(event)
	}
	return transcript
}

func transcriptEvent(event Event) TranscriptEvent {
	return TranscriptEvent{
		Type:                  event.Type,
		AgentID:               event.AgentID,
		RunID:                 event.RunID,
		SubagentID:            event.SubagentID,
		ToolName:              event.ToolName,
		ToolRisk:              event.ToolRisk,
		ToolSchemaHash:        event.ToolSchemaHash,
		SkillName:             event.SkillName,
		TraceID:               event.TraceID,
		SpanID:                event.SpanID,
		TraceState:            event.TraceState,
		RequestID:             event.RequestID,
		ParentRequestID:       event.ParentRequestID,
		Round:                 event.Round,
		EstimatedTokens:       event.EstimatedTokens,
		TokenUsage:            event.TokenUsage,
		StreamDeltaCount:      event.StreamTelemetry.DeltaCount,
		StreamByteCount:       event.StreamTelemetry.ByteCount,
		ProviderDiagnostics:   event.ProviderDiagnostics,
		ModelErrorSubcategory: event.ModelErrorSubcategory,
		Approved:              event.Approved,
		ApprovalReason:        event.ApprovalReason,
		ErrorCategory:         event.ErrorCategory,
		Failed:                event.Error != nil || event.ErrorCategory != "",
		Error:                 errorStringForEval(event.Error),
		Message:               messagePointerForEval(event.Message),
		ToolCall:              toolCallPointerForEval(event.ToolCall),
		ToolResult:            toolResultPointerForEval(event.ToolResult),
	}
}

func transcriptToolCalls(events []Event) []TranscriptToolCall {
	if len(events) == 0 {
		return nil
	}
	approvals := make(map[string]Event)
	for _, event := range events {
		if event.Type == EventAfterApproval {
			approvals[event.RequestID] = event
		}
	}
	var calls []TranscriptToolCall
	for _, event := range events {
		if event.Type != EventAfterTool {
			continue
		}
		call := TranscriptToolCall{
			RequestID:       event.RequestID,
			ParentRequestID: event.ParentRequestID,
			Round:           event.Round,
			ID:              event.ToolCall.ID,
			Name:            firstNonEmpty(event.ToolCall.Name, event.ToolName),
			Arguments:       cloneAnyMap(event.ToolCall.Arguments),
			Result:          toolResultPointerForEval(event.ToolResult),
			ErrorCategory:   event.ErrorCategory,
			Error:           errorStringForEval(event.Error),
		}
		if approval, ok := approvals[event.RequestID]; ok {
			approved := approval.Approved
			call.Approved = &approved
			call.ApprovalReason = approval.ApprovalReason
		}
		calls = append(calls, call)
	}
	return calls
}

func transcriptObservations(observations []Observation) []TranscriptObservation {
	if len(observations) == 0 {
		return nil
	}
	transcript := make([]TranscriptObservation, len(observations))
	for i, observation := range observations {
		transcript[i] = TranscriptObservation{
			Type:                   observation.Type,
			AgentID:                observation.AgentID,
			RunID:                  observation.RunID,
			SubagentID:             observation.SubagentID,
			ToolName:               observation.ToolName,
			ToolRisk:               observation.ToolRisk,
			ToolSchemaHash:         observation.ToolSchemaHash,
			SkillName:              observation.SkillName,
			TraceID:                observation.TraceID,
			SpanID:                 observation.SpanID,
			TraceState:             observation.TraceState,
			RequestID:              observation.RequestID,
			ParentRequestID:        observation.ParentRequestID,
			Round:                  observation.Round,
			EstimatedTokens:        observation.EstimatedTokens,
			TokenUsage:             observation.TokenUsage,
			StreamDeltaCount:       observation.StreamTelemetry.DeltaCount,
			StreamByteCount:        observation.StreamTelemetry.ByteCount,
			ProviderDiagnostics:    observation.ProviderDiagnostics,
			ToolResultContentBytes: observation.ToolResultMetadata.ContentBytes,
			ToolResultMetadataKeys: append([]string(nil), observation.ToolResultMetadata.MetadataKeys...),
			ToolResultMCPIsError:   cloneBoolPointer(observation.ToolResultMetadata.MCPIsError),
			ModelErrorSubcategory:  observation.ModelErrorSubcategory,
			Approved:               observation.Approved,
			ApprovalReason:         observation.ApprovalReason,
			ErrorCategory:          observation.ErrorCategory,
			Failed:                 observation.Failed,
		}
	}
	return transcript
}

func modelRequestTranscript(request ModelRequest) TranscriptModelRequest {
	tools := make([]TranscriptToolDescriptor, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, TranscriptToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			Risk:        tool.Risk,
		})
	}
	mcpServers := make([]TranscriptMCPServer, 0, len(request.MCPServers))
	for _, server := range request.MCPServers {
		mcpServers = append(mcpServers, TranscriptMCPServer{
			Name:      server.Name,
			Transport: server.Transport,
		})
	}
	activeSkills := make([]TranscriptSkill, 0, len(request.ActiveSkills))
	for _, skill := range request.ActiveSkills {
		activeSkills = append(activeSkills, TranscriptSkill{
			Name:        skill.Name,
			Description: skill.Description,
		})
	}
	return TranscriptModelRequest{
		AgentID:      request.AgentID,
		SystemPrompt: request.SystemPrompt,
		Messages:     cloneMessages(request.Messages),
		Tools:        tools,
		MCPServers:   mcpServers,
		ActiveSkills: activeSkills,
	}
}

func modelResponseTranscript(response ModelResponse) TranscriptModelResponse {
	return TranscriptModelResponse{
		Message:   cloneMessage(response.Message),
		ToolCalls: cloneToolCalls(response.ToolCalls),
		Usage:     response.Usage,
	}
}

func transcriptStreamEvents(events []StreamEvent) []TranscriptStreamEvent {
	if len(events) == 0 {
		return nil
	}
	transcript := make([]TranscriptStreamEvent, len(events))
	for i, event := range events {
		transcript[i] = TranscriptStreamEvent{
			Type:    event.Type,
			Delta:   event.Delta,
			Message: cloneMessage(event.Message),
			Usage:   event.Usage,
			Error:   errorStringForEval(event.Error),
		}
	}
	return transcript
}

func transcriptSessionEvent(event SessionEvent) TranscriptSessionEvent {
	return TranscriptSessionEvent{
		ID:            event.ID,
		SessionID:     event.SessionID,
		SchemaVersion: event.SchemaVersion,
		Sequence:      event.Sequence,
		Type:          event.Type,
		RunID:         event.RunID,
		Metadata:      transcriptMetadata(event.Metadata),
	}
}

func transcriptMetadata(metadata map[string]string) []TranscriptMetadata {
	if len(metadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]TranscriptMetadata, 0, len(keys))
	for _, key := range keys {
		result = append(result, TranscriptMetadata{Key: key, Value: metadata[key]})
	}
	return result
}

func transcriptEventTypes(events []TranscriptEvent) []EventType {
	result := make([]EventType, len(events))
	for i, event := range events {
		result[i] = event.Type
	}
	return result
}

func transcriptSessionEventTypes(events []TranscriptSessionEvent) []SessionEventType {
	result := make([]SessionEventType, len(events))
	for i, event := range events {
		result[i] = event.Type
	}
	return result
}

func cloneScriptedModelSteps(steps []ScriptedModelStep) []ScriptedModelStep {
	if len(steps) == 0 {
		return nil
	}
	cloned := make([]ScriptedModelStep, len(steps))
	for i, step := range steps {
		cloned[i] = ScriptedModelStep{
			Response: cloneModelResponse(step.Response),
			Err:      step.Err,
		}
	}
	return cloned
}

func cloneScriptedStreamSteps(steps []ScriptedStreamStep) []ScriptedStreamStep {
	if len(steps) == 0 {
		return nil
	}
	cloned := make([]ScriptedStreamStep, len(steps))
	for i, step := range steps {
		cloned[i] = ScriptedStreamStep{
			Events: cloneStreamEventsForEval(step.Events),
			Err:    step.Err,
		}
	}
	return cloned
}

func cloneModelResponse(response ModelResponse) ModelResponse {
	return ModelResponse{
		Message:   cloneMessage(response.Message),
		ToolCalls: cloneToolCalls(response.ToolCalls),
		Usage:     response.Usage,
	}
}

func cloneModelRequestForEval(request ModelRequest) ModelRequest {
	return ModelRequest{
		AgentID:      request.AgentID,
		SystemPrompt: request.SystemPrompt,
		Messages:     cloneMessages(request.Messages),
		Tools:        append([]ToolDescriptor(nil), request.Tools...),
		MCPServers:   cloneMCPServers(request.MCPServers),
		ActiveSkills: cloneSkills(request.ActiveSkills),
	}
}

func cloneModelRequestsForEval(requests []ModelRequest) []ModelRequest {
	if len(requests) == 0 {
		return nil
	}
	cloned := make([]ModelRequest, len(requests))
	for i, request := range requests {
		cloned[i] = cloneModelRequestForEval(request)
	}
	return cloned
}

func cloneStreamEventsForEval(events []StreamEvent) []StreamEvent {
	if len(events) == 0 {
		return nil
	}
	cloned := make([]StreamEvent, len(events))
	for i, event := range events {
		cloned[i] = cloneStreamEventForEval(event)
	}
	return cloned
}

func cloneStreamEventForEval(event StreamEvent) StreamEvent {
	event.Message = cloneMessage(event.Message)
	return event
}

func cloneEventsForEval(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	cloned := make([]Event, len(events))
	for i, event := range events {
		cloned[i] = cloneEventForEval(event)
	}
	return cloned
}

func cloneEventForEval(event Event) Event {
	event.Message = cloneMessage(event.Message)
	event.ToolCall = cloneToolCall(event.ToolCall)
	event.ToolResult = cloneToolResult(event.ToolResult)
	return event
}

func cloneObservationsForEval(observations []Observation) []Observation {
	if len(observations) == 0 {
		return nil
	}
	cloned := make([]Observation, len(observations))
	for i, observation := range observations {
		cloned[i] = cloneObservationForEval(observation)
	}
	return cloned
}

func cloneObservationForEval(observation Observation) Observation {
	observation.ToolResultMetadata.MetadataKeys = append([]string(nil), observation.ToolResultMetadata.MetadataKeys...)
	observation.ToolResultMetadata.MCPIsError = cloneBoolPointer(observation.ToolResultMetadata.MCPIsError)
	return observation
}

func cloneTranscriptModelExchanges(exchanges []TranscriptModelExchange) []TranscriptModelExchange {
	if len(exchanges) == 0 {
		return nil
	}
	cloned := make([]TranscriptModelExchange, len(exchanges))
	copy(cloned, exchanges)
	for i := range cloned {
		cloned[i].Request = cloneTranscriptModelRequest(cloned[i].Request)
		if cloned[i].Response != nil {
			response := *cloned[i].Response
			response.Message = cloneMessage(response.Message)
			response.ToolCalls = cloneToolCalls(response.ToolCalls)
			cloned[i].Response = &response
		}
	}
	return cloned
}

func cloneTranscriptStreamExchanges(exchanges []TranscriptStreamExchange) []TranscriptStreamExchange {
	if len(exchanges) == 0 {
		return nil
	}
	cloned := make([]TranscriptStreamExchange, len(exchanges))
	copy(cloned, exchanges)
	for i := range cloned {
		cloned[i].Request = cloneTranscriptModelRequest(cloned[i].Request)
		cloned[i].Events = append([]TranscriptStreamEvent(nil), cloned[i].Events...)
		for j := range cloned[i].Events {
			cloned[i].Events[j].Message = cloneMessage(cloned[i].Events[j].Message)
		}
	}
	return cloned
}

func cloneTranscriptModelRequest(request TranscriptModelRequest) TranscriptModelRequest {
	request.Messages = cloneMessages(request.Messages)
	request.Tools = append([]TranscriptToolDescriptor(nil), request.Tools...)
	request.MCPServers = append([]TranscriptMCPServer(nil), request.MCPServers...)
	request.ActiveSkills = append([]TranscriptSkill(nil), request.ActiveSkills...)
	return request
}

func cloneTranscriptOutcome(outcome *TranscriptOutcome) *TranscriptOutcome {
	if outcome == nil {
		return nil
	}
	cloned := *outcome
	if outcome.Message != nil {
		message := cloneMessage(*outcome.Message)
		cloned.Message = &message
	}
	return &cloned
}

func messagePointerForEval(message Message) *Message {
	if messageIsZeroForEval(message) {
		return nil
	}
	cloned := cloneMessage(message)
	return &cloned
}

func toolCallPointerForEval(call ToolCall) *ToolCall {
	if call.ID == "" && call.Name == "" && len(call.Arguments) == 0 {
		return nil
	}
	cloned := cloneToolCall(call)
	return &cloned
}

func toolResultPointerForEval(result ToolResult) *ToolResult {
	if result.CallID == "" && result.Name == "" && result.Content == "" && len(result.Metadata) == 0 {
		return nil
	}
	cloned := cloneToolResult(result)
	return &cloned
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func messageIsZeroForEval(message Message) bool {
	return message.Role == "" &&
		message.Content == "" &&
		message.Name == "" &&
		message.ToolCallID == "" &&
		len(message.ToolCalls) == 0 &&
		len(message.Metadata) == 0
}

func modelResponseIsZero(response ModelResponse) bool {
	return messageIsZeroForEval(response.Message) &&
		len(response.ToolCalls) == 0 &&
		response.Usage == (TokenUsage{})
}

func errorStringForEval(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
