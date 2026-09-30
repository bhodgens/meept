package agent

// M1 pin (bughunt 2026-09-29): RouteToAgent's digest consumers must key on
// the SESSION-level conversation id.
//
// d045a450 fixed only the recall branch (dispatcher.go:3048). Two sibling
// sites still queried the thread-router-resolved id:
//
//   - buildContextMessage's session-context digest block (dispatcher.go:3023)
//   - the SetGuardFallback digest arm (dispatcher.go:3087-3092)
//
// Both call buildSessionContextDigestExcluding, whose GetTasksForSession
// JOINs session_tasks on the SESSION-level id. With a thread router wired,
// the thread-scoped id found no task links, so a threaded conversation lost
// the executing-agent digest block and the reply-guard fallback entirely —
// the A5 failure class the recall fix already closed.
//
// Both pins drive the REAL RouteToAgent with a thread router wired. The
// agent loop is built on a capturing chatter, so the assertions read the
// actual prompt the executing agent received (digest block) and the actual
// fallback the loop ships on a guard trip (SetGuardFallback arm).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

func completedStateForDigest() task.TaskState { return task.StateCompleted }
func timeNowForDigest() time.Time             { return time.Now().UTC().Add(-time.Minute) }
func stepApprovedForDigest() task.StepState   { return task.StepApproved }

func defaultTestTimeout() time.Duration { return 30 * time.Second }

// promptCapturingChatter records the messages of every LLM call and answers
// with a machine-shaped dump — so the RunOnce reply trips the reply guard
// and exercises the digest-armed guard fallback.
type promptCapturingChatter struct {
	loop      *AgentLoop
	callCount int
	prompts   []string
}

func (m *promptCapturingChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(msg.Content)
		sb.WriteString("\n")
	}
	m.prompts = append(m.prompts, sb.String())
	// Reset the terminal state machine so successive calls stay legal
	// (StateMaxIterations only transitions back to Idle) — and stay OUT of
	// StateQuotaWait, or the loop's turnParked guard would classify the
	// blank reply as a parked turn.
	if m.loop != nil && m.loop.stateMachine != nil {
		_ = m.loop.stateMachine.Transition(StateIdle, "test_chatter_reset", map[string]any{})
	}
	// Vary per call so the loop's convergence detector never sees a repeat.
	return &llm.Response{
		Content: `{"memory_id":"` + strings.Repeat("m", m.callCount) + `","success":true}`,
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *promptCapturingChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *promptCapturingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "prompt-capturing-chatter"}
}

// newDigestThreadRouterDispatcher wires the full production chain the pins
// need: task registry (session_tasks), thread router over a store holding
// the session, registry with a chat spec, loop manager, and the capturing
// chatter as the LLM. RouteToAgent resolves the SESSION-scoped loop through
// the loop manager (session ProjectPath set), which is the loop the digest
// block and guard fallback arm.
func newDigestThreadRouterDispatcher(t *testing.T) (*Dispatcher, *promptCapturingChatter, *task.Registry) {
	t.Helper()

	logger := digestTestLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	threadStore := newMockThreadStore()
	const sessionID = "sess-digest-thread"
	const sessionConvID = "conv-digestthread"
	threadStore.addSession(&session.Session{
		ID:             sessionID,
		ConversationID: sessionConvID,
		Threads:        nil,
		ProjectPath:    "/tmp/digest-thread-project",
	})
	router := NewThreadRouter(WithThreadRouterSessionStore(threadStore))

	chatter := &promptCapturingChatter{}
	registry := NewAgentRegistry(RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&AgentSpec{
		ID:   "coder",
		Name: "coder",
		Role: "executor",
		Constraints: AgentConstraints{
			MaxIterations: 6,
			Timeout:       defaultTestTimeout(),
		},
	}); err != nil {
		t.Fatalf("RegisterSpec: %v", err)
	}
	// The session-scoped loop is wired from the registry template; give the
	// template the capturing chatter so GetOrCreateWired copies it. The
	// chatter gets the loop reference after resolveAgent creates the
	// session loop (set by the pins via wireChatterLoop below).
	if template, terr := registry.Get("coder"); terr == nil && template != nil {
		template.llm = chatter
	}

	d := NewDispatcher(DispatcherConfig{
		Registry:     registry,
		TaskStore:    reg.Store(),
		TaskRegistry: reg,
		Logger:       logger,
	})
	d.SetThreadRouter(router)
	d.SetAgentLoopManager(NewManager(ManagerConfig{}))
	d.sessionStore = &stubSessionStore{
		sessions: map[string]*session.Session{
			sessionID: {ID: sessionID, ConversationID: sessionConvID, ProjectPath: "/tmp/digest-thread-project"},
		},
	}
	return d, chatter, reg
}

