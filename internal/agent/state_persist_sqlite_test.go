package agent

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func newStatePersistTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteStatePersister_SaveLoad(t *testing.T) {
	db := newStatePersistTestDB(t)
	p, err := NewSQLiteStatePersister(db)
	if err != nil {
		t.Fatalf("NewSQLiteStatePersister failed: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second) // truncate sub-second for JSON round-trip
	snap := AgentStateSnapshot{
		CurrentState:   "completed",
		IsTerminal:     true,
		IsActive:       false,
		ConversationID: "conv-test-1",
		AgentID:        "agent-007",
		CurrentTurn:    42,
		History: []StateTransition{
			{
				From:      StateIdle,
				To:        StateThinking,
				Reason:    "start",
				Metadata:  map[string]any{"foo": "bar"},
				Timestamp: now,
				TurnID:    1,
			},
			{
				From:      StateThinking,
				To:        StateCompleted,
				Reason:    "done",
				Metadata:  nil,
				Timestamp: now,
				TurnID:    2,
			},
		},
		Timestamp: now,
	}

	ctx := context.Background()
	if err := p.Save(ctx, "agent-007", snap); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := p.Load(ctx, "agent-007")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.CurrentState != snap.CurrentState {
		t.Errorf("CurrentState = %q, want %q", loaded.CurrentState, snap.CurrentState)
	}
	if loaded.ConversationID != snap.ConversationID {
		t.Errorf("ConversationID = %q, want %q", loaded.ConversationID, snap.ConversationID)
	}
	if loaded.AgentID != snap.AgentID {
		t.Errorf("AgentID = %q, want %q", loaded.AgentID, snap.AgentID)
	}
	if loaded.CurrentTurn != snap.CurrentTurn {
		t.Errorf("CurrentTurn = %d, want %d", loaded.CurrentTurn, snap.CurrentTurn)
	}
	if len(loaded.History) != 2 {
		t.Fatalf("History len = %d, want 2", len(loaded.History))
	}
	if loaded.History[0].From != StateIdle || loaded.History[0].To != StateThinking {
		t.Errorf("History[0] = %+v", loaded.History[0])
	}
	if loaded.History[1].From != StateThinking || loaded.History[1].To != StateCompleted {
		t.Errorf("History[1] = %+v", loaded.History[1])
	}
	if !loaded.Timestamp.Equal(snap.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", loaded.Timestamp, snap.Timestamp)
	}
}

func TestSQLiteStatePersister_Upsert(t *testing.T) {
	db := newStatePersistTestDB(t)
	p, err := NewSQLiteStatePersister(db)
	if err != nil {
		t.Fatalf("NewSQLiteStatePersister failed: %v", err)
	}

	ctx := context.Background()
	snap1 := AgentStateSnapshot{
		CurrentState:   "thinking",
		ConversationID: "conv-1",
		AgentID:        "agent-upsert",
		CurrentTurn:    1,
		Timestamp:      time.Now(),
	}
	snap2 := AgentStateSnapshot{
		CurrentState:   "completed",
		ConversationID: "conv-2",
		AgentID:        "agent-upsert",
		CurrentTurn:    5,
		Timestamp:      time.Now(),
	}

	if err := p.Save(ctx, "agent-upsert", snap1); err != nil {
		t.Fatalf("Save(1) failed: %v", err)
	}
	if err := p.Save(ctx, "agent-upsert", snap2); err != nil {
		t.Fatalf("Save(2) failed: %v", err)
	}

	loaded, err := p.Load(ctx, "agent-upsert")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.CurrentState != "completed" {
		t.Errorf("after upsert, CurrentState = %q, want %q", loaded.CurrentState, "completed")
	}
	if loaded.ConversationID != "conv-2" {
		t.Errorf("after upsert, ConversationID = %q, want %q", loaded.ConversationID, "conv-2")
	}
	if loaded.CurrentTurn != 5 {
		t.Errorf("after upsert, CurrentTurn = %d, want 5", loaded.CurrentTurn)
	}
}

func TestSQLiteStatePersister_LoadMissing(t *testing.T) {
	db := newStatePersistTestDB(t)
	p, err := NewSQLiteStatePersister(db)
	if err != nil {
		t.Fatalf("NewSQLiteStatePersister failed: %v", err)
	}

	ctx := context.Background()
	_, err = p.Load(ctx, "nonexistent-agent")
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestSQLiteStatePersister_Delete(t *testing.T) {
	db := newStatePersistTestDB(t)
	p, err := NewSQLiteStatePersister(db)
	if err != nil {
		t.Fatalf("NewSQLiteStatePersister failed: %v", err)
	}

	ctx := context.Background()
	snap := AgentStateSnapshot{
		CurrentState: "idle",
		AgentID:      "agent-delete",
		Timestamp:    time.Now(),
	}
	if err := p.Save(ctx, "agent-delete", snap); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify it exists
	loaded, err := p.Load(ctx, "agent-delete")
	if err != nil {
		t.Fatalf("Load before delete failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot before delete")
	}

	// Delete
	if err := p.Delete(ctx, "agent-delete"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = p.Load(ctx, "agent-delete")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("after delete, expected sql.ErrNoRows, got %v", err)
	}
}

func TestSQLiteStatePersister_NilDB(t *testing.T) {
	_, err := NewSQLiteStatePersister(nil)
	if err == nil {
		t.Fatal("expected error for nil db, got nil")
	}
}

// --- AgentLoop setter test ---

func TestAgentLoop_SetStatePersister(t *testing.T) {
	loop := NewAgentLoop("test-persister-session", "/tmp/test")
	if loop.statePersister != nil {
		t.Fatal("new loop should have nil statePersister")
	}

	mock := &mockPersister{}
	loop.SetStatePersister(mock)
	if loop.statePersister != mock {
		t.Error("after SetStatePersister(mock), field should be set to mock")
	}

	// Nil must NOT overwrite the existing persister (nil-guard).
	loop.SetStatePersister(nil)
	if loop.statePersister != mock {
		t.Error("SetStatePersister(nil) should NOT overwrite existing persister")
	}
}

// mockPersister is a minimal AgentStatePersister for testing the setter.
type mockPersister struct {
	saveCalled   bool
	loadCalled   bool
	deleteCalled bool
	lastSnapshot AgentStateSnapshot
}

func (m *mockPersister) Save(_ context.Context, _ string, snap AgentStateSnapshot) error {
	m.saveCalled = true
	m.lastSnapshot = snap
	return nil
}

func (m *mockPersister) Load(_ context.Context, _ string) (*AgentStateSnapshot, error) {
	m.loadCalled = true
	return &m.lastSnapshot, nil
}

func (m *mockPersister) Delete(_ context.Context, _ string) error {
	m.deleteCalled = true
	return nil
}
