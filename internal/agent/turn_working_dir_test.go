package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// Turn-start working-directory binding.
//
// fresh-rig daemon11 (2026-09-13): the daemon served turns for a session with
// no project (and no client CWD), sessionLoop returned the singleton loop, the
// loop's working directory was empty, and every filesystem tool that needed a
// session directory failed. These tests pin the turn-start resolution:
// WorktreePath > ProjectPath > DetectionContext.CWD, then the user's active
// project, then the daemon's configured default. Never the daemon's own CWD.

func newTurnWorkdirHandler(t *testing.T, sessions map[string]*session.Session) *ChatHandler {
	t.Helper()
	singleton := NewAgentLoop("singleton", "/tmp")
	h := NewChatHandler(singleton, nil, nil, slogDiscardLogger())
	if sessions != nil {
		h.SetSessionStore(&stubSessionStore{sessions: sessions})
	}
	h.SetAgentLoopManager(NewManager(ManagerConfig{}))
	return h
}

// TestSessionLoop_DetectionCWDWithoutProject_BindsThatCWD: the daemon11 shape —
// a session whose only working-directory source is the client CWD must bind a
// session-scoped loop to it instead of falling through to the pathless
// singleton.
func TestSessionLoop_DetectionCWDWithoutProject_BindsThatCWD(t *testing.T) {
	const sessionID = "sess-detection-cwd"
	const clientCWD = "/repos/from-client-cwd"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {
			ID:               sessionID,
			ConversationID:   sessionID,
			DetectionContext: &session.DetectionContext{CWD: clientCWD},
		},
	})

	loop := h.sessionLoop(sessionID)
	if loop == nil {
		t.Fatal("expected a non-nil loop")
	}
	if loop.GetSessionID() == "singleton" {
		t.Fatal("expected a session-scoped loop, got the singleton")
	}
	if got := loop.GetWorkingDir(); got != clientCWD {
		t.Errorf("GetWorkingDir() = %q, want detection CWD %q", got, clientCWD)
	}
}

// TestSessionLoop_WorktreePreferredOverProject: the session's worktree wins
// over its project path.
func TestSessionLoop_WorktreePreferredOverProject(t *testing.T) {
	const sessionID = "sess-worktree"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {
			ID:             sessionID,
			ConversationID: sessionID,
			WorktreePath:   "/wt/phase-1",
			ProjectPath:    "/repos/project",
		},
	})

	loop := h.sessionLoop(sessionID)
	if loop == nil || loop.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop, got %v", loop)
	}
	if got := loop.GetWorkingDir(); got != "/wt/phase-1" {
		t.Errorf("GetWorkingDir() = %q, want the worktree /wt/phase-1", got)
	}
}

// TestSessionLoop_UnboundSessionFallsBackToActiveProject: a fresh session with
// no project binds to the user's ACTIVE project at turn start — the same
// source session creation uses. No synthetic project is created.
func TestSessionLoop_UnboundSessionFallsBackToActiveProject(t *testing.T) {
	const sessionID = "sess-unbound"
	const activePath = "/repos/active-project"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
	})
	h.SetActiveProjectPathResolver(func() string { return activePath })

	loop := h.sessionLoop(sessionID)
	if loop == nil || loop.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop, got %v", loop)
	}
	if got := loop.GetWorkingDir(); got != activePath {
		t.Errorf("GetWorkingDir() = %q, want the active project path %q", got, activePath)
	}
}

// TestSessionLoop_UnknownSessionFallsBackToActiveProject: even with no session
// record at all (the rig's sessions table had 0 rows while messages existed),
// a turn binds to the active project rather than running pathless.
func TestSessionLoop_UnknownSessionFallsBackToActiveProject(t *testing.T) {
	const activePath = "/repos/active-project"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{})
	h.SetActiveProjectPathResolver(func() string { return activePath })

	loop := h.sessionLoop("session-with-no-record")
	if loop == nil || loop.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop, got %v", loop)
	}
	if got := loop.GetWorkingDir(); got != activePath {
		t.Errorf("GetWorkingDir() = %q, want the active project path %q", got, activePath)
	}
}

// TestSessionLoop_DaemonDefaultWorkingDirIsLastResort: with no session and no
// active project, the daemon's configured default working dir applies.
func TestSessionLoop_DaemonDefaultWorkingDirIsLastResort(t *testing.T) {
	const sessionID = "sess-default-only"
	const configured = "/srv/meept-workspace"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
	})
	h.SetActiveProjectPathResolver(func() string { return "" })
	h.SetDefaultWorkingDir(configured)

	loop := h.sessionLoop(sessionID)
	if loop == nil || loop.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop, got %v", loop)
	}
	if got := loop.GetWorkingDir(); got != configured {
		t.Errorf("GetWorkingDir() = %q, want the configured default %q", got, configured)
	}
}

