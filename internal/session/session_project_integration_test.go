// Package session provides session management for multi-client attachment.
package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

// TestSessionCreate_WithDetectionContext verifies that sessions created with
// a detection_context properly store the CWD and project binding info.
func TestSessionCreate_WithDetectionContext(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	// Create session with detection_context
	params := map[string]any{
		"name": "test-session",
		"detection_context": map[string]string{
			"cwd": "/tmp/test-project",
		},
	}
	paramsJSON, _ := json.Marshal(params)

	// Call handler
	result, err := handler.handleCreate(&models.BusMessage{
		Payload: paramsJSON,
	})
	if err != nil {
		t.Fatalf("handleCreate failed: %v", err)
	}

	// Verify result is a session
	sess, ok := result.(*Session)
	if !ok {
		t.Fatalf("expected *session.Session, got %T", result)
	}

	// Verify detection context was stored
	if sess.DetectionContext == nil {
		t.Fatal("expected DetectionContext to be set")
	}
	if sess.DetectionContext.CWD != "/tmp/test-project" {
		t.Errorf("expected CWD=/tmp/test-project, got %s", sess.DetectionContext.CWD)
	}
}

// TestSessionCreate_WithoutDetectionContext verifies backward compatibility -
// sessions can still be created without detection_context.
func TestSessionCreate_WithoutDetectionContext(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	params := map[string]string{"name": "test-session-no-cwd"}
	paramsJSON, _ := json.Marshal(params)

	result, err := handler.handleCreate(&models.BusMessage{
		Payload: paramsJSON,
	})
	if err != nil {
		t.Fatalf("handleCreate failed: %v", err)
	}

	sess, ok := result.(*Session)
	if !ok {
		t.Fatalf("expected *session.Session, got %T", result)
	}

	// DetectionContext should be nil when not provided
	if sess.DetectionContext != nil {
		t.Error("expected DetectionContext to be nil when not provided")
	}
	// Session should still be created successfully
	if sess.Name != "test-session-no-cwd" {
		t.Errorf("expected name=test-session-no-cwd, got %s", sess.Name)
	}
}

// TestSession_ProjectIsolation verifies that multiple sessions can have
// different project bindings without cross-contamination.
func TestSession_ProjectIsolation(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	// Create session A with project /tmp/project-a
	paramsA := map[string]any{
		"name": "session-a",
		"detection_context": map[string]string{
			"cwd": "/tmp/project-a",
		},
	}
	paramsJSON_A, _ := json.Marshal(paramsA)
	resultA, err := handler.handleCreate(&models.BusMessage{Payload: paramsJSON_A})
	if err != nil {
		t.Fatalf("handleCreate for session A failed: %v", err)
	}
	sessA := resultA.(*Session)

	// Create session B with project /tmp/project-b
	paramsB := map[string]any{
		"name": "session-b",
		"detection_context": map[string]string{
			"cwd": "/tmp/project-b",
		},
	}
	paramsJSON_B, _ := json.Marshal(paramsB)
	resultB, err := handler.handleCreate(&models.BusMessage{Payload: paramsJSON_B})
	if err != nil {
		t.Fatalf("handleCreate for session B failed: %v", err)
	}
	sessB := resultB.(*Session)

	// Verify isolation
	if sessA.DetectionContext.CWD == sessB.DetectionContext.CWD {
		t.Error("sessions should have different CWD values")
	}
	if sessA.DetectionContext.CWD != "/tmp/project-a" {
		t.Errorf("session A CWD = %s, want /tmp/project-a", sessA.DetectionContext.CWD)
	}
	if sessB.DetectionContext.CWD != "/tmp/project-b" {
		t.Errorf("session B CWD = %s, want /tmp/project-b", sessB.DetectionContext.CWD)
	}

	// Reload sessions and verify data persistence
	reloadedA := store.Get(sessA.ID)
	if reloadedA == nil {
		t.Fatal("failed to reload session A")
	}
	if reloadedA.DetectionContext == nil || reloadedA.DetectionContext.CWD != "/tmp/project-a" {
		t.Errorf("reloaded session A lost detection context: got %+v", reloadedA.DetectionContext)
	}
}

// TestGetMostRecentSession_WithProject verifies that GetMostRecent returns
// session with project data intact.
func TestGetMostRecentSession_WithProject(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())

	// Create a session with project
	params := map[string]any{
		"name": "recent-session",
		"detection_context": map[string]string{
			"cwd": "/tmp/recent-project",
		},
	}
	paramsJSON, _ := json.Marshal(params)
	_, err := handler.handleCreate(&models.BusMessage{Payload: paramsJSON})
	if err != nil {
		t.Fatalf("handleCreate failed: %v", err)
	}

	// Get most recent
	result, err := handler.handleGetMostRecent(nil)
	if err != nil {
		t.Fatalf("handleGetMostRecent failed: %v", err)
	}

	sess, ok := result.(*Session)
	if !ok {
		t.Fatalf("expected *session.Session, got %T", result)
	}

	if sess.DetectionContext == nil {
		t.Fatal("expected DetectionContext on most recent session")
	}
	if sess.DetectionContext.CWD != "/tmp/recent-project" {
		t.Errorf("expected CWD=/tmp/recent-project, got %s", sess.DetectionContext.CWD)
	}
}

