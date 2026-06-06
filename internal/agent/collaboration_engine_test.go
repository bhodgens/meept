package agent

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

func TestNewCollaborationEngine(t *testing.T) {
	b := bus.New(nil, nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	e := NewCollaborationEngine(CollaborationEngineDeps{Bus: b, Logger: logger})

	if e.modes == nil {
		t.Error("modes map should be initialized")
	}
	if e.sessions == nil {
		t.Error("sessions map should be initialized")
	}
}

func TestCollaborationEngine_RegisterMode(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})
	driver := NewPairProgrammingDriver(PairProgrammingDriverDeps{})

	e.RegisterMode("pair_programming", driver)

	m, ok := e.GetMode("pair_programming")
	if !ok {
		t.Fatal("mode not found after registration")
	}
	if m.Name() != "pair_programming" {
		t.Errorf("name = %q, want pair_programming", m.Name())
	}
}

func TestCollaborationEngine_CreateSession(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})
	sess, err := e.CreateSession("pair_programming", "task-42", []string{"coder", "planner"}, DefaultSessionConfig())
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.Mode != "pair_programming" {
		t.Errorf("mode = %q, want pair_programming", sess.Mode)
	}

	got, ok := e.GetSession(sess.ID)
	if !ok {
		t.Fatal("session not found after creation")
	}
	if got.TaskID != "task-42" {
		t.Errorf("task_id = %q, want task-42", got.TaskID)
	}
}

func TestCollaborationEngine_CreateNestedSession(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})
	parent, _ := e.CreateSession("pair_programming", "task-parent", []string{"coder"}, DefaultSessionConfig())
	e.mu.Lock()
	e.nestedCount[parent.ID] = MaxCollaborationDepth
	e.mu.Unlock()

	_, err := e.CreateNestedSession(parent.ID, "pair_programming", "subtask", []string{"planner"}, DefaultSessionConfig())
	if err == nil {
		t.Fatal("expected depth exceeded error")
	}
	if err != ErrDepthExceeded {
		t.Errorf("expected ErrDepthExceeded, got %v", err)
	}
}

func TestCollaborationEngine_ResolveParticipants(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})

	parts := e.resolveParticipants("pair_programming", []string{"a", "b"})
	if len(parts) != 2 || parts[0] != "a" || parts[1] != "b" {
		t.Errorf("unexpected participants: %v", parts)
	}

	parts2 := e.resolveParticipants("pair_programming", []string{"a"})
	if len(parts2) < 2 {
		t.Errorf("expected at least 2 participants, got %v", parts2)
	}

	parts3 := e.resolveParticipants("differential", []string{})
	if len(parts3) < 3 {
		t.Errorf("expected at least 3 participants for differential, got %v", parts3)
	}
}

func TestCollaborationEngine_ActiveSessionCount(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})

	sess, _ := e.CreateSession("pair_programming", "t1", []string{"a", "b"}, DefaultSessionConfig())
	if e.ActiveSessionCount() != 1 {
		t.Errorf("active count = %d, want 1", e.ActiveSessionCount())
	}

	sess.MarkConverged()
	if e.ActiveSessionCount() != 0 {
		t.Errorf("active count = %d, want 0 after terminal", e.ActiveSessionCount())
	}
}

func TestCollaborationEngine_ListSessions(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})
	_, _ = e.CreateSession("pair_programming", "t1", []string{"a", "b"}, DefaultSessionConfig())

	all := e.ListSessions(false)
	if len(all) != 1 {
		t.Errorf("len(all) = %d, want 1", len(all))
	}

	active := e.ListSessions(true)
	if len(active) != 1 {
		t.Errorf("len(active) = %d, want 1", len(active))
	}
}

func TestCollaborationEngine_RunSession_MissingMode(t *testing.T) {
	e := NewCollaborationEngine(CollaborationEngineDeps{})
	sess, _ := e.CreateSession("nonexistent", "t1", []string{"a", "b"}, DefaultSessionConfig())

	_, err := e.RunSession(context.Background(), sess.ID)
	if err == nil {
		t.Fatal("expected error for unregistered mode")
	}
}
