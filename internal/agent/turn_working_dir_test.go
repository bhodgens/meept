package agent

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// Turn-start working-directory binding.
//
// fresh-rig daemon11 (2026-09-13): the daemon served turns for a session with
// no project (and no client CWD), sessionLoop returned the singleton loop, the
// loop's working directory was empty, and every filesystem tool that needed a
// session directory failed. These tests pin the turn-start resolution:
// WorktreePath > ProjectPath > DetectionContext.CWD, then the daemon's
// configured default. Never the daemon's own CWD.
//
// Project scoping is PER-SESSION: there is NO global active-project fallback.
// A session with no worktree, no project and no client CWD resolves only the
// configured default (or nothing), even when some other project is active.

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

// newCapturingTurnWorkdirHandler is newTurnWorkdirHandler with a logger whose
// records are captured, so a test can assert the unbound-turn WARN fired.
func newCapturingTurnWorkdirHandler(t *testing.T, sessions map[string]*session.Session) (*ChatHandler, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	singleton := NewAgentLoop("singleton", "/tmp")
	h := NewChatHandler(singleton, nil, nil, logger)
	if sessions != nil {
		h.SetSessionStore(&stubSessionStore{sessions: sessions})
	}
	h.SetAgentLoopManager(NewManager(ManagerConfig{}))
	return h, buf
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

// TestSessionLoop_PerSessionProjectIsolation: two sessions bound to different
// projects each resolve their OWN project. There is no global active project
// to leak into either of them.
func TestSessionLoop_PerSessionProjectIsolation(t *testing.T) {
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		"sess-a": {ID: "sess-a", ConversationID: "sess-a", ProjectID: "proj-a", ProjectPath: "/repos/a"},
		"sess-b": {ID: "sess-b", ConversationID: "sess-b", ProjectID: "proj-b", ProjectPath: "/repos/b"},
	})

	loopA := h.sessionLoop("sess-a")
	if loopA == nil || loopA.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop for sess-a, got %v", loopA)
	}
	if got := loopA.GetWorkingDir(); got != "/repos/a" {
		t.Errorf("sess-a GetWorkingDir() = %q, want /repos/a", got)
	}
	loopB := h.sessionLoop("sess-b")
	if loopB == nil || loopB.GetSessionID() == "singleton" {
		t.Fatalf("expected a session-scoped loop for sess-b, got %v", loopB)
	}
	if got := loopB.GetWorkingDir(); got != "/repos/b" {
		t.Errorf("sess-b GetWorkingDir() = %q, want /repos/b", got)
	}
	if loopA.GetWorkingDir() == loopB.GetWorkingDir() {
		t.Fatal("sessions bound to different projects must not share a working directory")
	}
}

// TestSessionLoop_UnboundSessionHasNoProjectFallback: a fresh session with no
// project, no worktree and no client CWD resolves NOTHING. The only thing it
// can fall to is the configured daemon default; there is no active-project
// fallback, so with no default configured the singleton is kept.
func TestSessionLoop_UnboundSessionHasNoProjectFallback(t *testing.T) {
	const sessionID = "sess-unbound"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
	})
	// Another session in the same store IS bound to a project; it must not
	// leak into this unbound session's resolution.
	h.sessionStore = &stubSessionStore{sessions: map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
		"other":   {ID: "other", ConversationID: "other", ProjectID: "p", ProjectPath: "/repos/other"},
	}}

	loop := h.sessionLoop(sessionID)
	if loop == nil || loop.GetSessionID() != "singleton" {
		t.Fatalf("expected the singleton for an unbound session, got %v", loop)
	}
}

// TestSessionLoop_UnknownSessionHasNoProjectFallback: even with no session
// record at all, there is no active-project fallback — the turn keeps the
// singleton rather than borrowing another session's project.
func TestSessionLoop_UnknownSessionHasNoProjectFallback(t *testing.T) {
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		"other": {ID: "other", ConversationID: "other", ProjectID: "p", ProjectPath: "/repos/other"},
	})

	loop := h.sessionLoop("session-with-no-record")
	if loop == nil || loop.GetSessionID() != "singleton" {
		t.Fatalf("expected the singleton for an unknown session, got %v", loop)
	}
}

// TestSessionLoop_DaemonDefaultWorkingDirIsLastResort: with no session-bound
// directory, the daemon's configured default working dir applies.
func TestSessionLoop_DaemonDefaultWorkingDirIsLastResort(t *testing.T) {
	const sessionID = "sess-default-only"
	const configured = "/srv/meept-workspace"
	h := newTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
	})
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

// TestSessionLoop_UnboundSessionWarns: the unbound turn emits a WARN naming
// the session and why nothing resolved, so a pathless turn is debuggable from
// the daemon log alone. Together with the tools sentinel
// (tools.ErrNoWorkingDir) this is the "actionable failure" contract.
func TestSessionLoop_UnboundSessionWarns(t *testing.T) {
	const sessionID = "sess-warn"
	h, buf := newCapturingTurnWorkdirHandler(t, map[string]*session.Session{
		sessionID: {ID: sessionID, ConversationID: sessionID},
	})

	loop := h.sessionLoop(sessionID)
	if loop == nil || loop.GetSessionID() != "singleton" {
		t.Fatalf("expected the singleton for an unbound session, got %v", loop)
	}
	logged := buf.String()
	if !strings.Contains(logged, "level=WARN") {
		t.Fatalf("expected a WARN log line for the unbound turn, got:\n%s", logged)
	}
	if !strings.Contains(logged, "no working directory bound") {
		t.Fatalf("expected the WARN to name the missing working directory, got:\n%s", logged)
	}
	if !strings.Contains(logged, sessionID) {
		t.Fatalf("expected the WARN to name the session %q, got:\n%s", sessionID, logged)
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
// There is no active-project case: projects are per-session.
func TestEffectiveWorkingDir_Labels(t *testing.T) {
	const sessionID = "sess-labels"
	h := newTurnWorkdirHandler(t, nil)

	tests := []struct {
		name       string
		sess       *session.Session
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
			name:       "daemon default alone",
			sess:       &session.Session{ID: sessionID},
			defaultDir: "/default",
			wantDir:    "/default",
			wantSource: "daemon_default",
		},
		{
			name:       "unbound session has no project fallback",
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
			h.SetDefaultWorkingDir("")
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

// TestSetDefaultWorkingDir_NilSafe: the setter follows the package's
// nil-receiver convention.
func TestSetDefaultWorkingDir_NilSafe(t *testing.T) {
	var h *ChatHandler
	h.SetDefaultWorkingDir("/x")
}