// TestSQLiteStore_UpdateSessionsProjectPath verifies that bulk-updating
// session project_path values works correctly for the rename flow.
func TestSQLiteStore_UpdateSessionsProjectPath(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create a session with a project path.
	sess, err := store.Create("test-sess")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetProject(sess.ID, "proj-1", "/old/path"); err != nil {
		t.Fatalf("SetProject: %v", err)
	}

	// Update the project path for all sessions pointing at /old/path.
	if err := store.UpdateSessionsProjectPath(ctx, "/old/path", "/new/path"); err != nil {
		t.Fatalf("UpdateSessionsProjectPath: %v", err)
	}

	// Verify the session now has the new path.
	got := store.Get(sess.ID)
	if got == nil {
		t.Fatal("session not found")
	}
	if got.ProjectPath != "/new/path" {
		t.Errorf("ProjectPath = %q, want %q", got.ProjectPath, "/new/path")
	}
}

// TestSQLiteStore_UpdateSessionsProjectPath_MultiSession verifies that
// multiple sessions pointing at the same old path are all updated.
func TestSQLiteStore_UpdateSessionsProjectPath_MultiSession(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	// Create 3 sessions, 2 pointing at the old path, 1 at a different path.
	sess1, _ := store.Create("sess-1")
	sess2, _ := store.Create("sess-2")
	sess3, _ := store.Create("sess-3")

	store.SetProject(sess1.ID, "proj-1", "/old/path")
	store.SetProject(sess2.ID, "proj-1", "/old/path")
	store.SetProject(sess3.ID, "proj-2", "/other/path")

	if err := store.UpdateSessionsProjectPath(ctx, "/old/path", "/new/path"); err != nil {
		t.Fatalf("UpdateSessionsProjectPath: %v", err)
	}

	for _, id := range []string{sess1.ID, sess2.ID} {
		got := store.Get(id)
		if got == nil {
			t.Fatalf("session %s not found", id)
		}
		if got.ProjectPath != "/new/path" {
			t.Errorf("session %s: ProjectPath = %q, want %q", id, got.ProjectPath, "/new/path")
		}
	}

	// Session 3 should be untouched.
	got3 := store.Get(sess3.ID)
	if got3.ProjectPath != "/other/path" {
		t.Errorf("session 3: ProjectPath = %q, want %q", got3.ProjectPath, "/other/path")
	}
}

// TestSQLiteStore_UpdateSessionsProjectPath_ZeroMatches verifies that
// updating with no matching sessions is a no-op (no error).
func TestSQLiteStore_UpdateSessionsProjectPath_ZeroMatches(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()

	sess, _ := store.Create("sess-1")
	store.SetProject(sess.ID, "proj-1", "/some/path")

	// Update a path that doesn't match any session.
	err := store.UpdateSessionsProjectPath(ctx, "/nonexistent", "/new/path")
	if err != nil {
		t.Fatalf("expected nil error for zero matches, got: %v", err)
	}

	// Original session should be unchanged.
	got := store.Get(sess.ID)
	if got.ProjectPath != "/some/path" {
		t.Errorf("ProjectPath = %q, want %q", got.ProjectPath, "/some/path")
	}
}

// TestMemoryStore_UpdateSessionsProjectPath_MultiSession verifies the
// MemoryStore implementation also updates last_activity for parity.
func TestMemoryStore_UpdateSessionsProjectPath_MultiSession(t *testing.T) {
	store := NewMemoryStore(nil)
	ctx := context.Background()

	sess1, _ := store.Create("sess-1")
	sess2, _ := store.Create("sess-2")
	store.SetProject(sess1.ID, "proj-1", "/old/path")
	store.SetProject(sess2.ID, "proj-1", "/old/path")

	// Record pre-update activity.
	before1 := store.Get(sess1.ID).LastActivity

	if err := store.UpdateSessionsProjectPath(ctx, "/old/path", "/new/path"); err != nil {
		t.Fatalf("UpdateSessionsProjectPath: %v", err)
	}

	for _, id := range []string{sess1.ID, sess2.ID} {
		got := store.Get(id)
		if got == nil {
			t.Fatalf("session %s not found", id)
		}
		if got.ProjectPath != "/new/path" {
			t.Errorf("session %s: ProjectPath = %q, want %q", id, got.ProjectPath, "/new/path")
		}
		// last_activity should be bumped.
		if !got.LastActivity.After(before1) {
			t.Errorf("session %s: LastActivity not updated", id)
		}
	}
}
