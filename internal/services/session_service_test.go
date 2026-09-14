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

// TestCreateSession_DoesNotInheritActiveProject: sessions are scoped
// PER-SESSION, so an active project must NOT leak into a session created
// without a project_id or a client CWD. The session is left unbound.
func TestCreateSession_DoesNotInheritActiveProject(t *testing.T) {
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	// Create an active project — it must not be inherited.
	activeProj, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty (no active-project fallback)", sess.ProjectID)
	}
	if sess.ProjectPath != "" {
		t.Errorf("ProjectPath = %q, want empty (no active-project fallback)", sess.ProjectPath)
	}
	if _, src := session.ResolveWorkingDir(sess); src != session.WorkingDirFromNone {
		t.Errorf("ResolveWorkingDir source = %q, want none", src)
	}
	// The active project still exists and is untouched.
	still, err := pm.GetActive(ctx)
	if err != nil || still == nil {
		t.Fatalf("GetActive: %v %v", still, err)
	}
	if still.ID != activeProj.ID {
		t.Errorf("active project changed: %q -> %q", activeProj.ID, still.ID)
	}
}

// TestCreateSession_BindsDetectionCWD: the client CWD is resolved into a
// project and bound to that session only.
func TestCreateSession_BindsDetectionCWD(t *testing.T) {
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()
	dir := t.TempDir()

	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	sess, err := svc.CreateSession(ctx, CreateSessionRequest{
		Name:             "cwd-bound",
		DetectionContext: &session.DetectionContext{CWD: dir},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID == "" || sess.ProjectPath == "" {
		t.Fatalf("expected the CWD to bind a project, got id=%q path=%q", sess.ProjectID, sess.ProjectPath)
	}
	if got, src := session.ResolveWorkingDir(sess); got != sess.ProjectPath || src != session.WorkingDirFromProject {
		t.Errorf("ResolveWorkingDir() = (%q, %q), want the bound project %q", got, src, sess.ProjectPath)
	}
	// The detection context was persisted too, so the CWD survives a restart.
	if sess.DetectionContext == nil || sess.DetectionContext.CWD != dir {
		t.Errorf("DetectionContext = %+v, want CWD %q", sess.DetectionContext, dir)
	}
	reloaded, _ := svc.GetSession(ctx, GetSessionRequest{ID: sess.ID})
	if reloaded == nil || reloaded.DetectionContext == nil || reloaded.DetectionContext.CWD != dir {
		t.Errorf("reloaded session lost its detection CWD: %+v", reloaded)
	}
}

// TestCreateSession_PerSessionProjectIsolation: two sessions created with
// different explicit projects each resolve their own project.
func TestCreateSession_PerSessionProjectIsolation(t *testing.T) {
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	projA, err := pm.CreateOrResolve(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("CreateOrResolve A: %v", err)
	}
	projB, err := pm.CreateOrResolve(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("CreateOrResolve B: %v", err)
	}

	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	sessA, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "a", ProjectID: projA.ID})
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}
	sessB, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "b", ProjectID: projB.ID})
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}
	if sessA.ProjectPath != projA.LocalPath {
		t.Errorf("session A path = %q, want %q", sessA.ProjectPath, projA.LocalPath)
	}
	if sessB.ProjectPath != projB.LocalPath {
		t.Errorf("session B path = %q, want %q", sessB.ProjectPath, projB.LocalPath)
	}
	if sessA.ProjectPath == sessB.ProjectPath {
		t.Error("sessions bound to different projects must not share a path")
	}
}

func TestCreateSession_UnboundWhenNoneActive(t *testing.T) {
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	// No active project yet.
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	// Create a session — should NOT auto-create a synthetic default project.
	// The session is left unbound; the caller binds a project later.
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID != "" {
		t.Errorf("ProjectID should be empty when no active project, got %q", sess.ProjectID)
	}
	if sess.ProjectPath != "" {
		t.Errorf("ProjectPath should be empty when no active project, got %q", sess.ProjectPath)
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
func (failingProjectResolver) CreateOrResolve(ctx context.Context, arg string) (*project.Project, error) {
	return nil, errors.New("simulated failure")
}

func TestCreateSession_ResolverFailuresLeaveSessionUnbound(t *testing.T) {
	// When every project-resolution call fails, CreateSession must still
	// succeed: the session is left unbound (never a global fallback) and the
	// error is logged, not propagated.
	sessionStore := session.NewMemoryStore(nil)
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(failingProjectResolver{})

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession should not fail on resolver error: %v", err)
	}
	if sess == nil || sess.ID == "" {
		t.Error("expected valid session")
	}
	if sess.ProjectID != "" {
		t.Errorf("expected empty ProjectID on resolver failure, got %q", sess.ProjectID)
	}
}

func TestCreateSession_ResolverFailureStillPersistsDetectionCWD(t *testing.T) {
	// A failed CWD->project resolution must NOT lose the client CWD: the
	// detection context is persisted regardless, so the session still
	// resolves its directory at turn time (including after a restart).
	sessionStore := session.NewMemoryStore(nil)
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(failingProjectResolver{})

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{
		Name:             "cwd-only",
		DetectionContext: &session.DetectionContext{CWD: "/tmp/cwd-only"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectPath != "" {
		t.Errorf("expected no project binding, got %q", sess.ProjectPath)
	}
	if dir, src := session.ResolveWorkingDir(sess); dir != "/tmp/cwd-only" || src != session.WorkingDirFromDetection {
		t.Errorf("ResolveWorkingDir() = (%q, %q), want the detection CWD", dir, src)
	}
}
