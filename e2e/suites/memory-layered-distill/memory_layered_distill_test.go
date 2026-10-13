//go:build e2e

// Suite memory-layered-distill: the L0 raw-conversation tier end-to-end
// (tree 01, plan forest 20261012-memory-capability-forest). Fixtures are
// written through the REAL l0.Recorder, distilled by a DistillWorker over a
// fake-chatter LLM and a real PersistentQueue — the same shape the daemon
// wiring (internal/daemon/l0_wiring.go) assembles:
//
//   - seeded conversation → L0 lines exist
//   - L1 job runs → auto-status claims land
//   - chained L2 job runs → a scene file exists
//   - the whole pipeline leaves zero dead-letters in queue stats
//   - a duplicate L1 enqueue is suppressed by the marker (no second claim
//     set, no second LLM call)
package memorylayereddistill

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/l0"
	"github.com/caimlas/meept/internal/queue"
)

// fakeChatter scripts memory.DistillLLM responses per prompt substring and
// records every prompt (the fake-chatter pattern: deterministic, no live
// model).
type fakeChatter struct {
	extraction string // returned for extraction-shaped prompts
	segment    string // returned for segmentation-shaped prompts
	err        error
	prompts    []string
}

func (f *fakeChatter) DistillComplete(_ context.Context, prompt string) ([]byte, error) {
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return nil, f.err
	}
	if strings.Contains(prompt, "scene") || strings.Contains(prompt, "segmenter") {
		return []byte(f.segment), nil
	}
	return []byte(f.extraction), nil
}

// newTestManager builds an initialized sqlite-backed memory manager over a
// temp dir (the same construction the daemon wiring uses, minus bus/LLM).
func newTestManager(t *testing.T, dataDir string) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  dataDir,
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init memory manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// newTestQueue opens a real PersistentQueue over a temp dir and closes it
// at cleanup.
func newTestQueue(t *testing.T) *queue.PersistentQueue {
	t.Helper()
	q, err := queue.NewPersistentQueue(filepath.Join(t.TempDir(), "queue.db"), nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("persistent queue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

// TestLayeredDistillPipeline covers the Integration Test Plan: seeded
// conversation → L0 lines exist, L1 job produces a status=auto claim, L2
// scene file exists, queue shows zero dead-letters.
func TestLayeredDistillPipeline(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mgr := newTestManager(t, dataDir)
	q := newTestQueue(t)

	// Seed the conversation through the REAL recorder (frozen §2.1 schema,
	// <memory-context> feedback-loop guard included).
	session := "sess-e2e-distill"
	recorder := l0.NewRecorder(dataDir, nil)
	if err := recorder.Append(session, "user", "i prefer sqlite over memvid for the memory backend", nil); err != nil {
		t.Fatalf("l0 append: %v", err)
	}
	if err := recorder.Append(session, "assistant", "noted, the memory backend stays sqlite from now on", nil); err != nil {
		t.Fatalf("l0 append: %v", err)
	}
	l0Path := recorder.DatePath(time.Now().Format("2006-01-02"))

	// L0 lines really exist on disk.
	recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), time.Now().Format("2006-01-02"), session)
	if err != nil {
		t.Fatalf("read l0: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("l0 lines = %d, want 2", len(recs))
	}

	chat := &fakeChatter{
		extraction: `[{"type":"claim","text":"user prefers sqlite over memvid","source":"conversation","confidence":0.8,"premises":[],"category":"technical"}]`,
		segment:    `[{"scene_name":"backend decision","message_ids":["` + recs[0].ID + `","` + recs[1].ID + `"]}]`,
	}
	worker := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager:  mgr,
		LLM:      chat,
		DataDir:  dataDir,
		Enqueuer: q,
		Logger:   slog.New(slog.DiscardHandler),
	})

	// Enqueue + process the L1 job (the daemon hook enqueues; the queue
	// worker claims — the e2e shortcut drives the same job through).
	l1ID, err := memory.EnqueueL1(ctx, q, memory.L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatalf("enqueue l1: %v", err)
	}
	job, err := q.Claim(ctx, "e2e-distill-worker", nil, "")
	if err != nil {
		t.Fatalf("claim l1: %v", err)
	}
	if job == nil || job.ID != l1ID {
		t.Fatalf("claimed job %+v, want l1 job %s", job, l1ID)
	}
	if _, err := worker.Process(ctx, job); err != nil {
		t.Fatalf("process l1: %v", err)
	}
	if err := q.Complete(ctx, job.ID, map[string]any{"status": "ok"}); err != nil {
		t.Fatalf("complete l1: %v", err)
	}

	// An L1 job ran and a status=auto claim landed.
	claims, err := mgr.Search(ctx, memory.MemoryQuery{Type: memory.MemoryTypeClaim, Query: "sqlite", Limit: 10})
	if err != nil {
		t.Fatalf("search claims: %v", err)
	}
	if len(claims) == 0 {
		t.Fatal("no auto claim stored by l1 extract")
	}
	for _, r := range claims {
		if status, _ := r.Memory.Metadata["status"].(string); status != string(memory.ClaimStatusAuto) {
			t.Fatalf("claim status = %q, want auto", status)
		}
	}

	// The chained L2 job (enqueued by the L1 success path) runs and writes
	// one scene file.
	l2Job, err := q.Claim(ctx, "e2e-distill-worker", nil, "")
	if err != nil {
		t.Fatalf("claim l2: %v", err)
	}
	if l2Job == nil || l2Job.Type != memory.MemoryJobL2Scene {
		t.Fatalf("chained job = %+v, want %s", l2Job, memory.MemoryJobL2Scene)
	}
	if _, err := worker.Process(ctx, l2Job); err != nil {
		t.Fatalf("process l2: %v", err)
	}
	if err := q.Complete(ctx, l2Job.ID, map[string]any{"status": "ok"}); err != nil {
		t.Fatalf("complete l2: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dataDir, "l2"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("l2 scene files missing: %v", err)
	}
	foundScene := false
	for _, e := range entries {
		b, rerr := os.ReadFile(filepath.Join(dataDir, "l2", e.Name()))
		if rerr != nil {
			t.Fatalf("read scene file: %v", rerr)
		}
		if strings.Contains(string(b), `"segment_method":"llm"`) && strings.Contains(string(b), session) {
			foundScene = true
		}
	}
	if !foundScene {
		t.Fatal("no llm-segmented scene file for the session")
	}

	// Queue stats: zero dead-letters after the whole pipeline.
	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}
	if stats.DeadCount != 0 {
		t.Fatalf("dead-letter count = %d, want 0", stats.DeadCount)
	}
}

