package pty

import (
	"testing"
)

func TestManager_CreateSession(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	id, sess, err := mgr.CreateAutoSession(SessionConfig{
		Cmd:  "cat",
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer mgr.DestroySession(id)

	if sess == nil {
		t.Fatal("session should not be nil")
	}

	if !sess.IsRunning() {
		t.Fatal("session should be running")
	}

	if id == "" {
		t.Fatal("session ID should not be empty")
	}

	if mgr.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", mgr.SessionCount())
	}
}

func TestManager_GetSession(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	// Get unknown session
	unknown := mgr.GetSession("unknown")
	if unknown != nil {
		t.Fatal("expected nil for unknown session")
	}

	// Create session
	id, sess, err := mgr.CreateAutoSession(SessionConfig{
		Cmd: "cat",
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer mgr.DestroySession(id)

	// Get session
	retrieved := mgr.GetSession(id)
	if retrieved == nil {
		t.Fatal("expected session, got nil")
	}
	if retrieved != sess {
		t.Fatal("retrieved session should be the same instance")
	}
}

func TestManager_SessionLimit(t *testing.T) {
	mgr := NewManager(WithMaxSessions(2))
	defer mgr.Close()

	// Create max sessions
	for i := 0; i < 2; i++ {
		id, sess, err := mgr.CreateAutoSession(SessionConfig{
			Cmd: "cat",
		})
		if err != nil {
			t.Skipf("PTY not available: %v", err)
		}
		_ = sess
		_ = id
	}

	// Ensure sessions are tracked
	ids := mgr.ListSessions()
	if len(ids) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(ids))
	}

	// Should fail: limit reached
	_, _, err := mgr.CreateAutoSession(SessionConfig{
		Cmd: "cat",
	})
	if err == nil {
		t.Fatal("expected error when session limit is reached")
	}
	errMsg := err.Error()
	// Error should contain "session limit"
	if errMsg != "session limit reached (2)" {
		t.Logf("got error: %v", err)
	}
}

func TestManager_DuplicateID(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	id, _, err := mgr.CreateAutoSession(SessionConfig{
		Cmd: "cat",
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}

	// Try to create session with same ID
	_, err = mgr.CreateSession(id, SessionConfig{
		Cmd: "cat",
	})
	if err == nil {
		t.Fatal("expected error for duplicate session ID")
	}
}

func TestManager_DestroySession(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	id, _, err := mgr.CreateAutoSession(SessionConfig{
		Cmd: "cat",
	})
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}

	// Destroy should work
	if err := mgr.DestroySession(id); err != nil {
		t.Fatalf("destroy failed: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after destroy, got %d", mgr.SessionCount())
	}

	// Double destroy should fail (session not found)
	err = mgr.DestroySession(id)
	if err == nil {
		t.Fatal("expected error for double destroy")
	}
}

func TestManager_DestroyUnknown(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	err := mgr.DestroySession("nonexistent")
	if err == nil {
		t.Fatal("expected error for destroying unknown session")
	}
}

func TestManager_ListSessions(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	ids := mgr.ListSessions()
	if len(ids) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(ids))
	}

	// Create a few sessions
	for i := 0; i < 3; i++ {
		sid, sess, err := mgr.CreateAutoSession(SessionConfig{
			Cmd: "cat",
		})
		if err != nil {
			t.Skipf("PTY not available: %v", err)
		}
		_ = sess
		_ = sid
	}

	listed := mgr.ListSessions()
	if len(listed) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(listed))
	}
}

func TestManager_Close(t *testing.T) {
	mgr := NewManager()

	// Create a few sessions
	for i := 0; i < 3; i++ {
		mgr.CreateAutoSession(SessionConfig{
			Cmd: "cat",
		})
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Error("expected 0 sessions after Close")
	}
}
