package services

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// TestSessionCreate_WithDetectionContext tests session creation with detection context.
func TestSessionCreate_WithDetectionContext(t *testing.T) {
	t.Parallel()

	svc := NewSessionService(session.NewMemoryStore(slog.Default()))
	ctx := context.Background()

	// Create session normally (no detection context)
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test-session"})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Initially no detection context
	if sess.DetectionContext != nil {
		t.Error("expected nil DetectionContext on fresh session")
	}

	// Verify GetSession applies migration (should still be nil since no ProjectPath)
	got, err := svc.GetSession(ctx, GetSessionRequest{ID: sess.ID})
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.DetectionContext != nil {
		t.Error("expected nil DetectionContext when ProjectPath is empty")
	}
}

// TestSession_Isolation tests that sessions with different projects are isolated.
func TestSession_Isolation(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(slog.Default())
	svc := NewSessionService(store)
	ctx := context.Background()

	// Create two sessions
	sess1, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "project-a-session"})
	if err != nil {
		t.Fatalf("CreateSession sess1 failed: %v", err)
	}

	sess2, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "project-b-session"})
	if err != nil {
		t.Fatalf("CreateSession sess2 failed: %v", err)
	}

	// Manually set different project paths (simulating what RPC would do)
	sess1.ProjectPath = "/tmp/project-a"
	sess1.ProjectID = "project-a"
	sess2.ProjectPath = "/tmp/project-b"
	sess2.ProjectID = "project-b"

	// Verify they're different sessions
	if sess1.ID == sess2.ID {
		t.Fatal("expected different session IDs")
	}

	// Verify GetSession returns correct project isolation
	got1, err := svc.GetSession(ctx, GetSessionRequest{ID: sess1.ID})
	if err != nil {
		t.Fatalf("GetSession sess1 failed: %v", err)
	}
	if got1.ProjectID != "project-a" {
		t.Errorf("sess1 ProjectID = %q, want %q", got1.ProjectID, "project-a")
	}
	if got1.ProjectPath != "/tmp/project-a" {
		t.Errorf("sess1 ProjectPath = %q, want %q", got1.ProjectPath, "/tmp/project-a")
	}

	got2, err := svc.GetSession(ctx, GetSessionRequest{ID: sess2.ID})
	if err != nil {
		t.Fatalf("GetSession sess2 failed: %v", err)
	}
	if got2.ProjectID != "project-b" {
		t.Errorf("sess2 ProjectID = %q, want %q", got2.ProjectID, "project-b")
	}
	if got2.ProjectPath != "/tmp/project-b" {
		t.Errorf("sess2 ProjectPath = %q, want %q", got2.ProjectPath, "/tmp/project-b")
	}
}

// TestMigrateSessionDetectionContext tests the migration helper.
func TestMigrateSessionDetectionContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		session          *session.Session
		wantContextAfter bool
		wantCWD          string
	}{
		{
			name: "legacy session with ProjectPath gets DetectionContext",
			session: &session.Session{
				ID:          "sess-1",
				ProjectPath: "/tmp/legacy-project",
				ProjectID:   "legacy",
			},
			wantContextAfter: true,
			wantCWD:          "/tmp/legacy-project",
		},
		{
			name: "session with existing DetectionContext unchanged",
			session: &session.Session{
				ID:               "sess-2",
				ProjectPath:      "/tmp/project",
				DetectionContext: &session.DetectionContext{CWD: "/existing"},
			},
			wantContextAfter: true,
			wantCWD:          "/existing", // unchanged
		},
		{
			name: "session without ProjectPath unchanged",
			session: &session.Session{
				ID:          "sess-3",
				ProjectPath: "",
			},
			wantContextAfter: false,
		},
		{
			name: "session with ProjectID but no ProjectPath unchanged",
			session: &session.Session{
				ID:          "sess-4",
				ProjectID:   "some-project",
				ProjectPath: "",
			},
			wantContextAfter: false,
		},
		{
			name:             "nil session is safe",
			session:          nil,
			wantContextAfter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clone session to avoid mutation affecting other tests
			var sess *session.Session
			if tt.session != nil {
				clone := *tt.session
				if tt.session.DetectionContext != nil {
					cloneDC := *tt.session.DetectionContext
					clone.DetectionContext = &cloneDC
				}
				sess = &clone
			}

			migrateSessionDetectionContext(sess)

			if tt.wantContextAfter {
				if sess == nil || sess.DetectionContext == nil {
					t.Fatal("expected DetectionContext after migration")
				}
				if sess.DetectionContext.CWD != tt.wantCWD {
					t.Errorf("DetectionContext.CWD = %q, want %q", sess.DetectionContext.CWD, tt.wantCWD)
				}
			} else {
				if sess != nil && sess.DetectionContext != nil {
					t.Error("expected no DetectionContext")
				}
			}
		})
	}
}

// TestSessionList_Migration tests that List applies migration to all sessions.
func TestSessionList_Migration(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(slog.Default())
	svc := NewSessionService(store)
	ctx := context.Background()

	// Create sessions
	sess1, _ := svc.CreateSession(ctx, CreateSessionRequest{Name: "sess-1"})
	sess2, _ := svc.CreateSession(ctx, CreateSessionRequest{Name: "sess-2"})

	// Set ProjectPath on one session
	sess1.ProjectPath = "/tmp/project-1"

	// List sessions
	list, err := svc.List(ctx, ListSessionsRequest{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Find our sessions and verify migration was applied
	for _, s := range list {
		if s.ID == sess1.ID {
			if s.DetectionContext == nil {
				t.Errorf("sess-1: expected DetectionContext after List migration")
			} else if s.DetectionContext.CWD != "/tmp/project-1" {
				t.Errorf("sess-1: DetectionContext.CWD = %q, want /tmp/project-1", s.DetectionContext.CWD)
			}
		}
		if s.ID == sess2.ID {
			// sess2 has no ProjectPath, so no migration expected
			if s.DetectionContext != nil {
				t.Errorf("sess-2: unexpected DetectionContext (no ProjectPath)")
			}
		}
	}
}
