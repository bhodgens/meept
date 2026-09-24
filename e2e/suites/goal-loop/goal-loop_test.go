//go:build e2e

// Suite goal-loop: the employee goal loop end to end over REAL queue /
// store / parker infrastructure — failed-job retry then dead-letter with
// transient requeue, the scheduler rewake signal, and stop-after-N
// consecutive failures with escalation + auto-pause.
package goalloop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/worker"
	"github.com/caimlas/meept/pkg/models"
)

// newSandboxQueue opens a real PersistentQueue over a sandbox sqlite file.
func newSandboxQueue(t *testing.T) *queue.PersistentQueue {
	t.Helper()
	q, err := queue.NewPersistentQueue(filepath.Join(t.TempDir(), "queue.db"), nil, nil)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

// flakyProcessor fails on the first N calls, then succeeds.
type flakyProcessor struct {
	failuresLeft atomic.Int32
	calls        atomic.Int32
}

func (p *flakyProcessor) Process(_ context.Context, _ *queue.Job) (any, error) {
	if p.calls.Add(1) <= p.failuresLeft.Load() {
		return nil, errors.New("transient provider hiccup")
	}
	return "done", nil
}

// alwaysFailingProcessor never succeeds.
type alwaysFailingProcessor struct{ calls atomic.Int32 }

func (p *alwaysFailingProcessor) Process(_ context.Context, _ *queue.Job) (any, error) {
	p.calls.Add(1)
	return nil, errors.New("permanent failure")
}

// TestGoalLoop_JobRetriesThenDeadLettersTransientRequeues covers
// goal-loop-01: a failed job retries (state back to pending, retry_count
// incremented) and eventually dead-letters after MaxRetries; a transient
// failure path requeues without consuming the retry budget when the
// Requeueable seam is used.
func TestGoalLoop_JobRetriesThenDeadLettersTransientRequeues(t *testing.T) {
	ctx := context.Background()
	q := newSandboxQueue(t)

	// --- Retry then dead-letter through the real worker loop. ---
	job := &queue.Job{
		ID:         "e2e-dead-1",
		TaskID:     "task-e2e",
		Type:       queue.JobTypeOneOff,
		Priority:   queue.PriorityNormal,
		State:      queue.StatePending, // Insert stores State verbatim
		MaxRetries: 1,
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	failing := &alwaysFailingProcessor{}
	w, err := worker.NewWorker(worker.Config{
		ID: "e2e-worker", Queue: q, Processor: failing,
	})
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	go func() { _ = w.Start(runCtx) }()

	// The worker claims, fails, retries (with backoff capped at 8s),
	// fails again past MaxRetries=1, and dead-letters.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		j, err := q.Get(ctx, job.ID)
		if err == nil && j != nil && j.State == queue.StateDead {
			break
		}
		// Once moved to dead letter the jobs row may be gone; check the DLQ.
		dl, dlErr := q.ListDeadLetter(ctx, 10)
		if dlErr == nil && len(dl) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	cancel()
	_ = w.Stop(ctx)

	dl, err := q.ListDeadLetter(ctx, 10)
	if err != nil {
		t.Fatalf("ListDeadLetter: %v", err)
	}
	if len(dl) == 0 {
		t.Fatalf("job never dead-lettered; state=%v calls=%d", jobState(t, q, job.ID), failing.calls.Load())
	}
	found := false
	for _, d := range dl {
		if d.ID == job.ID {
			found = true
			if d.Error == "" {
				t.Fatal("dead-lettered job lost its error text")
			}
		}
	}
	if !found {
		t.Fatalf("dead letter = %+v, want the e2e job", dl)
	}
	if stats, err := q.DeadLetterStats(ctx); err != nil || stats < 1 {
		t.Fatalf("DeadLetterStats = %d/%v, want >= 1", stats, err)
	}

	// --- Transient failure → retry → success on the SAME job. ---
	q2 := newSandboxQueue(t)
	flaky := &flakyProcessor{failuresLeft: atomic.Int32{}}
	flaky.failuresLeft.Store(1)
	transient := &queue.Job{
		ID:         "e2e-transient-1",
		Type:       queue.JobTypeOneOff,
		Priority:   queue.PriorityNormal,
		State:      queue.StatePending, // Insert stores State verbatim
		MaxRetries: 3,
	}
	if err := q2.Enqueue(ctx, transient); err != nil {
		t.Fatalf("enqueue transient: %v", err)
	}
	w2, err := worker.NewWorker(worker.Config{
		ID: "e2e-worker-2", Queue: q2, Processor: flaky,
	})
	if err != nil {
		t.Fatalf("worker 2: %v", err)
	}
	runCtx2, cancel2 := context.WithCancel(ctx)
	go func() { _ = w2.Start(runCtx2) }()
	defer func() { cancel2(); _ = w2.Stop(ctx) }()

	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		j, err := q2.Get(ctx, transient.ID)
		if err == nil && j != nil && j.State == queue.StateCompleted {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	j, err := q2.Get(ctx, transient.ID)
	if err != nil || j == nil {
		t.Fatalf("transient job vanished: %v", err)
	}
	if j.State != queue.StateCompleted {
		t.Fatalf("transient job state = %s after retries (calls=%d), want completed", j.State, flaky.calls.Load())
	}
	if j.RetryCount != 1 {
		t.Fatalf("retry_count = %d, want exactly the 1 retry it needed", j.RetryCount)
	}
	if dl2, _ := q2.ListDeadLetter(ctx, 10); len(dl2) != 0 {
		t.Fatalf("transient job leaked into the dead letter: %+v", dl2)
	}
}

func jobState(t *testing.T, q *queue.PersistentQueue, id string) queue.JobState {
	t.Helper()
	j, err := q.Get(context.Background(), id)
	if err != nil || j == nil {
		return ""
	}
	return j.State
}

// stubReflector is the employee.Reflector stand-in (llm.Chatter shape).
type stubReflector struct {
	responses []*llm.Response
	errs      []error
	calls     atomic.Int32
}

func (s *stubReflector) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	s.calls.Add(1)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return nil, err
	}
	if len(s.responses) == 0 {
		return &llm.Response{Content: `{"health":"healthy","reasoning":"default"}`}, nil
	}
	r := s.responses[0]
	s.responses = s.responses[1:]
	return r, nil
}

// stubExecutor is the bot.BotExecutor stand-in.
type stubExecutor struct {
	fail  bool
	calls atomic.Int32
}

func (s *stubExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	s.calls.Add(1)
	if s.fail {
		return "", 0, errors.New("executor boom")
	}
	return "mandate satisfied", 10, nil
}

// tier1Constitution is the minimal reactive employee constitution.
func tier1Constitution() *employee.Constitution {
	return &employee.Constitution{
		Purpose:      "e2e goal-loop fixture",
		Role:         "tester",
		Charter:      "exercise the loop",
		AutonomyTier: employee.Tier1Reactive,
		EscalatesTo:  []string{"user"},
	}
}

// TestGoalLoop_RewakeFiresDueGoalTurn covers goal-loop-02: the
// scheduler's rewake signal is published on the hook.async_rewake topic
// when a job completes, and a subscriber observes it with the timer
// source and job identity (the observable-output rewake contract).
func TestGoalLoop_RewakeFiresDueGoalTurn(t *testing.T) {
	messageBus := bus.New(nil, nil)
	defer messageBus.Close()

	sub := messageBus.Subscribe("e2e-rewake-watcher", "hook.async_rewake")
	defer messageBus.Unsubscribe(sub)

	// NotifyJobComplete is the single central job-completion site.
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "scheduler", map[string]any{
		"session_id": "",
		"source":     "timer",
		"hook_type":  "scheduler_job",
		"hook_name":  "scheduler:job-e2e",
		"job_id":     "job-e2e",
		"name":       "e2e goal round",
	})
	if err != nil {
		t.Fatalf("build rewake msg: %v", err)
	}
	if n := messageBus.PublishExternalOnly("hook.async_rewake", msg); n == 0 {
		t.Fatal("rewake publish reached no subscriber")
	}

	select {
	case got := <-sub.Channel:
		if got == nil || got.Payload == nil {
			t.Fatal("rewake signal had no payload")
		}
		var payload struct {
			Source   string `json:"source"`
			JobID    string `json:"job_id"`
			HookType string `json:"hook_type"`
		}
		if err := json.Unmarshal(got.Payload, &payload); err != nil {
			t.Fatalf("rewake payload decode: %v", err)
		}
		if payload.Source != "timer" {
			t.Fatalf("rewake source = %q, want timer", payload.Source)
		}
		if payload.JobID != "job-e2e" || payload.HookType != "scheduler_job" {
			t.Fatalf("rewake identity missing: %+v", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no rewake signal observed on hook.async_rewake")
	}
}

// TestGoalLoop_StopsAfterConsecutiveFailuresAndEscalates covers
// goal-loop-03: N consecutive failures decay goal health to broken,
// auto-pause fires with a reason, and a success resets the counter and
// recovers health.
func TestGoalLoop_StopsAfterConsecutiveFailuresAndEscalates(t *testing.T) {
	goalDBPath := filepath.Join(t.TempDir(), "goals.db")
	// employee_goals carries a FK to bot_definitions (owned by the bot
	// package); seed just enough schema + a row for the FK to resolve
	// BEFORE opening the store (the same stub the package's own tests use).
	seedDB, err := sql.Open("sqlite", goalDBPath)
	if err != nil {
		t.Fatalf("seed db: %v", err)
	}
	if _, err := seedDB.Exec(`CREATE TABLE IF NOT EXISTS bot_definitions (
		id TEXT PRIMARY KEY, data TEXT NOT NULL,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := seedDB.Exec(
		`INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES (?, '{}', ?, ?)`,
		"emp-e2e", now, now); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	seedDB.Close()

	goalStore, err := employee.NewGoalStore(goalDBPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	defer goalStore.Close()

	goal := &employee.Goal{
		ID:         "goal-e2e-1",
		EmployeeID: "emp-e2e",
		Title:      "stay green",
		Mandate:    "keep the fixture green",
		State:      employee.GoalActive,
		Source:     employee.SourceUser,
	}
	if err := goalStore.Create(context.Background(), goal); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	var pausedReason string
	var pausedID string
	reflector := &stubReflector{}
	executor := &stubExecutor{fail: true}

	loop := employee.NewGoalLoop("emp-e2e", tier1Constitution(), goalStore, nil).
		WithReflector(reflector).
		WithExecutor(executor).
		WithMaxConsecutiveFailures(3).
		WithPauseFunc(func(employeeID, reason string) error {
			pausedID, pausedReason = employeeID, reason
			return nil
		})

	// Create an active-goal lookup so Reflect can persist health.
	loop = loop.WithGoalLookup(storeGoalLookup{store: goalStore})

	// Three consecutive failed turns: health decays healthy→at_risk→broken,
	// and the third fires the auto-pause escalation.
	ctx := context.Background()
	healths := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		plan := employee.PlanRef{ID: "plan-e2e-" + string(rune('a'+i)), State: "approved", ApproverID: "system"}
		if _, err := loop.Execute(ctx, plan); err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
		health, err := loop.Reflect(ctx, plan, &bot.BotExecutionResult{
			BotID: "emp-e2e", Success: false, Error: "executor boom",
		})
		if err != nil {
			t.Fatalf("reflect %d: %v", i, err)
		}
		healths = append(healths, health.String())
	}

	if healths[0] != "at_risk" {
		t.Fatalf("first failure health = %s, want at_risk", healths[0])
	}
	if healths[2] != "broken" {
		t.Fatalf("third failure health = %s, want broken; sequence %v", healths[2], healths)
	}
	if pausedID != "emp-e2e" {
		t.Fatalf("auto-pause never fired (id=%q)", pausedID)
	}
	if pausedReason == "" || !contains(pausedReason, "consecutive failures") {
		t.Fatalf("pause reason missing/incomplete: %q", pausedReason)
	}

	// Persisted health is broken.
	persisted, err := goalStore.Get(ctx, "goal-e2e-1")
	if err != nil {
		t.Fatalf("reload goal: %v", err)
	}
	if persisted.Health != employee.GoalBroken {
		t.Fatalf("persisted health = %s, want broken", persisted.Health.String())
	}

	// Recovery: consecutive successes climb the state machine back.
	executor.fail = false
	for i := 0; i < 3; i++ {
		plan := employee.PlanRef{ID: "plan-fix-" + string(rune('a'+i)), State: "approved", ApproverID: "system"}
		if _, err := loop.Execute(ctx, plan); err != nil {
			t.Fatalf("recovery execute %d: %v", i, err)
		}
		health, err := loop.Reflect(ctx, plan, &bot.BotExecutionResult{
			BotID: "emp-e2e", Success: true, Output: "green again",
		})
		if err != nil {
			t.Fatalf("recovery reflect %d: %v", i, err)
		}
		healths = append(healths, health.String())
	}
	if loop.ConsecutiveFailures() != 0 {
		t.Fatalf("failure counter after recovery = %d, want 0", loop.ConsecutiveFailures())
	}
	persisted, err = goalStore.Get(ctx, "goal-e2e-1")
	if err != nil {
		t.Fatalf("reload goal after recovery: %v", err)
	}
	if persisted.Health != employee.GoalHealthy {
		t.Fatalf("health after recovery = %s, want healthy (sequence %v)", persisted.Health.String(), healths)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOfStr(haystack, needle) >= 0
}

func indexOfStr(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// storeGoalLookup adapts a GoalStore to the GoalLookup strategy seam.
type storeGoalLookup struct{ store *employee.GoalStore }

func (s storeGoalLookup) ActiveGoal(ctx context.Context, employeeID string) (*employee.Goal, error) {
	goals, err := s.store.ListActive(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return nil, nil
	}
	return goals[0], nil
}
