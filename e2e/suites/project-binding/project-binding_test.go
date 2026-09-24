//go:build e2e

// Suite project-binding: per-session project resolution — CreateOrResolve
// idempotently adopting the same directory via the sidecar id, and the
// no-global-active-project-fallback invariant (an unbound session is
// unaffected by another session's binding).
package projectbinding

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
	_ "modernc.org/sqlite"
)

// newSandboxManager builds a project.Store-backed ProjectManager over a
// sandbox projects.db (never ~/.meept) and returns it with a cleanup.
func newSandboxManager(t *testing.T, baseDir string) *project.ProjectManager {
	t.Helper()
	store, err := project.NewStore(filepath.Join(t.TempDir(), "projects.db"), nil)
	if err != nil {
		t.Fatalf("project store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.ProjectsConfig{
		Enabled:    true,
		BaseDir:    baseDir,
		AutoDetect: false,
	}
	return project.NewProjectManager(store, nil, cfg, nil)
}

// initGitRepo turns dir into a real git repo (DetectFromPath requires .git).
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "e2e@example.invalid"},
		{"config", "user.name", "e2e"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestProjectBinding_CreateOrResolveIdempotentSidecarID covers
// project-binding-01: repeated CreateOrResolve of the same directory
// resolves to ONE project with a stable id, and the sidecar (written on
// the local/shorthand registration path) drives adoption after a rename.
// A .git directory takes the DetectFromPath auto-register path, which
// deliberately does NOT write a sidecar — so this fixture uses a
// non-git local directory (the sidecar-bearing mode).
func TestProjectBinding_CreateOrResolveIdempotentSidecarID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := t.TempDir()
	projectDir := filepath.Join(base, "widget-dir") // NO .git: local-mode registration
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	pm := newSandboxManager(t, base)

	// First resolution registers (RegisterLocal path — no .git, no
	// sidecar) — CreateOrResolve with an absolute LOCAL path does NOT
	// write a sidecar; the sidecar belongs to the shorthand-name and
	// explicit registration paths. So assert the local idempotency and
	// use the SHORTHAND form (base_dir/<name>) to get sidecar-backed
	// adoption.
	p1, err := pm.CreateOrResolve(ctx, projectDir)
	if err != nil {
		t.Fatalf("first CreateOrResolve: %v", err)
	}
	if p1 == nil || p1.ID == "" {
		t.Fatalf("first resolution returned no project: %+v", p1)
	}
	for i := 0; i < 3; i++ {
		again, err := pm.CreateOrResolve(ctx, projectDir)
		if err != nil {
			t.Fatalf("idempotent resolve %d: %v", i, err)
		}
		if again.ID != p1.ID {
			t.Fatalf("resolve %d id = %q, want the same %q", i, again.ID, p1.ID)
		}
	}

	// Shorthand registration writes the sidecar.
	short, err := pm.CreateOrResolve(ctx, "named-dir")
	if err != nil {
		t.Fatalf("shorthand CreateOrResolve: %v", err)
	}
	namedDir := filepath.Join(base, "named-dir")
	sidecarID, err := pm.ReadSidecarID(namedDir)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if sidecarID == "" {
		t.Fatal("shorthand CreateOrResolve did not write the sidecar id")
	}
	if sidecarID != short.ID {
		t.Fatalf("sidecar id %q != project id %q", sidecarID, short.ID)
	}

	// Rename the SIDECAR-BEARING directory: the sidecar travels with it
	// and the SAME project id is adopted (path/name updated in the DB).
	renamed := filepath.Join(base, "named-renamed")
	if err := os.Rename(namedDir, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}
	p2, err := pm.CreateOrResolve(ctx, renamed)
	if err != nil {
		t.Fatalf("resolve after rename: %v", err)
	}
	if p2.ID != short.ID {
		t.Fatalf("post-rename id = %q, want the adopted %q", p2.ID, short.ID)
	}
	if p2.LocalPath != renamed {
		t.Fatalf("post-rename path = %q, want %q", p2.LocalPath, renamed)
	}
	if p2.Name != "named-renamed" {
		t.Fatalf("post-rename name = %q, want named-renamed", p2.Name)
	}

	// A .git directory resolves through DetectFromPath (auto-register)
	// with the same idempotency guarantee.
	gitDir := filepath.Join(base, "git-repo")
	initGitRepo(t, gitDir)
	g1, err := pm.CreateOrResolve(ctx, gitDir)
	if err != nil {
		t.Fatalf("git CreateOrResolve: %v", err)
	}
	g2, err := pm.CreateOrResolve(ctx, gitDir)
	if err != nil {
		t.Fatalf("git CreateOrResolve 2: %v", err)
	}
	if g1.ID != g2.ID {
		t.Fatalf("git resolves drifted: %q vs %q", g1.ID, g2.ID)
	}
}