// wireChatterLoop pre-creates the session-scoped loop (the same call
// resolveAgent makes) and hands the chatter its state-machine reference, so
// successive scripted calls keep the loop's state machine legal.
func wireChatterLoop(t *testing.T, d *Dispatcher, chatter *promptCapturingChatter, sessionID string) {
	t.Helper()
	template, err := d.registry.Get("coder")
	if err != nil || template == nil {
		t.Fatalf("registry template: %v", err)
	}
	loop, err := d.loopManager.GetOrCreateWired(sessionID, "/tmp/digest-thread-project", template)
	if err != nil || loop == nil {
		t.Fatalf("pre-create session loop: %v", err)
	}
	chatter.loop = loop
}

// TestRouteToAgent_DigestBlockKeysOnSessionLevelID (M1, buildContextMessage
// arm): a thread-routed turn's executing-agent prompt must carry the
// session digest block. Before the fix the digest was queried with the
// thread-scoped id, whose session_tasks JOIN is empty — the block vanished
// for exactly the threaded conversations the router serves.
func TestRouteToAgent_DigestBlockKeysOnSessionLevelID(t *testing.T) {
	d, chatter, reg := newDigestThreadRouterDispatcher(t)

	const sessionID = "sess-digest-thread"
	// The PRIOR completed task linked with the SESSION-level id — the same
	// shape every production createTask produces post-90eb0f45.
	seedDigestTask(t, d, "task-digest-prior", "create hello.txt", sessionID,
		completedStateForDigest(), timeNowForDigest(), "coder")
	seedDigestStep(t, reg, "task-digest-prior", 0, stepApprovedForDigest(),
		"/tmp/digest-thread-project/hello.txt contains hello")
	wireChatterLoop(t, d, chatter, sessionID)

	result := &DispatchResult{
		AgentID:       "coder",
		Intent:        &Intent{Type: string(IntentChat), Summary: "summarize the repo"},
		OriginalInput: "summarize the repo",
	}

	// Drive the real route: the thread router resolves a thread-scoped id,
	// and (pre-fix) the digest block queried that id.
	if _, err := d.RouteToAgent(context.Background(), result, sessionID); err != nil {
		t.Fatalf("RouteToAgent: %v", err)
	}
	if len(chatter.prompts) == 0 {
		t.Fatal("no LLM call captured")
	}
	prompt := chatter.prompts[0]

	// The digest block is keyed off the session-level id: it must contain
	// the prior task's evidence regardless of thread routing.
	if !strings.Contains(prompt, "## Session context") {
		t.Fatalf("executing-agent prompt lost the session-context block under thread routing; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "hello.txt contains hello") {
		t.Fatalf("session-context block missing the prior task result; got:\n%s", prompt)
	}
}

// TestRouteToAgent_GuardFallbackDigestKeysOnSessionLevelID (M1,
// SetGuardFallback arm): the digest-armed reply-guard fallback must arm on
// a thread-routed turn. Before the fix the digest query used the
// thread-scoped id and armed nothing, so a machine-shaped reply shipped the
// generic canned line instead of the session's answer.
func TestRouteToAgent_GuardFallbackDigestKeysOnSessionLevelID(t *testing.T) {
	d, chatter, reg := newDigestThreadRouterDispatcher(t)

	const sessionID = "sess-digest-thread"
	seedDigestTask(t, d, "task-guard-prior", "create hello.txt", sessionID,
		completedStateForDigest(), timeNowForDigest(), "coder")
	seedDigestStep(t, reg, "task-guard-prior", 0, stepApprovedForDigest(),
		"The file hello.txt has been created.")
	wireChatterLoop(t, d, chatter, sessionID)

	result := &DispatchResult{
		AgentID:       "coder",
		Intent:        &Intent{Type: string(IntentChat), Summary: "status check"},
		OriginalInput: "status check",
	}

	// The chatter replies with a machine-shaped dump on every call, so the
	// loop's reply guard trips: first trip earns the rewrite retry, second
	// ships the fallback. With the digest armed (session-level key) the
	// fallback is the digest answer, not the canned line.
	reply, err := d.RouteToAgent(context.Background(), result, sessionID)
	if err != nil {
		t.Fatalf("RouteToAgent: %v", err)
	}
	if chatter.callCount < 2 {
		t.Fatalf("LLM calls = %d, want >=2 (the guard must trip and exhaust the retry to reach the fallback)", chatter.callCount)
	}
	if !strings.Contains(reply, "create hello.txt") {
		t.Fatalf("reply = %q, want the digest-armed fallback naming the prior task", reply)
	}
	if strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, the generic canned line shipped although a digest fallback should have been armed", reply)
	}
}
