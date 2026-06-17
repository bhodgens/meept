package project

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateAndGetProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := &Project{
		ID:        "proj-1",
		Name:      "test-project",
		Mode:      ModeGit,
		GitURL:    "https://github.com/example/test.git",
		Branch:    "main",
		LocalPath: "/tmp/test-project",
	}

	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := s.GetProject(ctx, "proj-1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "test-project" {
		t.Errorf("Name = %q, want %q", got.Name, "test-project")
	}
	if got.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", got.Mode, ModeGit)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestGetProjectNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetProject(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetProject(nonexistent) error = %v, want ErrNotFound", err)
	}
}

func TestListProjects(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, tc := range []struct {
		id   string
		name string
		mode Mode
	}{
		{"p1", "project-1", ModeGit},
		{"p2", "project-2", ModeLocal},
		{"p3", "project-3", ModeGit},
	} {
		if err := s.CreateProject(ctx, &Project{
			ID:   tc.id,
			Name: tc.name,
			Mode: tc.mode,
		}); err != nil {
			t.Fatalf("CreateProject(%s): %v", tc.id, err)
		}
	}

	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("len(projects) = %d, want 3", len(projects))
	}
	// ListProjects returns in created_at DESC order, so p3 is first
	if projects[0].ID != "p3" {
		t.Errorf("projects[0].ID = %q, want %q", projects[0].ID, "p3")
	}
}

func TestUpdateProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := &Project{ID: "u1", Name: "original", Mode: ModeLocal}
	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatal(err)
	}

	p.Name = "updated"
	p.Status = "archived"
	if err := s.UpdateProject(ctx, p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	got, _ := s.GetProject(ctx, "u1")
	if got.Name != "updated" {
		t.Errorf("Name = %q, want %q", got.Name, "updated")
	}
	if got.Status != "archived" {
		t.Errorf("Status = %q, want %q", got.Status, "archived")
	}
}

func TestDeleteProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := &Project{ID: "d1", Name: "to-delete", Mode: ModeLocal}
	s.CreateProject(ctx, p)

	if err := s.DeleteProject(ctx, "d1"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	_, err := s.GetProject(ctx, "d1")
	if err != ErrNotFound {
		t.Errorf("after delete: error = %v, want ErrNotFound", err)
	}
}

func TestCreateAndGetWorktree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Need a project first
	s.CreateProject(ctx, &Project{ID: "wp1", Name: "proj", Mode: ModeGit})

	w := &Worktree{
		ProjectID: "wp1",
		SessionID: "sess-1",
		PlanID:    "plan-1",
		Path:      "/tmp/worktree-1",
		Branch:    "session/sess-1",
	}
	if err := s.CreateWorktree(ctx, w); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if w.ID == "" {
		t.Error("expected ID to be auto-generated")
	}

	got, err := s.GetWorktree(ctx, w.ID)
	if err != nil {
		t.Fatalf("GetWorktree: %v", err)
	}
	if got.ProjectID != "wp1" {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, "wp1")
	}
	if got.Branch != "session/sess-1" {
		t.Errorf("Branch = %q, want %q", got.Branch, "session/sess-1")
	}
}

func TestGetActiveWorktreeBySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateProject(ctx, &Project{ID: "ws1", Name: "proj", Mode: ModeGit})

	s.CreateWorktree(ctx, &Worktree{
		ProjectID: "ws1",
		SessionID: "sess-active",
		Path:      "/tmp/wt-active",
		Branch:    "session/sess-active",
		Status:    "active",
	})

	s.CreateWorktree(ctx, &Worktree{
		ProjectID: "ws1",
		SessionID: "sess-done",
		Path:      "/tmp/wt-done",
		Branch:    "session/sess-done",
		Status:    "cleaned",
	})

	got, err := s.GetActiveWorktreeBySession(ctx, "sess-active")
	if err != nil {
		t.Fatalf("GetActiveWorktreeBySession: %v", err)
	}
	if got.SessionID != "sess-active" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "sess-active")
	}

	_, err = s.GetActiveWorktreeBySession(ctx, "sess-done")
	if err != ErrNotFound {
		t.Errorf("GetActiveWorktreeBySession(sess-done) = %v, want ErrNotFound", err)
	}
}

func TestListWorktreesByProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateProject(ctx, &Project{ID: "wl1", Name: "proj", Mode: ModeGit})

	for _, id := range []string{"wt-a", "wt-b", "wt-c"} {
		s.CreateWorktree(ctx, &Worktree{
			ID:        id,
			ProjectID: "wl1",
			Path:      "/tmp/" + id,
			Branch:    "feature/" + id,
		})
	}

	wts, err := s.ListWorktreesByProject(ctx, "wl1")
	if err != nil {
		t.Fatalf("ListWorktreesByProject: %v", err)
	}
	if len(wts) != 3 {
		t.Fatalf("len(worktrees) = %d, want 3", len(wts))
	}
}

func TestCleanupOrphanedWorktrees(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateProject(ctx, &Project{ID: "wc1", Name: "proj", Mode: ModeGit})

	// active with empty session -> orphaned
	s.CreateWorktree(ctx, &Worktree{
		ID:        "wt-orphan",
		ProjectID: "wc1",
		Path:      "/tmp/orphan",
		Branch:    "orphan",
		Status:    "active",
	})
	// active with session -> not orphaned
	s.CreateWorktree(ctx, &Worktree{
		ID:        "wt-session",
		ProjectID: "wc1",
		SessionID: "sess-1",
		Path:      "/tmp/session",
		Branch:    "session/sess-1",
		Status:    "active",
	})
	// active with plan_id only -> not orphaned
	s.CreateWorktree(ctx, &Worktree{
		ID:        "wt-plan",
		ProjectID: "wc1",
		PlanID:    "plan-42",
		Path:      "/tmp/plan",
		Branch:    "plan/plan-42",
		Status:    "active",
	})

	cleaned, err := s.CleanupOrphanedWorktrees(ctx)
	if err != nil {
		t.Fatalf("CleanupOrphanedWorktrees: %v", err)
	}
	if cleaned != 1 {
		t.Errorf("cleaned = %d, want 1", cleaned)
	}

	got, _ := s.GetWorktree(ctx, "wt-orphan")
	if got.Status != "cleaned" {
		t.Errorf("orphan status = %q, want %q", got.Status, "cleaned")
	}

	got2, _ := s.GetWorktree(ctx, "wt-session")
	if got2.Status != "active" {
		t.Errorf("session worktree status = %q, want %q", got2.Status, "active")
	}

	got3, _ := s.GetWorktree(ctx, "wt-plan")
	if got3.Status != "active" {
		t.Errorf("plan worktree status = %q, want %q", got3.Status, "active")
	}
}

func TestGetProjectByPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateProject(ctx, &Project{
		ID:        "pp1",
		Name:      "path-proj",
		Mode:      ModeLocal,
		LocalPath: "/some/path",
	})

	got, err := s.GetProjectByPath(ctx, "/some/path")
	if err != nil {
		t.Fatalf("GetProjectByPath: %v", err)
	}
	if got.ID != "pp1" {
		t.Errorf("ID = %q, want %q", got.ID, "pp1")
	}

	_, err = s.GetProjectByPath(ctx, "/no/such/path")
	if err != ErrNotFound {
		t.Errorf("GetProjectByPath(missing) = %v, want ErrNotFound", err)
	}
}

// Ensure the pool types compile correctly.
var _ = (*sqlite.Pool)(nil)

// Ensure time formatting round-trips.
func TestTimeRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	s := formatTime(now)
	got := parseTime(s)
	if !got.Equal(now) {
		t.Errorf("round-trip: got %v, want %v", got, now)
	}
}