// TestProjectBinding_NoGlobalActiveProjectFallback covers
// project-binding-02: session working-dir resolution is PER-SESSION —
// binding a project onto session A never gives session B a working
// directory, and an unbound session resolves nothing (no global active
// fallback, no daemon CWD).
func TestProjectBinding_NoGlobalActiveProjectFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := t.TempDir()
	pm := newSandboxManager(t, base)
	projectDir := filepath.Join(base, "bound-repo")
	initGitRepo(t, projectDir)

	// Session A resolves a real project binding from its CWD.
	sessA := &session.Session{ID: "sess-A"}
	proj, err := pm.CreateOrResolve(ctx, projectDir)
	if err != nil {
		t.Fatalf("resolve A's project: %v", err)
	}
	sessA.ProjectID = proj.ID
	sessA.ProjectPath = proj.LocalPath

	// Session B is unbound: no project, no detection CWD, no worktree.
	sessB := &session.Session{ID: "sess-B"}

	// A resolves its own dir; B resolves NOTHING even though A is bound
	// and a "global active project" concept could have leaked it.
	dirA, srcA := session.ResolveWorkingDir(sessA)
	if dirA != projectDir {
		t.Fatalf("A's working dir = %q, want %q", dirA, projectDir)
	}
	if srcA != session.WorkingDirFromProject {
		t.Fatalf("A's source = %q, want project_path", srcA)
	}
	dirB, srcB := session.ResolveWorkingDir(sessB)
	if dirB != "" {
		t.Fatalf("unbound session B resolved a working dir %q — the global-active fallback leaked", dirB)
	}
	if srcB != session.WorkingDirFromNone {
		t.Fatalf("B's source = %q, want none", srcB)
	}

	// Even with the manager holding a "recently used" path, B stays
	// unbound: recents are diagnostics, not a resolution source.
	recentsDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "recents.db"))
	if err != nil {
		t.Fatalf("recents db: %v", err)
	}
	defer recentsDB.Close()
	if _, err := recentsDB.Exec(`CREATE TABLE IF NOT EXISTS project_recents (project_path TEXT PRIMARY KEY, last_used_at TEXT)`); err != nil {
		t.Fatalf("recents schema: %v", err)
	}
	recentsStore := project.NewRecentsStore(recentsDB)
	if err := recentsStore.TouchRecent(ctx, projectDir); err != nil {
		t.Fatalf("touch recent: %v", err)
	}
	recents, err := recentsStore.ListRecents(ctx, 10)
	if err != nil || len(recents) == 0 {
		t.Fatalf("recents not recorded (len=%d err=%v) — test precondition", len(recents), err)
	}
	dirB2, srcB2 := session.ResolveWorkingDir(sessB)
	if dirB2 != "" || srcB2 != session.WorkingDirFromNone {
		t.Fatalf("after recents, B still resolved %q/%q — resolution must consult only the session's own binding", dirB2, srcB2)
	}

	// A session carrying ONLY a detection-context CWD resolves the CWD
	// itself (the third precedence), never a synthesized project.
	sessC := &session.Session{ID: "sess-C", DetectionContext: &session.DetectionContext{CWD: base}}
	dirC, srcC := session.ResolveWorkingDir(sessC)
	if dirC != base || srcC != session.WorkingDirFromDetection {
		t.Fatalf("C resolved %q/%q, want %q/detection_context_cwd", dirC, srcC, base)
	}
}
