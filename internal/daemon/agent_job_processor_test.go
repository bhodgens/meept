package daemon

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// newTestAgentJobProcessor builds a minimal AgentJobProcessor wired to an
// in-memory session store and a temp-dir-backed task store, sufficient for
// resolveStepWorkingDir tests (no agent loop needed).
func newTestAgentJobProcessor(t *testing.T) (*AgentJobProcessor, *session.MemoryStore, *task.Store) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ss := session.NewMemoryStore(logger)

	ts, err := task.NewStore(t.TempDir()+"/tasks.db", logger)
	if err != nil {
		t.Fatalf("create task store: %v", err)
	}
	t.Cleanup(func() {
		if err := ts.Close(); err != nil {
			t.Errorf("close task store: %v", err)
		}
	})

	p := &AgentJobProcessor{
		sessionStore: ss,
		taskStore:    ts,
		logger:       logger,
	}
	return p, ss, ts
}

// seedTestSession creates a session and applies mutations to the stored
// pointer (MemoryStore keeps the same *Session it returns).
func seedTestSession(t *testing.T, ss *session.MemoryStore, mutate func(*session.Session)) {
	t.Helper()
	sess, err := ss.Create("step-cwd-test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if mutate != nil {
		mutate(sess)
	}
}

// seedTestTask inserts a task with a fixed ID and links the given sessions
// (conversation IDs), mirroring the chat-path task linkage.
func seedTestTask(t *testing.T, ts *task.Store, id string, linkedSessions []string) {
	t.Helper()
	tk := task.NewTask("step-cwd-task", "test task for resolveStepWorkingDir")
	tk.ID = id
	if err := ts.Create(tk); err != nil {
		t.Fatalf("create task %s: %v", id, err)
	}
	for _, sessID := range linkedSessions {
		if err := ts.LinkSession(id, sessID); err != nil {
			t.Fatalf("link session %s to task %s: %v", sessID, id, err)
		}
	}
}

// TestResolveStepWorkingDir_CWDFallback covers the `meept chat --cwd` case:
// a linked session with only a DetectionContext CWD (no ProjectPath or
// WorktreePath binding) must supply the step-job working directory, and
// existing project-bound behavior must be preserved.
func TestResolveStepWorkingDir_CWDFallback(t *testing.T) {
	p, ss, ts := newTestAgentJobProcessor(t)

	// Session created via `meept chat --cwd /tmp/user-session-dir`: only a
	// CWD, no ProjectPath/WorktreePath.
	seedTestSession(t, ss, func(s *session.Session) {
		s.ConversationID = "conv-cwd-1"
		s.DetectionContext = &session.DetectionContext{CWD: "/tmp/user-session-dir"}
	})
	seedTestTask(t, ts, "task-cwd-1", []string{"conv-cwd-1"})

	job := &queue.Job{TaskID: "task-cwd-1"}
	if got := p.resolveStepWorkingDir(job); got != "/tmp/user-session-dir" {
		t.Errorf("resolveStepWorkingDir = %q, want session CWD %q", got, "/tmp/user-session-dir")
	}

	// A project-bound session keeps existing behavior: ProjectPath wins.
	seedTestSession(t, ss, func(s *session.Session) {
		s.ConversationID = "conv-proj-1"
		s.ProjectPath = "/tmp/user-project"
	})
	seedTestTask(t, ts, "task-proj-1", []string{"conv-proj-1"})
	job = &queue.Job{TaskID: "task-proj-1"}
	if got := p.resolveStepWorkingDir(job); got != "/tmp/user-project" {
		t.Errorf("resolveStepWorkingDir = %q, want ProjectPath %q", got, "/tmp/user-project")
	}

	// A task with no linked sessions resolves to "" (caller falls back to
	// the loop default).
	seedTestTask(t, ts, "task-unlinked-1", nil)
	job = &queue.Job{TaskID: "task-unlinked-1"}
	if got := p.resolveStepWorkingDir(job); got != "" {
		t.Errorf("resolveStepWorkingDir = %q, want %q for task without linked sessions", got, "")
	}
}

// TestResolveStepWorkingDir_Precedence locks the full lookup order:
// WorktreePath > ProjectPath > session CWD > "".
func TestResolveStepWorkingDir_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		worktree string
		project  string
		cwd      string
		want     string
	}{
		{name: "worktree wins over project", worktree: "/wt", project: "/proj", cwd: "/cwd", want: "/wt"},
		{name: "project wins over cwd", project: "/proj", cwd: "/cwd", want: "/proj"},
		{name: "cwd used when others empty", cwd: "/cwd", want: "/cwd"},
		{name: "empty when nothing set", want: ""},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ss, ts := newTestAgentJobProcessor(t)

			convID := fmt.Sprintf("conv-prec-%d", i)
			taskID := fmt.Sprintf("task-prec-%d", i)
			seedTestSession(t, ss, func(s *session.Session) {
				s.ConversationID = convID
				s.WorktreePath = tt.worktree
				s.ProjectPath = tt.project
				s.DetectionContext = &session.DetectionContext{CWD: tt.cwd}
			})
			seedTestTask(t, ts, taskID, []string{convID})

			job := &queue.Job{TaskID: taskID}
			if got := p.resolveStepWorkingDir(job); got != tt.want {
				t.Errorf("resolveStepWorkingDir = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveStepWorkingDir_TaskIDFallbackCWD covers the fallback block where
// job.TaskID doubles as a conversation key (no task-store entry): a session
// with only a CWD must still supply the working directory.
func TestResolveStepWorkingDir_TaskIDFallbackCWD(t *testing.T) {
	p, ss, _ := newTestAgentJobProcessor(t)

	seedTestSession(t, ss, func(s *session.Session) {
		s.ConversationID = "task-conv-cwd"
		s.DetectionContext = &session.DetectionContext{CWD: "/tmp/fallback-cwd"}
	})

	job := &queue.Job{TaskID: "task-conv-cwd"}
	if got := p.resolveStepWorkingDir(job); got != "/tmp/fallback-cwd" {
		t.Errorf("resolveStepWorkingDir = %q, want fallback-block CWD %q", got, "/tmp/fallback-cwd")
	}
}
