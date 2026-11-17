package agent

// review_workdir_test.go — pins issue #137 mechanism 2, the code-shaped half:
// the code-reviewer validation lane resolved NO working directory of its own.
//
// The lane: review_manager.go ReviewStep -> registry.Get("code-reviewer"),
// whose Get() shim routes to the "_default" task bucket (registry.go:549).
// createLoop builds that loop with an EMPTY working dir (registry.go:964),
// and nothing ever set one — so the reviewer's file_read ran with
// l.workingDir == "" (the tools fell through to the daemon CWD) and a fence
// denial in the reviewer lane could not be traced to any directory. Every
// transcript shows the lane on the step conversation, so the bug is
// load-correlated: inside a full suite the shared loop carries whatever dir
// the last task stamped.
//
// Contract: the reviewer loop is scoped to the STEP (GetForTask) and inherits
// the step's session working dir — WorktreePath > ProjectPath >
// DetectionContext.CWD (session.ResolveWorkingDir) — the same precedence the
// step job and the chat lane use, resolved and stamped BEFORE the turn runs.
// The dir is read back off the cached task-scoped loop (the same instance the
// reviewer turn runs on), which is what the fence's decision log reads.

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/security"
)

// stepWorkingDirSessionStore serves a fixed set of sessions by id or
// conversation id (SessionStoreReader's two lookup keys).
type stepWorkingDirSessionStore struct{ sessions []*session.Session }

func (s stepWorkingDirSessionStore) find(id string) *session.Session {
	for _, sess := range s.sessions {
		if sess == nil {
			continue
		}
		if sess.ID == id || sess.ConversationID == id {
			return sess
		}
	}
	return nil
}

func (s stepWorkingDirSessionStore) Get(id string) *session.Session { return s.find(id) }

func (s stepWorkingDirSessionStore) GetByConversationID(conversationID string) *session.Session {
	return s.find(conversationID)
}

// newReviewerChatter is the minimal Chatter for the reviewer loop. It answers
// "approved" (ReviewStep's default parse for unrecognized text), so the full
// lane (loop resolution, turn, parse) runs; the tests assert on the RESOLVED
// DIR, never on the verdict.
type newReviewerChatter struct{}

func (newReviewerChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "approved"}, nil
}

func (newReviewerChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return newReviewerChatter{}.Chat(ctx, messages, opts...)
}

func (newReviewerChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{} }

// newReviewerWorkdirRegistry builds a registry carrying a coder spec (the
// step's actor) plus a code-reviewer spec on a working chatter, so the review
// manager resolves a real reviewer loop.
func newReviewerWorkdirRegistry(t *testing.T) *AgentRegistry {
	t.Helper()
	reg := NewAgentRegistry(RegistryConfig{
		Logger:          slog.New(slog.DiscardHandler),
		SharedChatter:   newReviewerChatter{},
		ToolRegistry:    NewPlaceholderToolRegistry(),
		SecurityChecker: security.NewPermissionChecker(security.Config{}),
	})
	if err := reg.RegisterSpec(&AgentSpec{
		ID:   config.AgentIDCoder,
		Name: "Coder",
		Role: RoleExecutor,
	}); err != nil {
		t.Fatalf("register coder spec: %v", err)
	}
	if err := reg.RegisterSpec(&AgentSpec{
		ID:          "code-reviewer",
		Name:        "Code Reviewer",
		Role:        RoleReviewer,
		Constraints: AgentConstraints{MaxIterations: 3},
	}); err != nil {
		t.Fatalf("register code-reviewer spec: %v", err)
	}
	// SelectReviewer falls back to "test-reviewer" when no reviewer spec
	// matches the step's domain (no reviews_domain on these specs).
	if err := reg.RegisterSpec(&AgentSpec{
		ID:          "test-reviewer",
		Name:        "Test Reviewer",
		Role:        RoleReviewer,
		Constraints: AgentConstraints{MaxIterations: 3},
	}); err != nil {
		t.Fatalf("register test-reviewer spec: %v", err)
	}
	return reg
}

