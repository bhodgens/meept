package memory

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
)

// TestKnowledgeDirIngestPassDormantByDefault: without SetKnowledgeDirIngest
// the consolidation pass must not call anything (nil closure = disabled,
// matching the bridge-pass dormant contract).
func TestKnowledgeDirIngestPassDormantByDefault(t *testing.T) {
	c := NewConsolidator(ConsolidatorConfig{Logger: slog.New(slog.DiscardHandler)})
	// No SetKnowledgeDirIngest call: must not panic on Run's pass. Run
	// needs a backend; call the pass directly through a Run-less path by
	// exercising the hook snapshot under the lock.
	c.runKnowledgeDirIngestPass(context.Background()) // must be a no-op
}

// TestSetKnowledgeDirIngestRoundTrip pins the wiring accessor and proves
// the consolidator tick invokes the closure (the daemon wires
// knowledge.SyncDir here in T04-L4).
func TestSetKnowledgeDirIngestRoundTrip(t *testing.T) {
	c := NewConsolidator(ConsolidatorConfig{Logger: slog.New(slog.DiscardHandler)})
	var calls atomic.Int32
	c.SetKnowledgeDirIngest(func(ctx context.Context) {
		calls.Add(1)
	})
	c.runKnowledgeDirIngestPass(context.Background())
	c.runKnowledgeDirIngestPass(context.Background())
	if calls.Load() != 2 {
		t.Errorf("closure called %d times, want 2", calls.Load())
	}

	// Detach: nil disables again.
	c.SetKnowledgeDirIngest(nil)
	c.runKnowledgeDirIngestPass(context.Background())
	if calls.Load() != 2 {
		t.Errorf("closure called %d times after detach, want 2", calls.Load())
	}
}

// TestKnowledgeDirIngestPassInRun proves the consolidation Run invokes the
// wired closure end-to-end (reuses the existing timer — no new ticker).
// The stub backend errors on consolidation work; the knowledge pass must
// still fire (best-effort, before/alongside failing passes).
func TestKnowledgeDirIngestPassInRun(t *testing.T) {
	c := NewConsolidator(ConsolidatorConfig{
		Backend: &stubConsolidationBackend{},
		Logger:  slog.New(slog.DiscardHandler),
	})
	var calls atomic.Int32
	c.SetKnowledgeDirIngest(func(ctx context.Context) { calls.Add(1) })

	// Run synchronously: episodic consolidation fails (stub), the knowledge
	// pass must still have been invoked by the time Run returns.
	_, _ = c.Run(context.Background(), 24)
	if calls.Load() != 1 {
		t.Fatalf("knowledge dir-ingest pass ran %d times inside Run, want 1", calls.Load())
	}
}
