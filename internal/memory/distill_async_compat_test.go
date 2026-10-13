package memory_test

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

// External-package roundtrip test: fixtures are written through the REAL
// l0.Recorder (frozen §2.1 schema), then consumed by memory.DistillWorker.
// Lives in package memory_test because internal/memory/l0 imports
// internal/memory — an in-package import would be a cycle.

type compatLLM struct {
	response string
	err      error
	prompt   string
}

func (f *compatLLM) DistillComplete(_ context.Context, prompt string) ([]byte, error) {
	f.prompt = prompt
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.response), nil
}

func TestDistillWorkerRoundtripsRealL0RecorderOutput(t *testing.T) {
	dataDir := t.TempDir()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  dataDir,
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	session := "sess-compat"
	rec := l0.NewRecorder(dataDir, nil)
	for i := 0; i < 3; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if err := rec.Append(session, role, "compat turn about the memory pipeline", nil); err != nil {
			t.Fatalf("recorder append: %v", err)
		}
	}
	// A second session in the same day file must be ignored by the worker.
	if err := rec.Append("sess-other", "user", "other session noise", nil); err != nil {
		t.Fatalf("recorder append other: %v", err)
	}

	l0Path := rec.DatePath(time.Now().Format("2006-01-02"))
	llm := &compatLLM{response: `[{"type":"claim","text":"user builds memory pipelines","source":"conversation","confidence":0.8,"premises":[],"category":"technical"}]`}
	chain := &compatEnqueuer{}
	w := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager:  mgr,
		LLM:      llm,
		DataDir:  dataDir,
		Enqueuer: chain,
		Logger:   slog.New(slog.DiscardHandler),
	})

	payload, err := json.Marshal(memory.L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Process(context.Background(), &queue.Job{Type: memory.MemoryJobL1Extract, Payload: payload}); err != nil {
		t.Fatalf("l1 process: %v", err)
	}
	if !strings.Contains(llm.prompt, "compat turn about the memory pipeline") {
		t.Fatal("prompt missing real recorder content")
	}
	if strings.Contains(llm.prompt, "other session noise") {
		t.Fatal("prompt leaked another session's lines")
	}
	if len(chain.jobs) != 1 || chain.jobs[0].Type != memory.MemoryJobL2Scene {
		t.Fatalf("expected one chained l2 job, got %d", len(chain.jobs))
	}
	// The chained L2 job's payload roundtrips the frozen contract.
	var lp memory.L2ScenePayload
	if err := json.Unmarshal(chain.jobs[0].Payload, &lp); err != nil {
		t.Fatalf("chained l2 payload: %v", err)
	}
	if lp.SessionID != session {
		t.Fatalf("chained l2 session = %q", lp.SessionID)
	}

	// Now run the chained L2 job: scene file written from the same day file.
	if _, err := w.Process(context.Background(), chain.jobs[0]); err != nil {
		t.Fatalf("l2 process: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "l2"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("l2 scene files missing: %v", err)
	}
	if !strings.HasPrefix(entries[0].Name(), session) {
		t.Fatalf("scene filename %q does not carry session prefix", entries[0].Name())
	}

	// A duplicate L1 re-run with the advanced marker position suppresses.
	recs, err := l0.ReadSession(filepath.Join(dataDir, "l0"), time.Now().Format("2006-01-02"), session)
	if err != nil {
		t.Fatal(err)
	}
	dup, err := json.Marshal(memory.L1ExtractPayload{SessionID: session, L0Path: l0Path, SinceMsgID: recs[len(recs)-1].ID})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Process(context.Background(), &queue.Job{Type: memory.MemoryJobL1Extract, Payload: dup})
	if err != nil {
		t.Fatalf("duplicate l1: %v", err)
	}
	if res == nil {
		t.Fatal("nil result from duplicate l1")
	}
}

func TestDistillWorkerL1LLMErrorPropagatesForQueueRetry(t *testing.T) {
	dataDir := t.TempDir()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  dataDir,
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	session := "sess-compat-err"
	rec := l0.NewRecorder(dataDir, nil)
	if err := rec.Append(session, "user", "hello pipeline", nil); err != nil {
		t.Fatal(err)
	}
	l0Path := rec.DatePath(time.Now().Format("2006-01-02"))
	llm := &compatLLM{err: errors.New("quota blocked")}
	w := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})
	payload, _ := json.Marshal(memory.L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if _, err := w.Process(context.Background(), &queue.Job{Type: memory.MemoryJobL1Extract, Payload: payload}); err == nil {
		t.Fatal("expected llm error to propagate (queue retry owns backoff)")
	}
}

type compatEnqueuer struct {
	jobs []*queue.Job
}

func (f *compatEnqueuer) Enqueue(_ context.Context, job *queue.Job) error {
	f.jobs = append(f.jobs, job)
	return nil
}
