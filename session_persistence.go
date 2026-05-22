package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CurrentSessionSchemaVersion is the JSON schema version used by session snapshots, records, and events.
const CurrentSessionSchemaVersion = 1

const sessionMigrationHint = "load this record with a newer SDK or migrate it to the current session schema before restoring"

var (
	// ErrSessionNotFound marks missing durable session records.
	ErrSessionNotFound = errors.New("agent: session not found")
	// ErrSessionVersionMismatch marks unsupported schema versions or stale session record versions.
	ErrSessionVersionMismatch = errors.New("agent: session version mismatch")
	// ErrSessionInvalidRecord marks malformed snapshots, records, or event log entries.
	ErrSessionInvalidRecord = errors.New("agent: invalid session record")
	// ErrSessionEventConflict marks append-only event log sequence or ID conflicts.
	ErrSessionEventConflict = errors.New("agent: session event sequence conflict")
)

// SessionPersistenceError adds safe context around session store and event log failures.
// It intentionally excludes prompts, messages, tool arguments, credentials, and raw payloads.
type SessionPersistenceError struct {
	Operation              string
	SessionID              string
	EventID                string
	Sequence               uint64
	ExpectedSequence       uint64
	Version                uint64
	ExpectedVersion        uint64
	SchemaVersion          int
	SupportedSchemaVersion int
	MigrationHint          string
	Cause                  error
}

func (e *SessionPersistenceError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"agent: session"}
	if e.Operation != "" {
		parts = append(parts, e.Operation)
	}
	if e.SessionID != "" {
		parts = append(parts, "session_id="+e.SessionID)
	}
	if e.EventID != "" {
		parts = append(parts, "event_id="+e.EventID)
	}
	if e.Sequence != 0 {
		parts = append(parts, fmt.Sprintf("sequence=%d", e.Sequence))
	}
	if e.ExpectedSequence != 0 {
		parts = append(parts, fmt.Sprintf("expected_sequence=%d", e.ExpectedSequence))
	}
	if e.Version != 0 {
		parts = append(parts, fmt.Sprintf("version=%d", e.Version))
	}
	if e.ExpectedVersion != 0 {
		parts = append(parts, fmt.Sprintf("expected_version=%d", e.ExpectedVersion))
	}
	if e.SchemaVersion != 0 {
		parts = append(parts, fmt.Sprintf("schema_version=%d", e.SchemaVersion))
	}
	if e.SupportedSchemaVersion != 0 {
		parts = append(parts, fmt.Sprintf("supported_schema_version=%d", e.SupportedSchemaVersion))
	}
	message := strings.Join(parts, " ")
	if e.Cause == nil {
		return message
	}
	return message + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel cause for errors.Is checks.
