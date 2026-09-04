package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
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

// --- Job-level quota surfacing (chat-dispatch-ux leaf 06) -------------------
//
// The primary loop parks quota waits via QuotaResumeWatcher and the goal
// loop has its own parker, but the step-job path (AgentJobProcessor.Process
// → RunOnce) just returned "agent execution failed: ..." — the tactical
// scheduler marked the step failed and the user never heard the word quota
// (2026-09-04 finding F6: "my tools aren't working"). These tests lock the
// job-level backstop: classify the escaped *llm.QuotaResetError, publish the
// EXISTING agent.quota_wait bus event (WS classifies agent.quota* as
// agent_progress — no new topic), and stamp the returned error with the
// user-facing quota sentence (stored verbatim as the step Result by
// TacticalScheduler.OnJobFailed → stepStore.SetResult, tactical.go:1116, so
// the chat reply quotes it).

// quotaFailChatter is the minimal llm.Chatter stub for the job-level quota
// tests: every Chat fails with the configured error — the scripted stand-in
// for a provider quota block that exhausted the loop's rotation chain.
// Mirrors the agent package's errChatter (unexported there).
type quotaFailChatter struct {
	err   error
	calls int32
}

func (c *quotaFailChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	atomic.AddInt32(&c.calls, 1)
	return nil, c.err
}

func (c *quotaFailChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *quotaFailChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "quota-chatter"}
}

// newQuotaTestLoop builds a bare agent loop wired to the failing chatter.
// No modelRef/resolver: the loop's quota branch cannot rotate, so the
// *llm.QuotaResetError surfaces on the first attempt — exactly the
// every-candidate-blocked terminal state the job-level backstop covers.
func newQuotaTestLoop(t *testing.T, chatter llm.Chatter) *agent.AgentLoop {
	t.Helper()
	return agent.NewAgentLoop("sess-quota-job-test", t.TempDir(),
		agent.WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		agent.WithLLMChatter(chatter),
		agent.WithSecurityChecker(security.NewPermissionChecker(security.Config{})),
	)
}

// drainTestSubscriber non-blockingly collects everything currently queued on
// a bus subscriber channel (Publish is synchronous per subscriber, so by the
// time Process returns the event is already buffered).
func drainTestSubscriber(sub *bus.Subscriber) []*models.BusMessage {
	var msgs []*models.BusMessage
	for {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				return msgs
			}
			msgs = append(msgs, m)
		default:
			return msgs
		}
	}
}

// stepJobPayload builds a step-job payload like the orchestrator emits.
func stepJobPayload(t *testing.T, stepID, taskID, description string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"step_id":     stepID,
		"task_id":     taskID,
		"description": description,
	})
	if err != nil {
		t.Fatalf("marshal step payload: %v", err)
	}
	return b
}

// quotaTestError is the agnes-shaped 429: unknown absolute reset, 30m
// RetryAfter — unblock_at must derive from now+RetryAfter.
func quotaTestError() *llm.QuotaResetError {
	return &llm.QuotaResetError{
		ProviderID: "agnes",
		ModelID:    "agnes-2.5-flash",
		Code:       "usage_limit_reached",
		StatusCode: http.StatusTooManyRequests,
		MaxWait:    24 * time.Hour,
		RetryAfter: 30 * time.Minute,
	}
}

