package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// newOwnerServiceTestStore returns a SQLite session store for service tests.
func newOwnerServiceTestStore(t *testing.T) session.Store {
	t.Helper()
	st, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSessionService_ListViewerFiltersOtherUsers(t *testing.T) {
	svc := NewSessionService(newOwnerServiceTestStore(t))
	ctx := context.Background()

	aSess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "alice s", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bSess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "bob s", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// User A lists only own (plus unowned) sessions.
	listA, err := svc.List(ctx, ListSessionsRequest{Viewer: session.NewViewer("user-a")})
	if err != nil {
		t.Fatalf("List(a): %v", err)
	}
	for _, s := range listA {
		if s.ID == bSess.ID {
			t.Error("user A can list user B's session")
		}
	}
	foundA := false
	for _, s := range listA {
		if s.ID == aSess.ID {
			foundA = true
		}
	}
	if !foundA {
		t.Error("user A cannot list own session")
	}

	// Nil viewer sees everything (legacy behavior).
	all, err := svc.List(ctx, ListSessionsRequest{})
	if err != nil {
		t.Fatalf("List(nil): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("nil viewer list = %d sessions, want 2", len(all))
	}
}

func TestSessionService_GetSessionViewable(t *testing.T) {
	svc := NewSessionService(newOwnerServiceTestStore(t))
	ctx := context.Background()

	bSess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "bob s", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// Another user's read of a foreign owned session is indistinguishable
	// from not-found.
	if _, err := svc.GetSessionViewable(ctx, GetSessionRequest{ID: bSess.ID}, session.NewViewer("user-a")); err == nil {
		t.Error("user A read user B's session via GetSessionViewable")
	}
	// Owner reads fine.
	if got, err := svc.GetSessionViewable(ctx, GetSessionRequest{ID: bSess.ID}, session.NewViewer("user-b")); err != nil || got == nil {
		t.Errorf("owner read failed: sess=%v err=%v", got, err)
	}
	// Nil viewer (legacy mode) reads everything.
	if got, err := svc.GetSessionViewable(ctx, GetSessionRequest{ID: bSess.ID}, nil); err != nil || got == nil {
		t.Errorf("nil-viewer read failed: sess=%v err=%v", got, err)
	}
}

func TestSessionService_CreateWithoutOwnerLeavesUnowned(t *testing.T) {
	svc := NewSessionService(newOwnerServiceTestStore(t))
	ctx := context.Background()

	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "legacy"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.OwnerID != "" {
		t.Errorf("OwnerID = %q, want empty in legacy path", sess.OwnerID)
	}
	// Any viewer still sees unowned sessions.
	listB, err := svc.List(ctx, ListSessionsRequest{Viewer: session.NewViewer("user-b")})
	if err != nil {
		t.Fatalf("List(b): %v", err)
	}
	if len(listB) != 1 || listB[0].ID != sess.ID {
		t.Errorf("unowned session invisible to viewer B: %d entries", len(listB))
	}
}
