package tierpolicy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/queue"
)

// stubDeleter records DeleteByIDs calls.
type stubDeleter struct {
	deleted [][]string
	err     error
}

func (d *stubDeleter) DeleteByIDs(_ context.Context, ids []string) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	d.deleted = append(d.deleted, ids)
	return len(ids), nil
}

// stubEnqueuer records Enqueue calls.
type stubEnqueuer struct {
	jobs []*queue.Job
	err  error
}

func (q *stubEnqueuer) Enqueue(_ context.Context, job *queue.Job) error {
	if q.err != nil {
		return q.err
	}
	q.jobs = append(q.jobs, job)
	return nil
}

// TestExecuteAppliesEvictAndCompress: real Deps mutations for both
// kinds; compression goes through the REAL memory.MemoryJobL1Extract
// job type.
func TestExecuteAppliesEvictAndCompress(t *testing.T) {
	t.Parallel()
	del := &stubDeleter{}
	enq := &stubEnqueuer{}
	e := New(enabledCfg(), Deps{Deletes: del, Enqueuer: enq})
	actions := []Action{
		{Kind: KindEvict, TargetIDs: []string{"a", "b"}, Reason: "tier=working;tokens=95;ceiling=100;target_tokens=5"},
		{Kind: KindCompress, TargetIDs: []string{"c", "d", "e", "f", "g"}, Reason: "tier=working;tokens=85;ceiling=100;target_tokens=5"},
	}
	rep, err := e.Execute(context.Background(), actions)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.ActionsApplied != 2 {
		t.Fatalf("want 2 applied, got %d", rep.ActionsApplied)
	}
	if rep.EvictionsDeleted != 2 {
		t.Fatalf("want 2 deletions, got %d", rep.EvictionsDeleted)
	}
	if rep.CompressionEnqueued != 5 {
		t.Fatalf("want 5 enqueued, got %d", rep.CompressionEnqueued)
	}
	if len(del.deleted) != 1 || len(del.deleted[0]) != 2 {
		t.Fatalf("want one 2-id delete batch, got %v", del.deleted)
	}
	if len(enq.jobs) != 1 {
		t.Fatalf("want 1 queue job, got %d", len(enq.jobs))
	}
	job := enq.jobs[0]
	if job.Type != memory.MemoryJobL1Extract {
		t.Fatalf("compression must reuse memory.MemoryJobL1Extract, got %q", job.Type)
	}
	var payload memory.L1ExtractPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("payload must be memory.L1ExtractPayload JSON: %v", err)
	}
	if !strings.HasPrefix(payload.SessionID, tierJobPrefix+":working") {
		t.Fatalf("job should carry the tier scope, got %q", payload.SessionID)
	}
}

// TestExecuteDryRun: dry-run reports counts but zero Deps mutations
// (leaf contract Given/When/Then: 2 evict + 1 compress -> report counts
// 3, zero mutations).
func TestExecuteDryRun(t *testing.T) {
	t.Parallel()
	del := &stubDeleter{}
	enq := &stubEnqueuer{}
	cfg := enabledCfg()
	cfg.DryRun = true
	e := New(cfg, Deps{Deletes: del, Enqueuer: enq})
	actions := []Action{
		{Kind: KindEvict, TargetIDs: []string{"a", "b"}, Reason: "tier=working"},
		{Kind: KindCompress, TargetIDs: []string{"c", "d", "e"}, Reason: "tier=working"},
	}
	rep, err := e.Execute(context.Background(), actions)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.ActionsApplied != 2 || rep.EvictRequested != 2 || rep.CompressRequested != 3 {
		t.Fatalf("dry-run must report would-be counts, got %+v", rep)
	}
	if rep.EvictionsDeleted != 0 || rep.CompressionEnqueued != 0 {
		t.Fatalf("dry-run must not count applied work, got %+v", rep)
	}
	if len(del.deleted) != 0 || len(enq.jobs) != 0 {
		t.Fatalf("dry-run must make zero Deps mutations, got deletes=%v jobs=%v", del.deleted, enq.jobs)
	}
	if !rep.DryRun {
		t.Fatal("report must mirror the dry-run posture")
	}
}

// TestExecutePerActionErrors: failures are recorded per action and never
// abort the batch.
func TestExecutePerActionErrors(t *testing.T) {
	t.Parallel()
	del := &stubDeleter{}
	enq := &stubEnqueuer{err: context.DeadlineExceeded}
	e := New(enabledCfg(), Deps{Deletes: del, Enqueuer: enq})
	actions := []Action{
		{Kind: KindCompress, TargetIDs: []string{"a", "b", "c", "d", "e"}, Reason: "tier=working"},
		{Kind: KindEvict, TargetIDs: []string{"x"}, Reason: "tier=working"},
		{Kind: "bogus", TargetIDs: []string{"y"}, Reason: "tier=working"},
	}
	rep, err := e.Execute(context.Background(), actions)
	if err == nil {
		t.Fatal("want batch-level error from the failing compress + bogus kind")
	}
	if len(rep.Errors) != 2 {
		t.Fatalf("want 2 recorded errors, got %v", rep.Errors)
	}
	// The evict after the failed compress still ran.
	if rep.EvictionsDeleted != 1 {
		t.Fatalf("batch must continue past per-action failure, got %+v", rep)
	}
}

// TestExecuteDisabled: a disabled engine executes nothing.
func TestExecuteDisabled(t *testing.T) {
	t.Parallel()
	del := &stubDeleter{}
	e := New(Config{}, Deps{Deletes: del})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindEvict, TargetIDs: []string{"a"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.ActionsApplied != 0 || len(del.deleted) != 0 {
		t.Fatalf("disabled engine must be inert, got %+v", rep)
	}
}

// TestEnqueueNeverBlocks: the enqueuer is called synchronously but the
// job is fire-and-forget (queue semantics) - assert no panic and a fast
// return shape with a slow enqueuer.
func TestEnqueueNeverBlocks(t *testing.T) {
	t.Parallel()
	e := New(enabledCfg(), Deps{Enqueuer: slowEnqueuer{delay: 10 * time.Millisecond}})
	start := time.Now()
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindCompress, TargetIDs: []string{"a", "b", "c", "d", "e"}, Reason: "tier=working"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.CompressionEnqueued != 5 {
		t.Fatalf("want 5 enqueued, got %+v", rep)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("enqueue must never block on LLM work, took %s", elapsed)
	}
}

type slowEnqueuer struct{ delay time.Duration }

func (s slowEnqueuer) Enqueue(_ context.Context, _ *queue.Job) error {
	time.Sleep(s.delay)
	return nil
}