// stepBackedReviewManager wires a ReviewManager over a REAL step store AND a
// real task store, so the pre-review gates (SetState, policy, isTrivialTask,
// linked-session fallback) all run their production path.
func stepBackedReviewManager(t *testing.T, reg *AgentRegistry, store SessionStoreReader) *ReviewManager {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open step db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	stepStore, err := task.NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("create step store: %v", err)
	}
	taskStore, err := task.NewStore(t.TempDir()+"/tasks.db", slogDiscardLogger())
	if err != nil {
		t.Fatalf("create task store: %v", err)
	}
	return NewReviewManager(ReviewManagerConfig{
		Registry:     reg,
		StepStore:    stepStore,
		TaskStore:    taskStore,
		SessionStore: store,
		Logger:       slog.New(slog.DiscardHandler),
	})
}

// seedReviewSteps persists a task row plus the given steps so ReviewStep's
// gates run their production path. isTrivialTask (<3 steps) short-circuits
// the LLM reviewer, so callers that need the full lane seed 3+ steps.
func seedReviewSteps(t *testing.T, rm *ReviewManager, taskID string, steps ...*task.TaskStep) {
	t.Helper()
	if err := rm.taskStore.Create(&task.Task{ID: taskID}); err != nil {
		t.Fatalf("create task %s: %v", taskID, err)
	}
	for _, step := range steps {
		cp := *step
		cp.State = task.StepCompleted
		if err := rm.stepStore.Create(&cp); err != nil {
			t.Fatalf("create step %s: %v", cp.ID, err)
		}
	}
}

// reviewerLoopDir returns the cached task-scoped reviewer loop's working dir —
// the value the executor's fence decision log reads. SelectReviewer falls back
// to "test-reviewer" when no reviewer spec matches the step's domain, so the
// test registry carries that spec too.
func reviewerLoopDir(t *testing.T, reg *AgentRegistry, taskID string) string {
	t.Helper()
	loop, err := reg.GetForTask("test-reviewer", taskID)
	if err != nil {
		t.Fatalf("get reviewer loop: %v", err)
	}
	return loop.GetWorkingDir()
}

// TestReviewerLoopInheritsStepWorkingDir pins the fix: ReviewStep resolves the
// step's session working dir and stamps it on the task-scoped reviewer loop.
func TestReviewerLoopInheritsStepWorkingDir(t *testing.T) {
	reg := newReviewerWorkdirRegistry(t)

	sess := &session.Session{
		ID:             "session-137",
		ConversationID: "conv-137",
		WorktreePath:   "/tmp/meept-137-worktree",
	}
	rm := stepBackedReviewManager(t, reg, stepWorkingDirSessionStore{sessions: []*session.Session{sess}})

	// Snapshot the loop BEFORE the review: the reviewer must reuse this
	// task-scoped instance and stamp the resolved dir on it. SelectReviewer
	// falls back to "test-reviewer" for these specs (no reviews_domain), so
	// that is the loop the lane resolves.
	before, err := reg.GetForTask("test-reviewer", "task-137")
	if err != nil {
		t.Fatalf("pre-create reviewer loop: %v", err)
	}

	step := &task.TaskStep{
		ID:        "step-137",
		TaskID:    "task-137",
		AgentID:   config.AgentIDCoder,
		ToolHint:  string(IntentCode),
		Result:    "changed the thing",
		SessionID: "session-137",
	}
	// 3 steps: isTrivialTask (<3 steps) would skip the LLM reviewer.
	seedReviewSteps(t, rm, "task-137", step,
		&task.TaskStep{ID: "step-137-b", TaskID: "task-137"},
		&task.TaskStep{ID: "step-137-c", TaskID: "task-137"},
	)

	if _, err := rm.ReviewStep(context.Background(), step, nil); err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}

	if got := reviewerLoopDir(t, reg, "task-137"); got != sess.WorktreePath {
		t.Errorf("reviewer loop working dir = %q, want %q (the step's session dir)", got, sess.WorktreePath)
	}
	if got := before.GetWorkingDir(); got != sess.WorktreePath {
		t.Errorf("pre-created reviewer loop working dir = %q, want %q (the review lane must reuse and stamp the task-scoped loop)", got, sess.WorktreePath)
	}
}