// TestLayeredDistillDuplicateEnqueueSuppressed covers the idempotency
// requirement: a second L1 enqueue for an already-distilled window is
// suppressed by the marker — no second claim set, no second extraction LLM
// call (kill-mid-extract is covered by the queue's re-claim semantics; the
// marker test pins the resulting exactly-once claim behavior).
func TestLayeredDistillDuplicateEnqueueSuppressed(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mgr := newTestManager(t, dataDir)
	q := newTestQueue(t)

	session := "sess-e2e-dup"
	recorder := l0.NewRecorder(dataDir, nil)
	if err := recorder.Append(session, "user", "deployment gate pins tag v9", nil); err != nil {
		t.Fatalf("l0 append: %v", err)
	}
	l0Path := recorder.DatePath(time.Now().Format("2006-01-02"))

	chat := &fakeChatter{
		extraction: `[{"type":"claim","text":"deploy gate pins v9","source":"conversation","confidence":0.7,"premises":[],"category":"technical"}]`,
		segment:    `[]`,
	}
	worker := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager:  mgr,
		LLM:      chat,
		DataDir:  dataDir,
		Enqueuer: q,
		Logger:   slog.New(slog.DiscardHandler),
	})

	// First pass: extract once.
	if _, err := memory.EnqueueL1(ctx, q, memory.L1ExtractPayload{SessionID: session, L0Path: l0Path}); err != nil {
		t.Fatalf("enqueue l1 (1st): %v", err)
	}
	job, err := q.Claim(ctx, "e2e-dup-worker", nil, "")
	if err != nil || job == nil {
		t.Fatalf("claim l1 (1st): %v", err)
	}
	if _, err := worker.Process(ctx, job); err != nil {
		t.Fatalf("process l1 (1st): %v", err)
	}
	if err := q.Complete(ctx, job.ID, nil); err != nil {
		t.Fatalf("complete l1 (1st): %v", err)
	}
	if got := len(chat.prompts); got != 1 {
		t.Fatalf("llm calls after first pass = %d, want 1", got)
	}

	// Drain the chained L2 job from the first pass before the duplicate
	// enqueue, so the next claim returns the dup L1 (claim order is FIFO
	// by created_at). The L2 scene-segmentation prompt also counts as a
	// fakeChatter call, so snapshot the call count first and assert the
	// EXTRACTION count is unchanged.
	l2Job, err := q.Claim(ctx, "e2e-dup-worker", nil, "")
	if err != nil || l2Job == nil {
		t.Fatalf("claim chained l2: %v", err)
	}
	if l2Job.Type != memory.MemoryJobL2Scene {
		t.Fatalf("chained job type = %s, want %s", l2Job.Type, memory.MemoryJobL2Scene)
	}
	if _, err := worker.Process(ctx, l2Job); err != nil {
		t.Fatalf("process chained l2: %v", err)
	}
	if err := q.Complete(ctx, l2Job.ID, nil); err != nil {
		t.Fatalf("complete chained l2: %v", err)
	}
	promptsAfterL2 := len(chat.prompts)

	// Second (duplicate) enqueue of the SAME window: the marker check
	// suppresses — no new claim, no second extraction call. The empty
	// SinceMsgID payload resolves to "today's undistilled tail", and the
	// marker already sits on the day's last record, so the suppression
	// check short-circuits with status "suppressed".
	if _, err := memory.EnqueueL1(ctx, q, memory.L1ExtractPayload{SessionID: session, L0Path: l0Path}); err != nil {
		t.Fatalf("enqueue l1 (dup): %v", err)
	}
	dupJob, err := q.Claim(ctx, "e2e-dup-worker", nil, "")
	if err != nil || dupJob == nil {
		t.Fatalf("claim l1 (dup): %v", err)
	}
	res, err := worker.Process(ctx, dupJob)
	if err != nil {
		t.Fatalf("process l1 (dup): %v", err)
	}
	if m, _ := res.(map[string]any); m == nil || m["status"] != "suppressed" {
		t.Fatalf("duplicate l1 result = %v, want status=suppressed", res)
	}
	if err := q.Complete(ctx, dupJob.ID, res); err != nil {
		t.Fatalf("complete l1 (dup): %v", err)
	}

	claims, err := mgr.Search(ctx, memory.MemoryQuery{Type: memory.MemoryTypeClaim, Query: "v9", Limit: 10})
	if err != nil {
		t.Fatalf("search claims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("claims after duplicate enqueue = %d, want 1 (exactly-once)", len(claims))
	}
	if got := len(chat.prompts); got != promptsAfterL2 {
		t.Fatalf("llm calls after duplicate = %d, want %d (extraction suppressed)", got, promptsAfterL2)
	}

	// Still zero dead-letters.
	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}
	if stats.DeadCount != 0 {
		t.Fatalf("dead-letter count = %d, want 0", stats.DeadCount)
	}
}

