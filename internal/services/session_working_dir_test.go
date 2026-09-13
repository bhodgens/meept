package services

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// Session-creation half of the always-have-a-working-directory contract.
//
// Session creation binds the ACTIVE project when one exists and records the
// client CWD from the detection context; it must never synthesize a project
// (AGENTS.md: "Do NOT call EnsureDefault() for session binding — it creates a
// synthetic empty git repo") and never fall back to the daemon's own process
// CWD. A session left unbound is reported loudly instead.

// TestCreateSession_DetectionCWDWithoutProjectResolvesCWD: no active project,
// but the client sent its CWD. Creation must leave the session resolvable to
// exactly that directory (session creation resolves the CWD into a project, or
// keeps it in the detection context — either way the working directory is the
// client's CWD, which is what the turn-start binding in agent.ChatHandler
// then uses).
func TestCreateSession_DetectionCWDWithoutProjectResolvesCWD(t *testing.T) {
	store := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t) // no active project in this store
	svc := NewSessionService(store)
	svc.SetProjectManager(pm)

	clientCWD := t.TempDir()
	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{
		Name:             "detection-cwd",
		DetectionContext: &session.DetectionContext{CWD: clientCWD},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	dir, src := session.ResolveWorkingDir(sess)
	if dir == "" {
		t.Fatal("session with a client CWD resolved no working directory")
	}
	if src == session.WorkingDirFromNone {
		t.Errorf("ResolveWorkingDir() source = %q, want a real source", src)
	}
	// session.create resolves the client CWD into a project (CreateOrResolve),
	// so the resolved directory is a project rooted in the client's CWD.
	if !strings.Contains(dir, "TestCreateSession_DetectionCWDWithoutProjectResolvesCWD") {
		t.Errorf("ResolveWorkingDir() dir = %q, want a directory derived from the client CWD %q", dir, clientCWD)
	}
}

// TestCreateSession_NoProjectNoCWDBindsNothingAndStaysUnbound: the daemon11
// shape. Without a project manager at all (has_project_manager=false) and with
// no client CWD, creation must succeed, create NO project, and leave the
// session with no working directory — the signal the turn-start path and the
// tools turn into an actionable error rather than a silent pathless turn.
func TestCreateSession_NoProjectNoCWDBindsNothingAndStaysUnbound(t *testing.T) {
	store := session.NewMemoryStore(nil)
	svc := NewSessionService(store) // no project manager wired (daemon11 shape)

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "unbound"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty (no synthetic project)", sess.ProjectID)
	}
	if sess.ProjectPath != "" {
		t.Errorf("ProjectPath = %q, want empty (no synthetic project)", sess.ProjectPath)
	}
	if dir, src := session.ResolveWorkingDir(sess); dir != "" || src != session.WorkingDirFromNone {
		t.Errorf("ResolveWorkingDir() = (%q, %q), want (\"\", %q)", dir, src, session.WorkingDirFromNone)
	}
}

// TestCreateSession_ExplicitProjectIDWinsOverClientCWD: an explicitly
// requested project keeps precedence (session creation priority: explicit
// project_id > CWD from detection context > active project), so binding is
// deterministic and never drifts to wherever the client happened to run.
func TestCreateSession_ExplicitProjectIDWinsOverClientCWD(t *testing.T) {
	store := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	activeProj, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	svc := NewSessionService(store)
	svc.SetProjectManager(pm)

	clientCWD := t.TempDir()
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{
		Name:             "explicit-project",
		ProjectID:        activeProj.ID,
		DetectionContext: &session.DetectionContext{CWD: clientCWD},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	dir, src := session.ResolveWorkingDir(sess)
	if dir != activeProj.LocalPath {
		t.Errorf("ResolveWorkingDir() dir = %q, want the explicit project path %q", dir, activeProj.LocalPath)
	}
	if src != session.WorkingDirFromProject {
		t.Errorf("ResolveWorkingDir() source = %q, want %q", src, session.WorkingDirFromProject)
	}
}
