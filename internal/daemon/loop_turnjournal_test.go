package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/agent/turnjournal"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
)

// journalingStore records appended rows + closes; CloseRun also appends
// a KindRunClosed row so kinds() sees the full sequence. CompletedSteps
// replays completed rows per turn.
type journalingStore struct {
	mu    sync.Mutex
	rows  []turnjournal.Record
	times []time.Time
}

func (s *journalingStore) Append(ctx context.Context, rec turnjournal.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	s.rows = append(s.rows, rec)
	return nil
}

func (s *journalingStore) OpenRuns(ctx context.Context) ([]turnjournal.OpenRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs []turnjournal.OpenRun
	closed := map[string]bool{}
	for _, r := range s.rows {
		if r.Kind == turnjournal.KindRunClosed {
			closed[r.TurnID] = true
		}
	}
	for _, r := range s.rows {
		if r.Kind == turnjournal.KindRunStarted && !closed[r.TurnID] {
			runs = append(runs, turnjournal.OpenRun{TurnID: r.TurnID, SessionID: r.SessionID, TurnPayload: r.ResultPayload})
		}
	}
	return runs, nil
}

func (s *journalingStore) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	steps := make(map[string][]byte)
	for _, r := range s.rows {
		if r.TurnID == turnID && r.Kind == turnjournal.KindStepCompleted {
			steps[r.StepID] = r.ResultPayload
		}
	}
	return steps, nil
}

func (s *journalingStore) CloseRun(ctx context.Context, turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, turnjournal.Record{TurnID: turnID, Kind: turnjournal.KindRunClosed, Timestamp: time.Now()})
	return nil
}

func (s *journalingStore) PruneClosed(ctx context.Context, olderThan time.Duration) error {
	return nil
}

func (s *journalingStore) Close() error { return nil }

func (s *journalingStore) kinds() []turnjournal.RecordKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]turnjournal.RecordKind, len(s.rows))
	for i, r := range s.rows {
		out[i] = r.Kind
	}
	return out
}

// okChatter always answers with its content.
type okChatter struct{ content string }

func (c *okChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: c.content}, nil
}
func (c *okChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}
func (c *okChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{ModelID: "ok"} }

// quotaAlwaysChatter always fails with a QuotaResetError - the terminal
// quota shape (no rotation candidates available).
type quotaAlwaysChatter struct{ err *llm.QuotaResetError }

func (c *quotaAlwaysChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return nil, c.err
}
func (c *quotaAlwaysChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}
func (c *quotaAlwaysChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{ModelID: "quota"} }

// journalingProcessor builds a processor with the turn journal wired
// over a fake store, using the existing discardLogger() helper.
func newJournalingProcessor(t *testing.T, loop *agent.AgentLoop) (*AgentJobProcessor, *journalingStore) {
	t.Helper()
	p, _, _ := newTestAgentJobProcessor(t)
	p.agentLoop = loop
	store := &journalingStore{}
	p.WithTurnJournal(agent.NewTurnJournalHooks(store, discardLogger()))
	return p, store
}

// newTurnJournalTestLoop mirrors newQuotaTestLoop with an arbitrary chatter.
func newTurnJournalTestLoop(t *testing.T, chatter llm.Chatter) *agent.AgentLoop {
	t.Helper()
	return newQuotaTestLoop(t, chatter)
}

// countingChatter wraps a chatter and counts Chat calls (the skip seam's
// "no LLM re-invocation" assertion).
type countingChatter struct {
	inner llm.Chatter
	calls int
}

func (c *countingChatter) Chat(ctx context.Context, msgs []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.calls++
	return c.inner.Chat(ctx, msgs, opts...)
}
func (c *countingChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	c.calls++
	return c.inner.ChatWithProgress(ctx, msgs, cb, opts...)
}
func (c *countingChatter) Config() *llm.ModelConfig { return c.inner.Config() }

func stepJob(t *testing.T, id, taskID string) *queue.Job {
	return &queue.Job{
		ID:      id,
		TaskID:  taskID,
		AgentID: "coder",
		Type:    queue.JobTypeProjectTask,
		Payload: stepJobPayload(t, id, taskID, "do the work"),
	}
}