// TestSessionLoop_NothingBoundKeepsSingleton: when nothing resolves at all the
// singleton is still used (unchanged behavior) — the tools then report the
// actionable tools.ErrNoWorkingDir instead of the bare "no path specified".
func TestSessionLoop_NothingBoundKeepsSingleton(t *testing.T) {
	const sessionID = "sess-nothing"
	singleton := NewAgentLoop("singleton", "/tmp")
	h := NewChatHandler(singleton, nil, nil, slogDiscardLogger())
	h.SetSessionStore(&stubSessionStore{sessions: map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
	}})
	h.SetAgentLoopManager(NewManager(ManagerConfig{}))

	if got := h.sessionLoop(sessionID); got != singleton {
		t.Errorf("expected the singleton loop when nothing is bound, got %v", got)
	}
}

// TestSessionLoop_PrimarySessionIDLookup: the client sends the session's
// primary ID in the conversation_id field, so a session whose ConversationID
// is empty must still resolve through the Get fallback.
func TestSessionLoop_PrimarySessionIDLookup(t *testing.T) {
	const sessionID = "session-abc123"
	const projectPath = "/repos/primary-id-project"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {ID: sessionID, ProjectPath: projectPath},
	})

	loop := h.sessionLoop(sessionID)
	if loop == nil || loop.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop via the primary-ID lookup, got %v", loop)
	}
	if got := loop.GetWorkingDir(); got != projectPath {
		t.Errorf("GetWorkingDir() = %q, want %q", got, projectPath)
	}
}

// TestEffectiveWorkingDir_Labels documents the resolution order and the
// diagnostic labels, including the explicit "none" when nothing resolves.
func TestEffectiveWorkingDir_Labels(t *testing.T) {
	const sessionID = "sess-labels"
	h := newTurnWorkdirHandler(t, nil)

	tests := []struct {
		name       string
		sess       *session.Session
		active     string
		defaultDir string
		wantDir    string
		wantSource string
	}{
		{
			name:       "worktree wins",
			sess:       &session.Session{WorktreePath: "/wt", ProjectPath: "/p", DetectionContext: &session.DetectionContext{CWD: "/c"}},
			wantDir:    "/wt",
			wantSource: "worktree_path",
		},
		{
			name:       "project over detection cwd",
			sess:       &session.Session{ProjectPath: "/p", DetectionContext: &session.DetectionContext{CWD: "/c"}},
			wantDir:    "/p",
			wantSource: "project_path",
		},
		{
			name:       "detection cwd",
			sess:       &session.Session{DetectionContext: &session.DetectionContext{CWD: "/c"}},
			wantDir:    "/c",
			wantSource: "detection_context_cwd",
		},
		{
			name:       "active project fallback",
			sess:       &session.Session{ID: sessionID},
			active:     "/active",
			wantDir:    "/active",
			wantSource: "active_project",
		},
		{
			name:       "daemon default is last",
			sess:       &session.Session{ID: sessionID},
			active:     "/active",
			defaultDir: "/default",
			wantDir:    "/active",
			wantSource: "active_project",
		},
		{
			name:       "daemon default alone",
			sess:       &session.Session{ID: sessionID},
			defaultDir: "/default",
			wantDir:    "/default",
			wantSource: "daemon_default",
		},
		{
			name:       "nothing bound",
			sess:       &session.Session{ID: sessionID},
			wantDir:    "",
			wantSource: "none",
		},
		{
			name:       "nil session",
			sess:       nil,
			wantDir:    "",
			wantSource: "none",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.SetActiveProjectPathResolver(nil)
			h.SetDefaultWorkingDir("")
			if tt.active != "" {
				active := tt.active
				h.SetActiveProjectPathResolver(func() string { return active })
			}
			if tt.defaultDir != "" {
				h.SetDefaultWorkingDir(tt.defaultDir)
			}
			dir, src := h.effectiveWorkingDir(tt.sess)
			if dir != tt.wantDir {
				t.Errorf("effectiveWorkingDir() dir = %q, want %q", dir, tt.wantDir)
			}
			if src != tt.wantSource {
				t.Errorf("effectiveWorkingDir() source = %q, want %q", src, tt.wantSource)
			}
		})
	}
}

// TestSetActiveProjectPathResolver_NilSafe: the setter follows the package's
// nil-receiver convention.
func TestSetActiveProjectPathResolver_NilSafe(t *testing.T) {
	var h *ChatHandler
	h.SetActiveProjectPathResolver(func() string { return "/x" })
	h.SetDefaultWorkingDir("/x")
}
