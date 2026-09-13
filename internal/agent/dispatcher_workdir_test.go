package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// newDispatcherWithSessionStore builds a Dispatcher carrying only the session
// store and the working-directory seams under test.
func newDispatcherWithSessionStore(store SessionStoreReader) *Dispatcher {
	d := &Dispatcher{}
	if store != nil {
		d.SetSessionStore(store)
	}
	return d
}

type fakeSessionStore map[string]*session.Session

func (f fakeSessionStore) Get(id string) *session.Session { return f[id] }

func (f fakeSessionStore) GetByConversationID(conversationID string) *session.Session {
	return f[conversationID]
}

// TestDispatcherResolveSessionWorkingDir_DetectionCWDBindsTheTurn is the
// regression for the HTTP chat route: a session with no project but a
// detection-context CWD (what `meept chat` sends) must resolve that directory,
// because without it every filesystem tool failed with tools.ErrNoWorkingDir.
func TestDispatcherResolveSessionWorkingDir_DetectionCWDBindsTheTurn(t *testing.T) {
	sess := &session.Session{ID: "s1", ConversationID: "c1"}
	sess.DetectionContext = &session.DetectionContext{CWD: "/tmp/detected"}
	d := newDispatcherWithSessionStore(fakeSessionStore{"c1": sess})

	dir, source := d.resolveSessionWorkingDir(sess)
	if dir != "/tmp/detected" {
		t.Fatalf("dir = %q, want /tmp/detected", dir)
	}
	if source != session.WorkingDirFromDetection {
		t.Fatalf("source = %q, want %q", source, session.WorkingDirFromDetection)
	}
}

// TestDispatcherResolveSessionWorkingDir_Precedence pins the documented order:
// worktree beats project beats detection CWD.
func TestDispatcherResolveSessionWorkingDir_Precedence(t *testing.T) {
	full := &session.Session{ID: "s1", WorktreePath: "/wt", ProjectPath: "/proj"}
	full.DetectionContext = &session.DetectionContext{CWD: "/cwd"}
	d := newDispatcherWithSessionStore(nil)

	if dir, src := d.resolveSessionWorkingDir(full); dir != "/wt" || src != session.WorkingDirFromWorktree {
		t.Fatalf("worktree case: got (%q, %q)", dir, src)
	}
	full.WorktreePath = ""
	if dir, src := d.resolveSessionWorkingDir(full); dir != "/proj" || src != session.WorkingDirFromProject {
		t.Fatalf("project case: got (%q, %q)", dir, src)
	}
	full.ProjectPath = ""
	if dir, src := d.resolveSessionWorkingDir(full); dir != "/cwd" || src != session.WorkingDirFromDetection {
		t.Fatalf("detection case: got (%q, %q)", dir, src)
	}
	full.DetectionContext = nil
	if dir, src := d.resolveSessionWorkingDir(full); dir != "" || src != session.WorkingDirFromNone {
		t.Fatalf("nothing case: got (%q, %q)", dir, src)
	}
}

// TestDispatcherResolveSessionWorkingDir_ActiveProjectFallback covers the
// unbound session: the user's ACTIVE project is the fallback, and it is
// labelled as such so the daemon log names the real source.
func TestDispatcherResolveSessionWorkingDir_ActiveProjectFallback(t *testing.T) {
	d := newDispatcherWithSessionStore(nil)
	d.SetActiveProjectPathResolver(func() string { return "/active/proj" })

	dir, source := d.resolveSessionWorkingDir(&session.Session{ID: "s1"})
	if dir != "/active/proj" {
		t.Fatalf("dir = %q, want /active/proj", dir)
	}
	if source != session.WorkingDirFromActiveProject {
		t.Fatalf("source = %q, want %q", source, session.WorkingDirFromActiveProject)
	}
}

// TestDispatcherResolveSessionWorkingDir_DefaultIsLastResort pins that the
// configured default (daemon.default_working_dir) is consulted only after the
// session chain and the active project, and that an empty default means none.
func TestDispatcherResolveSessionWorkingDir_DefaultIsLastResort(t *testing.T) {
	d := newDispatcherWithSessionStore(nil)
	d.SetDefaultWorkingDir("/configured/default")
	d.SetActiveProjectPathResolver(func() string { return "/active/proj" })

	if dir, _ := d.resolveSessionWorkingDir(&session.Session{ID: "s1"}); dir != "/active/proj" {
		t.Fatalf("active project must win over the configured default, got %q", dir)
	}
	d.SetActiveProjectPathResolver(func() string { return "" })
	dir, source := d.resolveSessionWorkingDir(&session.Session{ID: "s1"})
	if dir != "/configured/default" || source != session.WorkingDirFromDefault {
		t.Fatalf("got (%q, %q), want the configured default", dir, source)
	}
}

// TestDispatcherSetDefaults_NilAndEmptyAreNoOps guards the setter contracts
// (AGENTS.md: every Set* nil-guards; empty means "unset", not "unlimited").
func TestDispatcherSetDefaults_NilAndEmptyAreNoOps(t *testing.T) {
	d := newDispatcherWithSessionStore(nil)
	d.SetDefaultWorkingDir("")
	d.SetActiveProjectPathResolver(nil)
	if d.defaultWorkingDir != "" || d.activeProjectPath != nil {
		t.Fatal("empty default / nil resolver must not be stored")
	}
	var nilDispatcher *Dispatcher
	nilDispatcher.SetDefaultWorkingDir("/x")
	nilDispatcher.SetActiveProjectPathResolver(func() string { return "/x" })
}
