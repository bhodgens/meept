package memory

import (
	"context"
	"strings"
	"testing"
)

// TestAtomicCompaction_ReducesFixtureWindow is the wave-2 functional pin
// (HALO Phase 6, plan
// docs/plans/2026-07-17-halo-implementation-status.md priority #1): the
// Compactor reduces a fixture conversation context — merged tool +
// observation pairs, deduped observations, and a summary middle produce
// strictly fewer turns than went in.
func TestAtomicCompaction_ReducesFixtureWindow(t *testing.T) {
	compactor := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 4,
		KeepLastNTurns:        1,
	})

	// Fixture: 12 observation turns (over the trigger, dedupable).
	turns := make([]TurnRecord, 0, 12)
	for i := range 12 {
		turns = append(turns, TurnRecord{
			Index:   i,
			Type:    "observation",
			Content: "obs body",
			Tokens:  10,
		})
	}

	result, compacted := compactor.Compact(context.Background(), turns)
	if result.OriginalTurnCount != len(turns) {
		t.Fatalf("OriginalTurnCount = %d, want %d", result.OriginalTurnCount, len(turns))
	}
	if len(compacted) >= len(turns) {
		t.Fatalf("compaction must shrink the fixture window: %d -> %d", len(turns), len(compacted))
	}
	if result.CompressionRatio >= 1.0 {
		t.Fatalf("expected compression ratio < 1.0, got %f", result.CompressionRatio)
	}
	if result.Summary == "" {
		t.Fatal("expected a non-empty compaction summary")
	}
}

// TestDistillWorker_CompactedWindowFeedsPrompt pins the L1 integration: a
// DistillWorker with a compactor passes the COMPACTED window to the LLM
// prompt (fewer lines than records), while a nil compactor passes the raw
// window byte-identically.
func TestDistillWorker_CompactedWindowFeedsPrompt(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-atomic"
	dataDir, l0Path, recs := writeL0Fixture(t, t.TempDir(), session, 8)

	llm := &fakeDistillLLM{script: []fakeDistillEntry{
		{match: "", response: candidateJSON("user believes alpha ships friday", "user prefers go")},
	}}
	compactor := NewCompactor(CompactionConfig{
		MaxTurnsBeforeCompact: 4,
		KeepLastNTurns:        1,
	})
	w := NewDistillWorker(DistillWorkerConfig{
		Manager:   mgr,
		LLM:       llm,
		DataDir:   dataDir,
		Compactor: compactor,
		Logger:    testDiscardLogger(),
	})

	payload, err := jsonMarshalForTest(L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Process(context.Background(), newL1JobForTest(payload)); err != nil {
		t.Fatalf("process l1: %v", err)
	}

	// The prompt must carry the compacted window: fewer fixture-window
	// content lines than the 8 raw records (merge/dedup/summary ran), and
	// the prompt must differ from the raw-window rendering.
	if llm.prompt == "" {
		t.Fatal("LLM was not called")
	}
	if llm.prompt == ambientPromptForTest(joinL0Conversation(recs)) {
		t.Fatal("compacted prompt must differ from the raw-window prompt")
	}
	if !strings.Contains(llm.prompt, "Compacted: ") {
		t.Fatalf("compacted prompt must carry the summary turn marker; prompt=%s", llm.prompt)
	}
}

// TestDistillWorker_NilCompactorKeepsRawPrompt pins byte-identical disabled
// behavior: no compactor means the prompt is exactly the raw L0 window.
func TestDistillWorker_NilCompactorKeepsRawPrompt(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-atomic-off"
	dataDir, l0Path, recs := writeL0Fixture(t, t.TempDir(), session, 6)

	llm := &fakeDistillLLM{script: []fakeDistillEntry{
		{match: "", response: candidateJSON("user believes alpha ships friday", "user prefers go")},
	}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  testDiscardLogger(),
	})

	payload, err := jsonMarshalForTest(L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Process(context.Background(), newL1JobForTest(payload)); err != nil {
		t.Fatalf("process l1: %v", err)
	}

	want := joinL0Conversation(recs)
	if llm.prompt != ambientPromptForTest(want) {
		t.Fatalf("disabled compaction must keep the raw-window prompt byte-identical")
	}
}
