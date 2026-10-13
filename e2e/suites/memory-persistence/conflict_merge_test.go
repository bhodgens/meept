//go:build e2e

// Conflict detection & merge wiring scenarios (leaf T02-L2). Lives in the
// memory-persistence suite: the same sandboxed SQLite manager, and the
// conflict path is a memory-package concern (internal/memory maps to this
// suite in e2e/manifest.json).
package memorypersistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/conflict"
	"github.com/caimlas/meept/internal/queue"
	"log/slog"
)

// fakeDistillLLMSingle returns one canned extraction response for every
// DistillComplete call.
type fakeDistillLLMSingle struct {
	response string
}

func (f *fakeDistillLLMSingle) DistillComplete(_ context.Context, _ string) ([]byte, error) {
	return []byte(f.response), nil
}

// fakeConflictLLM scripts conflict.Detector LLM responses.
type fakeConflictLLM struct {
	mu       sync.Mutex
	response string
	err      error
	prompts  []string
}

func (f *fakeConflictLLM) Complete(_ context.Context, prompt string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.response), nil
}

// newConflictTestManager builds an initialized SQLite-backed manager with the
// calibration logger writing claim_verdicts.jsonl into dir.
func newConflictTestManager(t *testing.T, dir string) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  dir,
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.SetCalibrationLogger(memory.NewCalibrationLogger(dir, slog.New(slog.DiscardHandler)))
	return mgr
}

// writeConflictL0Fixture writes one user turn for the session into
// <dir>/l0/<today>.jsonl and returns the file path.
func writeConflictL0Fixture(t *testing.T, dir, sessionID, content string) string {
	t.Helper()
	date := time.Now().Format("2006-01-02")
	l0Dir := filepath.Join(dir, "l0")
	if err := os.MkdirAll(l0Dir, 0o755); err != nil {
		t.Fatalf("mkdir l0: %v", err)
	}
	rec := map[string]any{
		"id":         "msg-conflict-0001",
		"ts":         time.Now().Format(time.RFC3339),
		"session_id": sessionID,
		"role":       "user",
		"content":    content,
		"meta":       map[string]any{},
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal l0 fixture: %v", err)
	}
	path := filepath.Join(l0Dir, date+".jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write l0 fixture: %v", err)
	}
	return path
}

func conflictExtractResponse(text string) string {
	return fmt.Sprintf(`[{"type":"claim","text":%q,"source":"conversation","confidence":0.8,"premises":[],"category":"general"}]`, text)
}

// TestMemoryConflict_MergeSupersedesTarget covers memory-persistence-03:
// seed claim "user deploys on Fridays" -> L1 extract candidate "user deploys
// on Friday afternoons" -> fake detector says merge -> the merged claim is
// stored with conflict_action=merge, the old claim's validity window closes,
// and a supersede verdict row lands in claim_verdicts.jsonl.
func TestMemoryConflict_MergeSupersedesTarget(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	session := "sess-conflict-merge"
	mgr := newConflictTestManager(t, dir)

	seedID, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:       "user deploys on Fridays",
		Source:     "conversation",
		Confidence: 0.7,
		Status:     memory.ClaimStatusAuto,
	})
	if err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	l0Path := writeConflictL0Fixture(t, dir, session, "heads up that I deploy on Friday afternoons these days")

	distillFake := &fakeDistillLLMSingle{response: conflictExtractResponse("user deploys on Friday afternoons")}
	conflictFake := &fakeConflictLLM{response: fmt.Sprintf(
		`[{"candidate":0,"action":"merge","target_ids":["%s"],"merged_content":"user deploys on Friday afternoons","reason":"same deploy-schedule fact"}]`, seedID)}

	w := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: mgr,
		LLM:     distillFake,
		DataDir: dir,
		Logger:  slog.New(slog.DiscardHandler),
		Conflict: &memory.ConflictWiring{
			Enabled:  true,
			TopK:     5,
			MaxBatch: 20,
			LLM:      conflictFake,
		},
	})

	payload, err := json.Marshal(memory.L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Process(ctx, &queue.Job{Type: memory.MemoryJobL1Extract, Payload: payload})
	if err != nil {
		t.Fatalf("process l1: %v", err)
	}
	got := res.(map[string]any)
	if got["conflict_merged"].(int) != 1 {
		t.Fatalf("conflict_merged = %v, want 1", got["conflict_merged"])
	}
	if got["claims_stored"].(int) != 1 {
		t.Fatalf("claims_stored = %v, want 1 (merged claim only)", got["claims_stored"])
	}
	if len(conflictFake.prompts) != 1 {
		t.Fatalf("conflict llm calls = %d, want 1", len(conflictFake.prompts))
	}

	// The merged claim exists and carries the merge metadata (the surface
	// `meept memory conflicts` / memory.listConflicts reads).
	merges, err := mgr.ListMergeClaims(ctx, time.Time{}, 20)
	if err != nil {
		t.Fatalf("ListMergeClaims: %v", err)
	}
	if len(merges) != 1 {
		t.Fatalf("ListMergeClaims = %d claims, want 1", len(merges))
	}
	m := merges[0].Memory
	if m.Content != "user deploys on Friday afternoons" {
		t.Fatalf("merged claim content = %q", m.Content)
	}
	if m.Metadata["conflict_action"] != "merge" {
		t.Fatalf("conflict_action = %v, want merge", m.Metadata["conflict_action"])
	}
	targets, _ := m.Metadata["conflict_targets"].([]any)
	if len(targets) != 1 || targets[0] != seedID {
		t.Fatalf("conflict_targets = %v, want [%s]", targets, seedID)
	}

	// The old claim is superseded: valid_to closed by MarkSuperseded.
	// GetByID filters is_current, and MarkSuperseded flipped the seeded
	// claim non-current — read it back through the raw store instead.
	oldRes, err := mgr.Episodic().GetByID(ctx, seedID)
	if err != nil || oldRes == nil {
		t.Fatalf("get seeded claim after supersede: %v", err)
	}
	old := &oldRes.Memory
	if vt, _ := old.Metadata["valid_to"].(string); vt == "" {
		t.Fatal("seeded claim valid_to not closed after merge supersede")
	}
	if _, ok := old.Metadata["superseded_at"].(string); !ok {
		t.Fatal("seeded claim superseded_at not stamped after merge supersede")
	}

	// A calibration/verdict row was written for the supersede.
	calBytes, err := os.ReadFile(filepath.Join(dir, "claim_verdicts.jsonl"))
	if err != nil {
		t.Fatalf("read claim_verdicts.jsonl: %v", err)
	}
	cal := string(calBytes)
	if !strings.Contains(cal, seedID) || !strings.Contains(cal, "supersede") {
		t.Fatalf("calibration log missing supersede row for %s: %s", seedID, cal)
	}
}

