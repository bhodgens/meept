package memory

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/queue"
)

// TestDistillL1SkipWarnNoModel pins the models-less degradation (memory
// second-brain leaf 02 T4): with default-on L0, a host with NO resolvable
// memory model must not error-loop the queue. The L1 job SKIPS with
// exactly ONE Warn per worker (status=skipped_no_model, nil error — the
// job completes so the queue never retries), the progress marker never
// advances, and the L0 lines survive on disk for a later model-driven
// re-distill. L2 shares the same Warn-once and degrades to the fallback
// whole-session scene.
func TestDistillL1SkipWarnNoModel(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	mgr := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  dir,
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		DataDir: dir,
		Logger:  slog.New(slog.NewTextHandler(&buf, nil)),
	})
	p := L1ExtractPayload{SessionID: "s1", L0Path: filepath.Join(dir, "l0", "x.jsonl")}
	job, err := queue.NewJob(MemoryJobL1Extract, p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("models-less skip must not error: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["status"] != "skipped_no_model" {
		t.Fatalf("status = %v, want skipped_no_model", res)
	}
	if strings.Count(buf.String(), "no memory model available") != 1 {
		t.Fatalf("want exactly ONE Warn, log:\n%s", buf.String())
	}

	// A second cycle (the queue re-driving the same window) must not
	// re-warn — one Warn per worker, not one per retry cycle.
	job2, err := queue.NewJob(MemoryJobL1Extract, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Process(context.Background(), job2); err != nil {
		t.Fatalf("second skip must not error: %v", err)
	}
	if strings.Count(buf.String(), "no memory model available") != 1 {
		t.Fatalf("second cycle must not re-warn, log:\n%s", buf.String())
	}
}