// TestAgentJobProcessor_QuotaErrorPublishesEvent (leaf 06 Task 1): a step
// job failing terminal on *llm.QuotaResetError publishes exactly one
// agent.quota_wait event with the contract payload (class, agent_id,
// task_id, conversation_id/session_id from the task's linked session,
// RFC3339 unblock_at, lowercase message), and the returned error keeps the
// "agent execution failed:" prefix and carries the quota sentence with
// errors.As still reaching the QuotaResetError.
func TestAgentJobProcessor_QuotaErrorPublishesEvent(t *testing.T) {
	p, ss, ts := newTestAgentJobProcessor(t)

	sess, err := ss.Create("quota-surface-test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ConversationID = "conv-quota-1"
	seedTestTask(t, ts, "task-quota-1", []string{"conv-quota-1"})

	b := bus.New(nil, slog.New(slog.DiscardHandler))
	sub := b.Subscribe("quota-test-sub", "agent.quota_wait")
	t.Cleanup(func() { b.Unsubscribe(sub) })
	p.WithBus(b)

	chatter := &quotaFailChatter{err: quotaTestError()}
	p.agentLoop = newQuotaTestLoop(t, chatter)

	before := time.Now()
	job := &queue.Job{
		ID:      "job-quota-1",
		TaskID:  "task-quota-1",
		AgentID: "coder",
		Type:    queue.JobTypeProjectTask,
		Payload: stepJobPayload(t, "step-quota-1", "task-quota-1", "write the report"),
	}
	_, perr := p.Process(context.Background(), job)
	if perr == nil {
		t.Fatal("Process = nil error, want quota failure to propagate")
	}
	if got := atomic.LoadInt32(&chatter.calls); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (terminal quota error, no retry)", got)
	}

	events := drainTestSubscriber(sub)
	if len(events) != 1 {
		t.Fatalf("got %d quota_wait events, want 1", len(events))
	}
	if events[0].Topic != "agent.quota_wait" {
		t.Errorf("event topic = %q, want agent.quota_wait", events[0].Topic)
	}
	var ev map[string]any
	if err := json.Unmarshal(events[0].Payload, &ev); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if v, ok := ev["class"].(string); !ok || v != "quota_wait" {
		t.Errorf("class = %v, want quota_wait", ev["class"])
	}
	if v, ok := ev["agent_id"].(string); !ok || v != "coder" {
		t.Errorf("agent_id = %v, want coder", ev["agent_id"])
	}
	if v, ok := ev["task_id"].(string); !ok || v != "task-quota-1" {
		t.Errorf("task_id = %v, want task-quota-1", ev["task_id"])
	}
	if v, ok := ev["conversation_id"].(string); !ok || v != "conv-quota-1" {
		t.Errorf("conversation_id = %v, want conv-quota-1 (from task linked sessions)", ev["conversation_id"])
	}
	if v, ok := ev["session_id"].(string); !ok || v != sess.ID {
		t.Errorf("session_id = %v, want %q", ev["session_id"], sess.ID)
	}
	unblockRaw, ok := ev["unblock_at"].(string)
	if !ok || unblockRaw == "" {
		t.Fatalf("unblock_at missing or not a string: %v", ev["unblock_at"])
	}
	unblock, err := time.Parse(time.RFC3339, unblockRaw)
	if err != nil {
		t.Fatalf("unblock_at %q is not RFC3339: %v", unblockRaw, err)
	}
	lo := before.Add(29 * time.Minute)
	hi := time.Now().Add(31 * time.Minute)
	if unblock.Before(lo) || unblock.After(hi) {
		t.Errorf("unblock_at = %v, want now+30m (window %v..%v)", unblock, lo, hi)
	}
	for _, key := range []string{"provider_id", "model_id", "message"} {
		if v, ok := ev[key].(string); !ok || v == "" {
			t.Errorf("%s missing or empty: %v", key, ev[key])
		}
	}
	// Lowercase one-liner: the human wording is lowercase (the RFC3339
	// timestamp keeps its conventional uppercase T separator).
	if msg, _ := ev["message"].(string); !strings.HasPrefix(msg, "quota wait: ") ||
		!strings.HasSuffix(msg, "your request is saved and will need a re-ask once the limit resets.") {
		t.Errorf("message = %q, want the lowercase quota one-liner shape", msg)
	}

	// Returned-error contract: legacy prefix + quota sentence; errors.As
	// reaches the QuotaResetError through the wrap chain.
	if !strings.Contains(perr.Error(), "agent execution failed:") {
		t.Errorf("error %q lacks the legacy agent-execution prefix", perr.Error())
	}
	if !strings.Contains(perr.Error(), "quota wait: agnes/agnes-2.5-flash is rate-limited until ") {
		t.Errorf("error %q lacks the quota sentence with provider/model", perr.Error())
	}
	if !strings.Contains(perr.Error(), "your request is saved and will need a re-ask once the limit resets.") {
		t.Errorf("error %q lacks the saved/re-ask sentence", perr.Error())
	}
	var gotQ *llm.QuotaResetError
	if !errors.As(perr, &gotQ) {
		t.Fatalf("errors.As on returned err = false, want QuotaResetError reachable through wrap")
	}
	if gotQ.ProviderID != "agnes" || gotQ.ModelID != "agnes-2.5-flash" {
		t.Errorf("unwrapped quota error = %s/%s, want agnes/agnes-2.5-flash", gotQ.ProviderID, gotQ.ModelID)
	}
}

