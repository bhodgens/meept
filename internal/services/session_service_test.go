package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

// newTestSessionService constructs a SessionService backed by an in-memory
// session store for unit testing.
func newTestSessionService(t *testing.T) *SessionService {
	t.Helper()
	store := session.NewMemoryStore(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewSessionService(store)
}

func TestSessionServiceArchiveSession(t *testing.T) {
	svc := newTestSessionService(t)

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "to-archive"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: sess.ID, Archived: true}); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	got, err := svc.GetSession(context.Background(), GetSessionRequest{ID: sess.ID})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !got.Archived {
		t.Fatalf("expected Archived=true, got false")
	}

	// Unarchive round-trip
	if err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: sess.ID, Archived: false}); err != nil {
		t.Fatalf("ArchiveSession unarchive: %v", err)
	}
	got, _ = svc.GetSession(context.Background(), GetSessionRequest{ID: sess.ID})
	if got.Archived {
		t.Fatalf("expected Archived=false after unarchive, got true")
	}
}

func TestSessionServiceArchiveSession_NotFound(t *testing.T) {
	svc := newTestSessionService(t)

	err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: "nonexistent", Archived: true})
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got nil")
	}
	// Verify it maps to ErrNotFound for HTTP 404 handling.
	if !isServiceError(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound mapping, got: %v", err)
	}
}

func TestSessionServiceArchiveSession_InvalidInput(t *testing.T) {
	svc := newTestSessionService(t)

	err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: "", Archived: true})
	if err == nil {
		t.Fatalf("expected error for empty ID, got nil")
	}
	if !isServiceError(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput mapping, got: %v", err)
	}
}

// isServiceError reports whether err is a *ServiceError wrapping target via
// errors.Is. This avoids duplicating error-text checks across tests.
func isServiceError(err, target error) bool {
	se, ok := err.(*ServiceError)
	if !ok {
		return false
	}
	return errors.Is(se, target)
}

func newTestProjectManager(t *testing.T) *project.ProjectManager {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.ProjectsConfig{
		BaseDir:       filepath.Join(dir, "projects"),
		DefaultBranch: "main",
	}
	os.MkdirAll(cfg.BaseDir, 0o755)
	return project.NewProjectManager(store, nil, cfg, nil)
}

func TestCreateSession_InheritsActiveProject(t *testing.T) {
	// Create a session store + project manager with an active project.
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	// Create an active project.
	activeProj, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	// Wire project manager into session service.
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	// Create a session — should inherit the active project.
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID != activeProj.ID {
		t.Errorf("ProjectID = %q, want %q", sess.ProjectID, activeProj.ID)
	}
	if sess.ProjectPath != activeProj.LocalPath {
		t.Errorf("ProjectPath = %q, want %q", sess.ProjectPath, activeProj.LocalPath)
	}
}

func TestCreateSession_CreatesDefaultWhenNoneActive(t *testing.T) {
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	// No active project yet.
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	// Create a session — should auto-create a default project.
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID == "" {
		t.Error("ProjectID should not be empty")
	}
	if sess.ProjectPath == "" {
		t.Error("ProjectPath should not be empty")
	}
}

func TestCreateSession_NilProjectManager(t *testing.T) {
	// SessionService without a project manager should still create sessions.
	store := session.NewMemoryStore(nil)
	svc := NewSessionService(store)
	// Do NOT call SetProjectManager — pm stays nil.

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "no-pm"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID != "" {
		t.Errorf("expected empty ProjectID, got %q", sess.ProjectID)
	}
}

func TestSetProjectManager_NilGuard(t *testing.T) {
	svc := NewSessionService(session.NewMemoryStore(nil))
	// SetProjectManager(nil) should be a no-op.
	svc.SetProjectManager(nil)
	if svc.pm != nil {
		t.Error("expected nil pm after SetProjectManager(nil)")
	}
}

// failingProjectResolver implements ProjectResolver but always returns an error.
type failingProjectResolver struct{}

func (failingProjectResolver) EnsureDefault(ctx context.Context) (*project.Project, error) {
	return nil, errors.New("simulated failure")
}
func (failingProjectResolver) GetActive(ctx context.Context) (*project.Project, error) {
	return nil, errors.New("simulated failure")
}
func (failingProjectResolver) Get(ctx context.Context, id string) (*project.Project, error) {
	return nil, errors.New("simulated failure")
}

func TestCreateSession_EnsureDefaultError(t *testing.T) {
	// When EnsureDefault fails, CreateSession should still succeed — the
	// error is logged, not propagated. The session just won't have a project.
	sessionStore := session.NewMemoryStore(nil)
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(failingProjectResolver{})

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession should not fail even if EnsureDefault errors: %v", err)
	}
	if sess == nil || sess.ID == "" {
		t.Error("expected valid session")
	}
	if sess.ProjectID != "" {
		t.Errorf("expected empty ProjectID on EnsureDefault failure, got %q", sess.ProjectID)
	}
}