func (e *SessionPersistenceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// SessionRecord is the durable unit saved by a SessionStore.
// It contains conversation state and safe application metadata, not provider credentials or runtime config.
type SessionRecord struct {
	ID            string            `json:"id"`
	SchemaVersion int               `json:"schema_version"`
	Version       uint64            `json:"version"`
	Snapshot      SessionSnapshot   `json:"snapshot"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
	MigrationHint string            `json:"migration_hint,omitempty"`
}

// NewSessionRecord wraps a snapshot with durable storage metadata.
func NewSessionRecord(id string, snapshot SessionSnapshot) SessionRecord {
	now := time.Now().UTC()
	snapshot = cloneSessionSnapshot(snapshot)
	return SessionRecord{
		ID:            strings.TrimSpace(id),
		SchemaVersion: CurrentSessionSchemaVersion,
		Version:       1,
		Snapshot:      snapshot,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// SessionStore defines durable session persistence without binding the SDK to a database.
type SessionStore interface {
	SaveSession(context.Context, SessionRecord) (SessionRecord, error)
	LoadSession(context.Context, string) (SessionRecord, error)
	DeleteSession(context.Context, string) error
}

// SessionEventType identifies append-only lifecycle records for a session event log.
type SessionEventType string

const (
	// SessionEventRunStarted records that an application-level run began.
	SessionEventRunStarted SessionEventType = "run.started"
	// SessionEventRunCompleted records that an application-level run completed.
	SessionEventRunCompleted SessionEventType = "run.completed"
	// SessionEventRunFailed records that an application-level run failed.
	SessionEventRunFailed SessionEventType = "run.failed"
	// SessionEventSnapshotSaved records that a session snapshot was persisted.
	SessionEventSnapshotSaved SessionEventType = "snapshot.saved"
)

// SessionEvent is a safe append-only event log record.
// Metadata should contain only caller-approved labels, IDs, hashes, and routing data.
type SessionEvent struct {
	ID            string            `json:"id"`
	SessionID     string            `json:"session_id"`
	SchemaVersion int               `json:"schema_version"`
	Sequence      uint64            `json:"sequence"`
	Type          SessionEventType  `json:"type"`
	RunID         string            `json:"run_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at,omitempty"`
}

// SessionEventLog defines an append-only event stream for session lifecycle records.
type SessionEventLog interface {
	AppendSessionEvent(context.Context, SessionEvent) (SessionEvent, error)
	ListSessionEvents(context.Context, string, uint64) ([]SessionEvent, error)
}

// MemorySessionStore is a concurrency-safe in-memory SessionStore and SessionEventLog.
// It is intended for tests, examples, and as a reference for database-backed adapters.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionRecord
	events   map[string][]SessionEvent
}

var _ SessionStore = (*MemorySessionStore)(nil)
var _ SessionEventLog = (*MemorySessionStore)(nil)

// NewMemorySessionStore constructs an empty in-memory session store and event log.
func NewMemorySessionStore() *MemorySessionStore {
	store := &MemorySessionStore{}
	store.initLocked()
	return store
}

// SaveSession validates and stores a deep copy of record, incrementing Version on updates.
func (s *MemorySessionStore) SaveSession(ctx context.Context, record SessionRecord) (SessionRecord, error) {
	if err := sessionContextErr(ctx); err != nil {
		return SessionRecord{}, err
	}
	normalized, err := normalizeSessionRecord(record)
	if err != nil {
		return SessionRecord{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()

	now := time.Now().UTC()
	if existing, ok := s.sessions[normalized.ID]; ok {
		if normalized.Version != 0 && normalized.Version != existing.Version {
			return SessionRecord{}, &SessionPersistenceError{
				Operation:       "save",
				SessionID:       normalized.ID,
				Version:         normalized.Version,
				ExpectedVersion: existing.Version,
				MigrationHint:   "reload the latest session record before saving a new version",
				Cause:           ErrSessionVersionMismatch,
			}
		}
		normalized.Version = existing.Version + 1
		normalized.CreatedAt = existing.CreatedAt
	} else if normalized.Version == 0 {
		normalized.Version = 1
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now

	s.sessions[normalized.ID] = cloneSessionRecord(normalized)
	return cloneSessionRecord(normalized), nil
}

// LoadSession returns a deep copy of a stored session record.
func (s *MemorySessionStore) LoadSession(ctx context.Context, id string) (SessionRecord, error) {
	if err := sessionContextErr(ctx); err != nil {
		return SessionRecord{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return SessionRecord{}, invalidSessionError("load", "", "session ID is required")
	}

	s.mu.RLock()
	record, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return SessionRecord{}, &SessionPersistenceError{
			Operation: "load",
			SessionID: id,
			Cause:     ErrSessionNotFound,
		}
	}
	cloned := cloneSessionRecord(record)
	if err := ValidateSessionRecord(cloned); err != nil {
		return SessionRecord{}, err
	}
	return cloned, nil
}

// DeleteSession removes a stored session record and its in-memory event log entries.
func (s *MemorySessionStore) DeleteSession(ctx context.Context, id string) error {
	if err := sessionContextErr(ctx); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return invalidSessionError("delete", "", "session ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return &SessionPersistenceError{
			Operation: "delete",
			SessionID: id,
			Cause:     ErrSessionNotFound,
		}
	}
	delete(s.sessions, id)
	delete(s.events, id)
	return nil
}

// AppendSessionEvent appends event at the next sequence and returns the stored copy.
func (s *MemorySessionStore) AppendSessionEvent(ctx context.Context, event SessionEvent) (SessionEvent, error) {
	if err := sessionContextErr(ctx); err != nil {
		return SessionEvent{}, err
	}
	normalized, err := normalizeSessionEvent(event)
	if err != nil {
		return SessionEvent{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked()

	events := s.events[normalized.SessionID]
	nextSequence := uint64(len(events) + 1)
	if normalized.Sequence == 0 {
		normalized.Sequence = nextSequence
	} else if normalized.Sequence != nextSequence {
		return SessionEvent{}, &SessionPersistenceError{
			Operation:        "append_event",
			SessionID:        normalized.SessionID,
			EventID:          normalized.ID,
			Sequence:         normalized.Sequence,
			ExpectedSequence: nextSequence,
			Cause:            ErrSessionEventConflict,
		}
	}
	if normalized.ID == "" {
		normalized.ID = fmt.Sprintf("%s:%d", normalized.SessionID, normalized.Sequence)
	}
	for _, existing := range events {
		if existing.ID == normalized.ID {
			return SessionEvent{}, &SessionPersistenceError{
				Operation: "append_event",
				SessionID: normalized.SessionID,
				EventID:   normalized.ID,
				Sequence:  normalized.Sequence,
				Cause:     ErrSessionEventConflict,
			}
		}
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = time.Now().UTC()
	}

	s.events[normalized.SessionID] = append(events, cloneSessionEvent(normalized))
	return cloneSessionEvent(normalized), nil
}

// ListSessionEvents returns events for sessionID with Sequence greater than afterSequence.
func (s *MemorySessionStore) ListSessionEvents(ctx context.Context, sessionID string, afterSequence uint64) ([]SessionEvent, error) {
	if err := sessionContextErr(ctx); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, invalidSessionError("list_events", "", "session ID is required")
	}

	s.mu.RLock()
	events := s.events[sessionID]
	filtered := make([]SessionEvent, 0, len(events))
	for _, event := range events {
		if event.Sequence > afterSequence {
			filtered = append(filtered, cloneSessionEvent(event))
		}
	}
	s.mu.RUnlock()
	return filtered, nil
}

// ValidateSessionRecord checks schema metadata, IDs, and the nested snapshot before restore or storage.
func ValidateSessionRecord(record SessionRecord) error {
	_, err := normalizeSessionRecord(record)
	return err
}

// ValidateSessionSnapshot checks whether snapshot can be restored by this SDK version.
func ValidateSessionSnapshot(snapshot SessionSnapshot) error {
	return validateSessionSnapshot("restore", "", snapshot)
}

func (s *MemorySessionStore) initLocked() {
	if s.sessions == nil {
		s.sessions = make(map[string]SessionRecord)
	}
	if s.events == nil {
		s.events = make(map[string][]SessionEvent)
	}
}

func normalizeSessionRecord(record SessionRecord) (SessionRecord, error) {
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		return SessionRecord{}, invalidSessionError("validate", "", "session ID is required")
	}
	if err := validateSessionSchemaVersion("validate", record.ID, record.SchemaVersion); err != nil {
		return SessionRecord{}, err
	}
	record.SchemaVersion = normalizeSessionSchemaVersion(record.SchemaVersion)
	record.Snapshot = cloneSessionSnapshot(record.Snapshot)
	if record.Snapshot.SchemaVersion == 0 {
		record.Snapshot.SchemaVersion = record.SchemaVersion
	}
	if err := validateSessionSnapshot("validate", record.ID, record.Snapshot); err != nil {
		return SessionRecord{}, err
	}
	record.Metadata = cloneStringMap(record.Metadata)
	return record, nil
}

func validateSessionSnapshot(operation, sessionID string, snapshot SessionSnapshot) error {
	if err := validateSessionSchemaVersion(operation, sessionID, snapshot.SchemaVersion); err != nil {
		return err
	}
	for i, message := range snapshot.messages {
		if !isValidSessionRole(message.Role) {
			return &SessionPersistenceError{
				Operation: operation,
				SessionID: sessionID,
				Cause:     fmt.Errorf("%w: message %d has invalid role", ErrSessionInvalidRecord, i),
			}
		}
	}
	return nil
}

func isValidSessionRole(role Role) bool {
	switch role {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}

func validateSessionSchemaVersion(operation, sessionID string, version int) error {
	if version < 0 {
		return &SessionPersistenceError{
			Operation:     operation,
			SessionID:     sessionID,
			SchemaVersion: version,
			Cause:         ErrSessionInvalidRecord,
		}
	}
	actual := normalizeSessionSchemaVersion(version)
	if actual != CurrentSessionSchemaVersion {
		return &SessionPersistenceError{
			Operation:              operation,
			SessionID:              sessionID,
			SchemaVersion:          actual,
			SupportedSchemaVersion: CurrentSessionSchemaVersion,
			MigrationHint:          sessionMigrationHint,
			Cause:                  ErrSessionVersionMismatch,
		}
	}
	return nil
}

func normalizeSessionEvent(event SessionEvent) (SessionEvent, error) {
	event.SessionID = strings.TrimSpace(event.SessionID)
	if event.SessionID == "" {
		return SessionEvent{}, invalidSessionError("append_event", "", "session ID is required")
	}
	if strings.TrimSpace(string(event.Type)) == "" {
		return SessionEvent{}, invalidSessionError("append_event", event.SessionID, "event type is required")
	}
	if err := validateSessionSchemaVersion("append_event", event.SessionID, event.SchemaVersion); err != nil {
		return SessionEvent{}, err
	}
	event.ID = strings.TrimSpace(event.ID)
	event.Type = SessionEventType(strings.TrimSpace(string(event.Type)))
	event.SchemaVersion = normalizeSessionSchemaVersion(event.SchemaVersion)
	event.Metadata = cloneStringMap(event.Metadata)
	return event, nil
}

func normalizeSessionSchemaVersion(version int) int {
	if version == 0 {
		return CurrentSessionSchemaVersion
	}
	return version
}

func invalidSessionError(operation, sessionID, message string) error {
	return &SessionPersistenceError{
		Operation: operation,
		SessionID: sessionID,
		Cause:     fmt.Errorf("%w: %s", ErrSessionInvalidRecord, message),
	}
}

func sessionContextErr(ctx context.Context) error {
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

func cloneSessionRecord(record SessionRecord) SessionRecord {
	record.Snapshot = cloneSessionSnapshot(record.Snapshot)
	record.Metadata = cloneStringMap(record.Metadata)
	return record
}

func cloneSessionSnapshot(snapshot SessionSnapshot) SessionSnapshot {
	return SessionSnapshot{
		SchemaVersion: normalizeSessionSchemaVersion(snapshot.SchemaVersion),
		AgentID:       snapshot.AgentID,
		CreatedAt:     snapshot.CreatedAt,
		messages:      cloneMessages(snapshot.messages),
	}
}

func cloneSessionEvent(event SessionEvent) SessionEvent {
	event.Metadata = cloneStringMap(event.Metadata)
	return event
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
