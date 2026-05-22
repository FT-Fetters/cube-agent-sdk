package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSessionSnapshotJSONBackwardCompatibilityAndRestoreValidation(t *testing.T) {
	legacyPayload := []byte(`{"agent_id":"legacy","messages":[{"Role":"user","Content":"hello"}]}`)

	var snapshot SessionSnapshot
	if err := json.Unmarshal(legacyPayload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != CurrentSessionSchemaVersion {
		t.Fatalf("legacy snapshot schema version = %d, want current", snapshot.SchemaVersion)
	}
	if got, want := snapshot.Messages(), []Message{{Role: RoleUser, Content: "hello"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy snapshot messages = %#v, want %#v", got, want)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"schema_version":`) {
		t.Fatalf("encoded snapshot = %s, want schema_version metadata", encoded)
	}

	target, err := New(Config{ID: "target"}, &recordingModel{})
	if err != nil {
		t.Fatal(err)
	}
	future := snapshot
	future.SchemaVersion = CurrentSessionSchemaVersion + 1
	err = target.Restore(future)
	if !errors.Is(err, ErrSessionVersionMismatch) {
		t.Fatalf("restore future snapshot error = %v, want ErrSessionVersionMismatch", err)
	}
	var sessionErr *SessionPersistenceError
	if !errors.As(err, &sessionErr) || sessionErr.MigrationHint == "" {
		t.Fatalf("restore future snapshot error = %#v, want migration hint", err)
	}

	invalid := snapshot
	invalid.SchemaVersion = -1
	err = target.Restore(invalid)
	if !errors.Is(err, ErrSessionInvalidRecord) {
		t.Fatalf("restore invalid snapshot error = %v, want ErrSessionInvalidRecord", err)
	}

	invalidRole := NewSessionSnapshot("invalid", []Message{{Role: Role("invalid"), Content: "hello"}})
	err = target.Restore(invalidRole)
	if !errors.Is(err, ErrSessionInvalidRecord) {
		t.Fatalf("restore invalid role error = %v, want ErrSessionInvalidRecord", err)
	}
}

func TestMemorySessionStoreSaveLoadNotFoundVersionMismatchAndDeepCopy(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStore()

	if _, err := store.LoadSession(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("load missing error = %v, want ErrSessionNotFound", err)
	}

	snapshot := NewSessionSnapshot("agent-1", []Message{complexMessageForSessionTests()})
	record := NewSessionRecord("session-1", snapshot)
	record.Metadata = map[string]string{"tenant": "acme"}

	saved, err := store.SaveSession(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SchemaVersion != CurrentSessionSchemaVersion || saved.Version != 1 {
		t.Fatalf("saved version metadata = schema %d version %d, want current/1", saved.SchemaVersion, saved.Version)
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatalf("saved timestamps = created %s updated %s, want populated", saved.CreatedAt, saved.UpdatedAt)
	}

	record.Metadata["tenant"] = "changed"
	record.Snapshot.messages[0].Content = "changed"
	mutateSessionMessage(record.Snapshot.messages[0])

	loaded, err := store.LoadSession(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Metadata["tenant"] != "acme" {
		t.Fatalf("loaded metadata tenant = %q, want acme", loaded.Metadata["tenant"])
	}
	assertComplexSessionMessageUnchanged(t, loaded.Snapshot.Messages()[0])

	loaded.Metadata["tenant"] = "changed"
	loaded.Snapshot.messages[0].Content = "changed"
	mutateSessionMessage(loaded.Snapshot.messages[0])
	loadedAgain, err := store.LoadSession(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if loadedAgain.Metadata["tenant"] != "acme" {
		t.Fatalf("loaded-again metadata tenant = %q, want acme", loadedAgain.Metadata["tenant"])
	}
	assertComplexSessionMessageUnchanged(t, loadedAgain.Snapshot.Messages()[0])

	updated := saved
	updated.Metadata = map[string]string{"tenant": "beta"}
	updated, err = store.SaveSession(ctx, updated)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("updated session version = %d, want 2", updated.Version)
	}

	stale := saved
	stale.Metadata = map[string]string{"tenant": "stale"}
	errRecord, err := store.SaveSession(ctx, stale)
	if err == nil || errRecord.Version != 0 {
		t.Fatalf("stale save = record %#v error %v, want error and zero record", errRecord, err)
	}
	if !errors.Is(err, ErrSessionVersionMismatch) {
		t.Fatalf("stale save error = %v, want ErrSessionVersionMismatch", err)
	}

	future := NewSessionRecord("future", snapshot)
	future.SchemaVersion = CurrentSessionSchemaVersion + 1
	_, err = store.SaveSession(ctx, future)
	if !errors.Is(err, ErrSessionVersionMismatch) {
		t.Fatalf("future save error = %v, want ErrSessionVersionMismatch", err)
	}

	_, err = store.SaveSession(ctx, SessionRecord{ID: "   "})
	if !errors.Is(err, ErrSessionInvalidRecord) {
		t.Fatalf("invalid save error = %v, want ErrSessionInvalidRecord", err)
	}

	invalidRole := NewSessionRecord("invalid-role", NewSessionSnapshot("agent-1", []Message{{Role: Role("invalid"), Content: "hello"}}))
	_, err = store.SaveSession(ctx, invalidRole)
	if !errors.Is(err, ErrSessionInvalidRecord) {
		t.Fatalf("invalid role save error = %v, want ErrSessionInvalidRecord", err)
	}
}

func TestMemorySessionEventLogAppendOrderConflictAndDeepCopy(t *testing.T) {
	ctx := context.Background()
	log := NewMemorySessionStore()

	event := SessionEvent{
		SessionID: "session-1",
		Type:      SessionEventRunStarted,
		RunID:     "run-1",
		Metadata:  map[string]string{"agent_id": "agent-1"},
	}
	first, err := log.AppendSessionEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.Sequence != 1 || first.SchemaVersion != CurrentSessionSchemaVersion {
		t.Fatalf("first event = %#v, want stable ID, sequence 1, current schema", first)
	}
	if first.CreatedAt.IsZero() {
		t.Fatal("first event creation time was not recorded")
	}

	event.Metadata["agent_id"] = "changed"
	events, err := log.ListSessionEvents(ctx, "session-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[0].Metadata["agent_id"]; got != "agent-1" {
		t.Fatalf("stored event metadata agent_id = %q, want agent-1", got)
	}

	events[0].Metadata["agent_id"] = "changed"
	events, err = log.ListSessionEvents(ctx, "session-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[0].Metadata["agent_id"]; got != "agent-1" {
		t.Fatalf("reloaded event metadata agent_id = %q, want agent-1", got)
	}

	second, err := log.AppendSessionEvent(ctx, SessionEvent{
		ID:        "custom-event",
		SessionID: "session-1",
		Sequence:  2,
		Type:      SessionEventRunCompleted,
		RunID:     "run-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "custom-event" || second.Sequence != 2 {
		t.Fatalf("second event = %#v, want explicit ID and sequence 2", second)
	}

	events, err = log.ListSessionEvents(ctx, "session-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := []uint64{events[0].Sequence, events[1].Sequence}; !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("event order = %#v, want [1 2]", got)
	}

	events, err = log.ListSessionEvents(ctx, "session-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Sequence != 2 {
		t.Fatalf("events after sequence 1 = %#v, want only sequence 2", events)
	}

	_, err = log.AppendSessionEvent(ctx, SessionEvent{
		SessionID: "session-1",
		Sequence:  2,
		Type:      SessionEventRunStarted,
	})
	if !errors.Is(err, ErrSessionEventConflict) {
		t.Fatalf("sequence conflict error = %v, want ErrSessionEventConflict", err)
	}
	var sessionErr *SessionPersistenceError
	if !errors.As(err, &sessionErr) || sessionErr.ExpectedSequence != 3 || sessionErr.Sequence != 2 {
		t.Fatalf("sequence conflict detail = %#v, want expected 3 actual 2", err)
	}

	_, err = log.AppendSessionEvent(ctx, SessionEvent{SessionID: "session-1"})
	if !errors.Is(err, ErrSessionInvalidRecord) {
		t.Fatalf("invalid event error = %v, want ErrSessionInvalidRecord", err)
	}
}