// TestReviewerLoopWorkingDirNotShared pins the isolation half: two tasks with
// different session dirs each get a task-scoped reviewer loop carrying its OWN
// dir — a shared _default loop would carry whichever dir was stamped last (the
// load-correlated shape from #137: the reviewer lane only diverges inside a
// full suite, where many tasks share one daemon).
func TestReviewerLoopWorkingDirNotShared(t *testing.T) {
	reg := newReviewerWorkdirRegistry(t)

	sessA := &session.Session{ID: "session-a", ConversationID: "conv-a", ProjectPath: "/tmp/meept-137-a"}
	sessB := &session.Session{ID: "session-b", ConversationID: "conv-b", ProjectPath: "/tmp/meept-137-b"}
	rm := stepBackedReviewManager(t, reg,
		stepWorkingDirSessionStore{sessions: []*session.Session{sessA, sessB}})

	stepA := &task.TaskStep{ID: "step-a", TaskID: "task-a", AgentID: config.AgentIDCoder, ToolHint: string(IntentCode), Result: "did a thing", SessionID: "session-a"}
	stepB := &task.TaskStep{ID: "step-b", TaskID: "task-b", AgentID: config.AgentIDCoder, ToolHint: string(IntentCode), Result: "did a thing", SessionID: "session-b"}
	seedReviewSteps(t, rm, "task-a", stepA,
		&task.TaskStep{ID: "step-a2", TaskID: "task-a"}, &task.TaskStep{ID: "step-a3", TaskID: "task-a"})
	seedReviewSteps(t, rm, "task-b", stepB,
		&task.TaskStep{ID: "step-b2", TaskID: "task-b"}, &task.TaskStep{ID: "step-b3", TaskID: "task-b"})

	for _, step := range []*task.TaskStep{stepA, stepB} {
		if _, err := rm.ReviewStep(context.Background(), step, nil); err != nil {
			t.Fatalf("ReviewStep(%s): %v", step.TaskID, err)
		}
	}

	if got := reviewerLoopDir(t, reg, "task-a"); got != sessA.ProjectPath {
		t.Errorf("reviewer loop A dir = %q, want %q", got, sessA.ProjectPath)
	}
	if got := reviewerLoopDir(t, reg, "task-b"); got != sessB.ProjectPath {
		t.Errorf("reviewer loop B dir = %q, want %q", got, sessB.ProjectPath)
	}
}

// TestResolveStepWorkingDir covers the ReviewManager's resolution seam: the
// session-bound precedence (WorktreePath > ProjectPath > DetectionContext.CWD),
// the step's own SessionID hint, and the conversation-id fallback.
func TestResolveStepWorkingDir(t *testing.T) {
	worktree := &session.Session{ID: "s1", ConversationID: "c1", WorktreePath: "/wt", ProjectPath: "/proj"}
	project := &session.Session{ID: "s2", ConversationID: "c2", ProjectPath: "/proj"}
	detected := &session.Session{ID: "s3", ConversationID: "c3"}
	detected.DetectionContext = &session.DetectionContext{CWD: "/detected"}
	rm := NewReviewManager(ReviewManagerConfig{
		Registry:     newReviewerWorkdirRegistry(t),
		SessionStore: stepWorkingDirSessionStore{sessions: []*session.Session{worktree, project, detected}},
		Logger:       slog.New(slog.DiscardHandler),
	})

	cases := []struct {
		name       string
		step       *task.TaskStep
		wantDir    string
		wantSource session.WorkingDirSource
	}{
		{"worktree wins", &task.TaskStep{SessionID: "s1"}, "/wt", session.WorkingDirFromWorktree},
		{"project second", &task.TaskStep{SessionID: "s2"}, "/proj", session.WorkingDirFromProject},
		{"detection third", &task.TaskStep{SessionID: "s3"}, "/detected", session.WorkingDirFromDetection},
		{"conversation id resolves the session", &task.TaskStep{SessionID: "c2"}, "/proj", session.WorkingDirFromProject},
		{"unknown session resolves nothing", &task.TaskStep{SessionID: "nope"}, "", session.WorkingDirFromNone},
		{"no hint at all", &task.TaskStep{}, "", session.WorkingDirFromNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, src := rm.resolveStepWorkingDir(tc.step)
			if dir != tc.wantDir || src != tc.wantSource {
				t.Errorf("resolveStepWorkingDir = (%q, %q), want (%q, %q)", dir, src, tc.wantDir, tc.wantSource)
			}
		})
	}
}