// TestMemoryConflict_FailOpenStoresAll covers memory-persistence-04: an
// injected detector LLM failure fails open — every candidate is stored plain,
// the extraction succeeds, and no merge metadata is written.
func TestMemoryConflict_FailOpenStoresAll(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	session := "sess-conflict-failopen"
	mgr := newConflictTestManager(t, dir)

	seedID, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:       "user deploys on Fridays",
		Source:     "conversation",
		Confidence: 0.7,
		Status:     memory.ClaimStatusAuto,
	})
	if err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	l0Path := writeConflictL0Fixture(t, dir, session, "heads up that I deploy on Friday afternoons these days")

	distillFake := &fakeDistillLLMSingle{response: conflictExtractResponse("user deploys on Friday afternoons")}
	conflictFake := &fakeConflictLLM{err: errors.New("injected detector outage")}

	w := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: mgr,
		LLM:     distillFake,
		DataDir: dir,
		Logger:  slog.New(slog.DiscardHandler),
		Conflict: &memory.ConflictWiring{
			Enabled: true,
			LLM:     conflictFake,
		},
	})

	payload, err := json.Marshal(memory.L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Process(ctx, &queue.Job{Type: memory.MemoryJobL1Extract, Payload: payload})
	if err != nil {
		t.Fatalf("process l1 must not fail on detector error: %v", err)
	}
	got := res.(map[string]any)
	if got["claims_stored"].(int) != 1 {
		t.Fatalf("claims_stored = %v, want 1 (fail-open store)", got["claims_stored"])
	}

	// The candidate was stored plain: no conflict metadata anywhere, and the
	// seeded claim is still in force.
	merges, err := mgr.ListMergeClaims(ctx, time.Time{}, 20)
	if err != nil {
		t.Fatalf("ListMergeClaims: %v", err)
	}
	if len(merges) != 0 {
		t.Fatalf("fail-open wrote %d merge claims, want 0", len(merges))
	}
	old, err := mgr.GetByID(ctx, seedID)
	if err != nil {
		t.Fatalf("get seeded claim: %v", err)
	}
	if vt, _ := old.Metadata["valid_to"].(string); vt != "" {
		t.Fatal("fail-open must not supersede the seeded claim")
	}
	if _, ok := old.Metadata["conflict_action"]; ok {
		t.Fatal("fail-open stamped conflict metadata on a claim")
	}
}

// compile-time reference: the conflict detector seam the wiring consumes.
var _ conflict.LLM = (*fakeConflictLLM)(nil)