// T2: a successful step-job turn journals the exact row sequence:
// run_started, step_started, step_completed, run_closed.
func TestLoopJournalSeamHappyPath(t *testing.T) {
	loop := newTurnJournalTestLoop(t, &okChatter{content: "did the work"})
	p, store := newJournalingProcessor(t, loop)

	if _, perr := p.Process(context.Background(), stepJob(t, "job-tj-1", "task-tj-1")); perr != nil {
		t.Fatalf("Process: %v", perr)
	}

	got := store.kinds()
	want := []turnjournal.RecordKind{
		turnjournal.KindRunStarted,
		turnjournal.KindStepStarted,
		turnjournal.KindStepCompleted,
		turnjournal.KindRunClosed,
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	// The step_completed payload is the serialized result envelope.
	store.mu.Lock()
	completed := store.rows[2]
	store.mu.Unlock()
	var envelope map[string]any
	if err := json.Unmarshal(completed.ResultPayload, &envelope); err != nil {
		t.Fatalf("step_completed payload not JSON: %v", err)
	}
	if envelope["step_id"] != "job-tj-1" {
		t.Errorf("envelope step_id = %v, want job-tj-1", envelope["step_id"])
	}
	if envelope["response"] != "did the work" {
		t.Errorf("envelope response = %v", envelope["response"])
	}
	// run_started carries the queued job's payload blob.
	store.mu.Lock()
	started := store.rows[0]
	store.mu.Unlock()
	if !strings.Contains(string(started.ResultPayload), "do the work") {
		t.Errorf("run_started payload = %q, want the job payload", started.ResultPayload)
	}
}

// T2: nil writer (flag off) journals nothing and the turn result is
// unchanged.
func TestLoopJournalSeamNilWriter(t *testing.T) {
	loop := newTurnJournalTestLoop(t, &okChatter{content: "legacy answer"})
	p, _, _ := newTestAgentJobProcessor(t)
	p.agentLoop = loop

	res, perr := p.Process(context.Background(), stepJob(t, "job-tj-nil", "task-tj-nil"))
	if perr != nil {
		t.Fatalf("Process: %v", perr)
	}
	m, ok := res.(map[string]any)
	if !ok || m["response"] != "legacy answer" {
		t.Fatalf("result = %#v, want the legacy envelope", res)
	}
}

// T3: a journaled completed step is RESTORED without LLM re-invocation.
func TestLoopJournalSeamSkipRestoresResult(t *testing.T) {
	chatter := &countingChatter{inner: &okChatter{content: "FRESH EXECUTION"}}
	loop := newTurnJournalTestLoop(t, chatter)
	p, store := newJournalingProcessor(t, loop)

	// Seed: the previous attempt completed the step and journaled it.
	ctx := context.Background()
	payload, _ := json.Marshal(map[string]any{
		"job_id": "job-tj-skip", "response": "JOURNALED ANSWER",
		"status": "completed", "success": true,
	})
	if err := store.Append(ctx, turnjournal.Record{TurnID: "task-tj-skip", SessionID: "c", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, turnjournal.Record{
		TurnID: "task-tj-skip", SessionID: "c", StepID: "job-tj-skip", StepIndex: 0,
		Kind: turnjournal.KindStepCompleted, ResultPayload: payload,
	}); err != nil {
		t.Fatal(err)
	}

	res, perr := p.Process(ctx, stepJob(t, "job-tj-skip", "task-tj-skip"))
	if perr != nil {
		t.Fatalf("Process: %v", perr)
	}
	envelope, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", res)
	}
	if envelope["response"] != "JOURNALED ANSWER" {
		t.Errorf("restored response = %v, want the journaled result (never re-executed)", envelope["response"])
	}
	if calls := chatter.calls; calls != 0 {
		t.Errorf("LLM calls = %d, want 0 (skip seam must not re-invoke)", calls)
	}
	// The restore path returns BEFORE the success-side run_closed hook
	// (the restored envelope short-circuits Process), so no NEW
	// step_started/step_completed rows appear for this attempt - the
	// seeded rows are the only journal content.
	kinds := store.kinds()
	if n := countKind(kinds, turnjournal.KindStepCompleted); n != 1 {
		t.Errorf("step_completed rows = %d, want 1 (the seeded row only)", n)
	}
	if n := countKind(kinds, turnjournal.KindStepStarted); n != 0 {
		t.Errorf("step_started rows = %d, want 0 (the step was skipped)", n)
	}
}

// T3: an unparseable journaled blob fails OPEN - the step re-executes.
func TestLoopJournalSeamSkipUnparseableFailsOpen(t *testing.T) {
	loop := newTurnJournalTestLoop(t, &okChatter{content: "re-executed"})
	p, store := newJournalingProcessor(t, loop)

	ctx := context.Background()
	if err := store.Append(ctx, turnjournal.Record{TurnID: "task-tj-bad", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, turnjournal.Record{
		TurnID: "task-tj-bad", StepID: "job-tj-bad", StepIndex: 0,
		Kind: turnjournal.KindStepCompleted, ResultPayload: []byte("not json at all"),
	}); err != nil {
		t.Fatal(err)
	}

	res, perr := p.Process(ctx, stepJob(t, "job-tj-bad", "task-tj-bad"))
	if perr != nil {
		t.Fatalf("Process: %v", perr)
	}
	if envelope, ok := res.(map[string]any); !ok || envelope["response"] != "re-executed" {
		t.Fatalf("result = %#v, want a fresh execution", res)
	}
}

// T4: a QuotaResetError mid-turn PARKS via the existing path (the
// sentinel error chain), and the journal leaves the run OPEN - no
// run_closed, the completed steps stay journaled, and no short-retry
// happened (the worker's requeueOnProviderWait owns the resume).
func TestLoopJournalQuotaPark(t *testing.T) {
	loop := newTurnJournalTestLoop(t, &quotaAlwaysChatter{err: &llm.QuotaResetError{
		ProviderID: "p1", ModelID: "m1", Code: "quota_exhausted",
		Message: "quota reset pending", RetryAfter: 30 * time.Minute,
	}})
	p, store := newJournalingProcessor(t, loop)

	_, perr := p.Process(context.Background(), stepJob(t, "job-tj-q", "task-tj-q"))
	if perr == nil {
		t.Fatal("Process = nil error, want the quota failure to propagate (the park/requeue shape)")
	}
	var qe *llm.QuotaResetError
	if !errors.As(perr, &qe) {
		t.Fatalf("error %v is not QuotaResetError-reachable; the job must PARK via the provider-wait path, never short-retry", perr)
	}

	kinds := store.kinds()
	// run_started + step_started recorded; NO run_closed (the run stays
	// open for leaf 03's rehydrate) and NO step_completed (nothing to
	// restore for a step that never finished).
	if countKind(kinds, turnjournal.KindRunClosed) != 0 {
		t.Errorf("run_closed rows = %d, want 0 (a parked run stays OPEN)", countKind(kinds, turnjournal.KindRunClosed))
	}
	if countKind(kinds, turnjournal.KindStepCompleted) != 0 {
		t.Errorf("step_completed rows = %d, want 0", countKind(kinds, turnjournal.KindStepCompleted))
	}
	if countKind(kinds, turnjournal.KindRunStarted) != 1 {
		t.Errorf("run_started rows = %d, want 1", countKind(kinds, turnjournal.KindRunStarted))
	}
	// No retry consumed: the quota error surfaces on the FIRST LLM call
	// and the loop's rotation never short-retries a quota block (the
	// chatter call count is pinned implicitly by the chatter always
	// failing; a short-retry would still be 1 call, so also assert the
	// returned error is the quota-sentence shape the park path renders).
	if !strings.Contains(perr.Error(), "quota wait:") {
		t.Errorf("error %q lacks the park-path quota sentence", perr.Error())
	}
}

// T4 regression pin: a turn that completed steps 0 then hit quota on a
// LATER resume keeps its journaled completed steps with the run open.
func TestLoopJournalQuotaParkKeepsCompletedSteps(t *testing.T) {
	loop := newTurnJournalTestLoop(t, &quotaAlwaysChatter{err: &llm.QuotaResetError{
		ProviderID: "p1", ModelID: "m1", RetryAfter: time.Hour,
	}})
	p, store := newJournalingProcessor(t, loop)

	ctx := context.Background()
	// Step A of the task completed in an earlier attempt.
	if err := store.Append(ctx, turnjournal.Record{TurnID: "task-tj-q2", Kind: turnjournal.KindRunStarted}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, turnjournal.Record{
		TurnID: "task-tj-q2", StepID: "step-a", StepIndex: 0,
		Kind: turnjournal.KindStepCompleted, ResultPayload: []byte(`{"response":"a done"}`),
	}); err != nil {
		t.Fatal(err)
	}

	if _, perr := p.Process(ctx, stepJob(t, "step-b", "task-tj-q2")); perr == nil {
		t.Fatal("expected quota failure for step-b")
	}

	kinds := store.kinds()
	if countKind(kinds, turnjournal.KindRunClosed) != 0 {
		t.Errorf("run_closed = %d, want 0", countKind(kinds, turnjournal.KindRunClosed))
	}
	steps, err := store.CompletedSteps(ctx, "task-tj-q2")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := steps["step-a"]; !ok {
		t.Errorf("journaled step-a lost after the park: %v", steps)
	}
	// step-b must NOT be journaled completed.
	if _, ok := steps["step-b"]; ok {
		t.Errorf("step-b journaled completed on a parked turn: %v", steps)
	}
}

func countKind(kinds []turnjournal.RecordKind, want turnjournal.RecordKind) int {
	n := 0
	for _, k := range kinds {
		if k == want {
			n++
		}
	}
	return n
}