// TestResolveStepWorkingDir_StepStore proves the production wiring path: the
// daemon builds the ReviewManager with a StepStore whose Create persists
// SessionID (migration 24). Resolution must read it back from the DB when the
// in-memory step copy carries no stamp.
func TestResolveStepWorkingDir_StepStore(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open step db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	stepStore, err := task.NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("create step store: %v", err)
	}

	sess := &session.Session{ID: "sess-db", ConversationID: "conv-db", WorktreePath: "/from-db"}
	if err := stepStore.Create(&task.TaskStep{
		ID:        "step-db",
		TaskID:    "task-db",
		State:     task.StepCompleted,
		SessionID: "sess-db",
	}); err != nil {
		t.Fatalf("create step: %v", err)
	}

	rm := NewReviewManager(ReviewManagerConfig{
		Registry:     newReviewerWorkdirRegistry(t),
		StepStore:    stepStore,
		SessionStore: stepWorkingDirSessionStore{sessions: []*session.Session{sess}},
		Logger:       slog.New(slog.DiscardHandler),
	})

	// The step passed in is a fresh copy with the same ID: only the store's
	// row carries SessionID.
	dir, src := rm.resolveStepWorkingDir(&task.TaskStep{ID: "step-db", TaskID: "task-db"})
	if dir != "/from-db" || src != session.WorkingDirFromWorktree {
		t.Errorf("resolveStepWorkingDir = (%q, %q), want (/from-db, %q)", dir, src, session.WorkingDirFromWorktree)
	}
}

// TestResolveStepWorkingDir_LinkedSessionFallback covers the no-step-stamp
// path: the task's linked sessions resolve the dir, by conversation id too.
// (linked_sessions rows are written through LinkSession — Create persists the
// task row only.)
func TestResolveStepWorkingDir_LinkedSessionFallback(t *testing.T) {
	store, err := task.NewStore(t.TempDir()+"/tasks.db", nil)
	if err != nil {
		t.Fatalf("create task store: %v", err)
	}
	linked := &session.Session{ID: "sess-linked", ConversationID: "conv-linked", ProjectPath: "/linked"}
	if err := store.Create(&task.Task{ID: "task-linked"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := store.LinkSession("task-linked", "conv-linked"); err != nil {
		t.Fatalf("link session: %v", err)
	}
	rm := NewReviewManager(ReviewManagerConfig{
		Registry:     newReviewerWorkdirRegistry(t),
		TaskStore:    store,
		SessionStore: stepWorkingDirSessionStore{sessions: []*session.Session{linked}},
		Logger:       slog.New(slog.DiscardHandler),
	})

	dir, src := rm.resolveStepWorkingDir(&task.TaskStep{ID: "step-linked", TaskID: "task-linked"})
	if dir != "/linked" || src != session.WorkingDirFromProject {
		t.Errorf("resolveStepWorkingDir = (%q, %q), want (/linked, %q)", dir, src, session.WorkingDirFromProject)
	}
}