// TestAgentJobProcessor_QuotaErrorResultStampNilBus (leaf 06 Task 2): the
// returned error string IS the step Result — TacticalScheduler.OnJobFailed
// stores it verbatim via stepStore.SetResult(step.ID, jobErr)
// (tactical.go:1116), so asserting the string here is asserting the stored
// Result the chat reply quotes. Runs with a nil bus, proving the
// nil-bus guard: no publish, no panic, sentence still stamped.
func TestAgentJobProcessor_QuotaErrorResultStampNilBus(t *testing.T) {
	p, _, _ := newTestAgentJobProcessor(t) // bus intentionally left nil

	chatter := &quotaFailChatter{err: quotaTestError()}
	p.agentLoop = newQuotaTestLoop(t, chatter)

	job := &queue.Job{
		ID:      "job-quota-nilbus",
		TaskID:  "task-quota-nilbus",
		AgentID: "coder",
		Type:    queue.JobTypeProjectTask,
		Payload: stepJobPayload(t, "step-nilbus", "task-quota-nilbus", "write the report"),
	}
	_, perr := p.Process(context.Background(), job)
	if perr == nil {
		t.Fatal("Process = nil error, want quota failure to propagate")
	}

	// This exact string is what the tactical failure path writes as the
	// step Result: prefix first (error-shape compatible), quota sentence
	// present, no doubled prefix.
	result := perr.Error()
	if !strings.HasPrefix(result, "agent execution failed: ") {
		t.Errorf("result %q lacks the agent-execution prefix", result)
	}
	if n := strings.Count(result, "agent execution failed:"); n != 1 {
		t.Errorf("result has %d agent-execution prefixes, want 1: %q", n, result)
	}
	if !strings.Contains(result, "quota wait: agnes/agnes-2.5-flash is rate-limited until ") {
		t.Errorf("result %q lacks the quota sentence with provider/model", result)
	}
	if !strings.Contains(result, "your request is saved and will need a re-ask once the limit resets.") {
		t.Errorf("result %q lacks the saved/re-ask sentence", result)
	}
}

// TestAgentJobProcessor_NonQuotaErrorNoEvent locks the invariant that
// non-quota errors keep byte-identical legacy behavior: the plain
// "agent execution failed: %w" wrap, and NO agent.quota_wait event.
func TestAgentJobProcessor_NonQuotaErrorNoEvent(t *testing.T) {
	p, _, _ := newTestAgentJobProcessor(t)

	b := bus.New(nil, slog.New(slog.DiscardHandler))
	sub := b.Subscribe("quota-test-sub-2", "agent.quota_wait")
	t.Cleanup(func() { b.Unsubscribe(sub) })
	p.WithBus(b)

	chatter := &quotaFailChatter{err: errors.New("kaboom")}
	p.agentLoop = newQuotaTestLoop(t, chatter)

	job := &queue.Job{
		ID:      "job-nonquota-1",
		TaskID:  "task-nonquota-1",
		AgentID: "coder",
		Type:    queue.JobTypeProjectTask,
		Payload: stepJobPayload(t, "step-nonquota", "task-nonquota-1", "write the report"),
	}
	_, perr := p.Process(context.Background(), job)
	if perr == nil {
		t.Fatal("Process = nil error, want failure to propagate")
	}
	// The loop wraps its own LLM failure ("LLM call failed: kaboom") before
	// the processor sees it; byte-identical means the processor adds only
	// its legacy prefix on top of what the loop returned — reproduced here
	// with the loop's own wrap verb.
	want := fmt.Sprintf("agent execution failed: LLM call failed: %v", chatter.err)
	if perr.Error() != want {
		t.Errorf("err = %q, want legacy wrap %q", perr.Error(), want)
	}
	if events := drainTestSubscriber(sub); len(events) != 0 {
		t.Errorf("got %d quota_wait events for a non-quota error, want 0", len(events))
	}
}