// TestLayeredDistillLLMErrorNeverAdvancesMarker pins the failure contract:
// an LLM failure surfaces as a job error (the queue retries / dead-letters
// per its own policy) and the marker never advances, so a retry re-reads
// the same window.
func TestLayeredDistillLLMErrorNeverAdvancesMarker(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mgr := newTestManager(t, dataDir)

	session := "sess-e2e-err"
	recorder := l0.NewRecorder(dataDir, nil)
	if err := recorder.Append(session, "user", "quota check line", nil); err != nil {
		t.Fatalf("l0 append: %v", err)
	}
	l0Path := recorder.DatePath(time.Now().Format("2006-01-02"))

	chat := &fakeChatter{err: errors.New("provider unavailable")}
	worker := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: mgr,
		LLM:     chat,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	payload, err := json.Marshal(memory.L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Process(ctx, &queue.Job{Type: memory.MemoryJobL1Extract, Payload: payload}); err == nil {
		t.Fatal("expected llm error to propagate to the queue layer")
	}

	// The window is still undistilled: a retry with a healthy LLM stores
	// the claim (proving the marker did not advance).
	claims, err := mgr.Search(ctx, memory.MemoryQuery{Type: memory.MemoryTypeClaim, Query: "quota", Limit: 10})
	if err != nil {
		t.Fatalf("search claims: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("claims stored despite llm error = %d, want 0", len(claims))
	}

	chat.err = nil
	chat.extraction = `[{"type":"claim","text":"quota retry worked","source":"conversation","confidence":0.7,"premises":[],"category":"technical"}]`
	if _, err := worker.Process(ctx, &queue.Job{Type: memory.MemoryJobL1Extract, Payload: payload}); err != nil {
		t.Fatalf("retry l1: %v", err)
	}
	claims, err = mgr.Search(ctx, memory.MemoryQuery{Type: memory.MemoryTypeClaim, Query: "quota retry", Limit: 10})
	if err != nil {
		t.Fatalf("search claims after retry: %v", err)
	}
	if len(claims) == 0 {
		t.Fatal("retry after llm error stored no claim (window lost)")
	}
}
