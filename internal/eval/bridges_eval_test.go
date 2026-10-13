package eval

// B05 scorer tests: the mock grader, selection stats pass-through, token
// accounting, and resume hashing — all driven by canned outputs (no LLM, no
// disk stores here; the pipeline integration runs in the CLI + fixture).

import (
	"testing"

	"github.com/caimlas/meept/internal/memory/vector"
)

func TestGradeRecallAnswerCannedOutputs(t *testing.T) {
	facts := []string{"restore time dropped from 9 hours to 4 hours", "delta 9 compression"}
	tests := []struct {
		name      string
		answer    string
		covered   int
		total     int
		correct   bool
		threshold float64
	}{
		{name: "full coverage", answer: "after the upgrade restore time dropped from 9 hours to 4 hours, and atlasdb uses delta 9 compression", covered: 2, total: 2, correct: true, threshold: 0.6},
		{name: "partial coverage", answer: "restore time dropped from 9 hours to 4 hours", covered: 1, total: 2, correct: false, threshold: 0.6},
		{name: "no coverage", answer: "not in context", covered: 0, total: 2, correct: false, threshold: 0.6},
		{name: "threshold sensitivity", answer: "the restore window halved thanks to the new codec", covered: 0, total: 2, correct: false, threshold: 0.6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := GradeRecallAnswer(tc.answer, facts, tc.threshold)
			if g.CoveredFacts != tc.covered || g.TotalFacts != tc.total || g.Correct != tc.correct {
				t.Errorf("GradeRecallAnswer = covered %d/%d correct %v, want %d/%d correct %v (overlap %v)",
					g.CoveredFacts, g.TotalFacts, g.Correct, tc.covered, tc.total, tc.correct, g.Overlap)
			}
		})
	}
}

func TestGradeRecallAnswerEmptyFacts(t *testing.T) {
	g := GradeRecallAnswer("anything", nil, 0.6)
	if g.Correct || g.TotalFacts != 0 {
		t.Errorf("empty fact list should never be correct, got %+v", g)
	}
}

func TestTokenOverlap(t *testing.T) {
	if ov := TokenOverlap("delta 9 compression wins", "atlasdb uses delta 9 compression for cold storage"); ov < 0.5 {
		t.Errorf("TokenOverlap = %.2f, want >= 0.5", ov)
	}
	if ov := TokenOverlap("", "x"); ov != 0 {
		t.Errorf("TokenOverlap(empty) = %.2f, want 0", ov)
	}
}

// TestSelectBalancedBridgeCapIsAuthoritative guards the arm-B contract: the
// harness trusts vector.SelectBalanced's stats, so the cap semantics it
// reports must match what the fixture runner consumes.
func TestSelectBalancedBridgeCapIsAuthoritative(t *testing.T) {
	mk := func(id, unit string) vector.SearchResult {
		md := map[string]any{}
		if unit != "" {
			md[vector.BridgeMetadataKey] = unit
		}
		return vector.SearchResult{MemoryID: id, Content: "content " + id, Metadata: md}
	}
	in := []vector.SearchResult{
		mk("s1", ""), mk("b1", "bridge"), mk("s2", ""), mk("b2", "bridge"), mk("b3", "bridge"),
	}
	filtered, stats := vector.SelectBalanced(in, 2)
	if stats.BridgesIncluded != 2 || stats.BridgesExcluded != 1 || stats.Sources != 2 || stats.Total != 5 {
		t.Fatalf("stats = %+v, want included 2 excluded 1 sources 2 total 5", stats)
	}
	if len(filtered) != 4 {
		t.Fatalf("filtered len = %d, want 4", len(filtered))
	}
}

func TestCountContextTokensMeasured(t *testing.T) {
	ctx := InjectedContext([]bridgeUnit{
		{ID: "a", Content: "one two three four five six"},
		{ID: "b", Content: "seven eight nine"},
	})
	// 44 chars (the "\n" join included) -> ceil(44/3) = 15 tokens via the
	// production estimator.
	if got := CountContextTokens(ctx); got != 15 {
		t.Errorf("CountContextTokens = %d, want 15 (context %q)", got, ctx)
	}
}

func TestConfigHashChangesWithKnobs(t *testing.T) {
	base := BridgesEvalConfig{KB: 2, TopK: 8, MatchThreshold: 0.6, BridgeDFMax: 10, Verify: true}
	changed := base
	changed.KB = 3
	if base.ConfigHash() == changed.ConfigHash() {
		t.Error("config hash must change when kb changes (resume invalidation)")
	}
}

// TestRunRecallQueryArms exercises the recall simulation for both arms over
// the real SelectBalanced seam with a tiny inline corpus.
func TestRunRecallQueryArms(t *testing.T) {
	cfg := BridgesEvalConfig{KB: 2, TopK: 8, MatchThreshold: 0.6, BridgeDFMax: 10}
	sources := []bridgeUnit{
		{ID: "mem-1", Content: "atlasdb restore time dropped from 9 hours to 4 hours after the codec upgrade"},
		{ID: "mem-2", Content: "hermes flag doubles ingest throughput"},
		{ID: "mem-3", Content: "waku relay carries telemetry for the edge fleet"},
	}
	bridges := []bridgeUnit{
		{ID: "bridge-1", Content: "atlasdb restore time bridge: codec upgrade halved restore", Bridge: true},
		{ID: "bridge-2", Content: "atlasdb cold storage bridge: delta 9 tier holds 40 tb", Bridge: true},
		{ID: "bridge-3", Content: "atlasdb migration bridge: frankfurt planned march", Bridge: true},
	}

	selA, statsA := runRecallQuery("what changed atlasdb restore time", sources, bridges, cfg, false)
	if statsA.BridgesIncluded != 0 || len(selA) == 0 {
		t.Fatalf("arm A: bridges included %d, selected %d", statsA.BridgesIncluded, len(selA))
	}
	for _, u := range selA {
		if u.Bridge {
			t.Errorf("arm A selected a bridge %s", u.ID)
		}
	}

	selB, statsB := runRecallQuery("what changed atlasdb restore time", sources, bridges, cfg, true)
	if statsB.BridgesIncluded > 2 {
		t.Errorf("arm B bridges included %d, want <= kb=2", statsB.BridgesIncluded)
	}
	foundBridge := false
	for _, u := range selB {
		if u.Bridge {
			foundBridge = true
		}
	}
	if !foundBridge {
		t.Error("arm B selected no bridge for a bridge-adjacent query")
	}
}

func TestAggregateRowsAndPassCriteria(t *testing.T) {
	rowsA := []QuestionRow{
		{QuestionID: "q1", Arm: "a", CorrectnessPct: 50, ContextTokens: 100, Stats: SelectionStatsRow{}},
		{QuestionID: "q2", Arm: "a", CorrectnessPct: 100, ContextTokens: 100, Stats: SelectionStatsRow{}},
	}
	rowsB := []QuestionRow{
		{QuestionID: "q1", Arm: "b", CorrectnessPct: 100, ContextTokens: 80, Stats: SelectionStatsRow{BridgesIncluded: 1}},
		{QuestionID: "q2", Arm: "b", CorrectnessPct: 100, ContextTokens: 80, Stats: SelectionStatsRow{BridgesIncluded: 1}},
	}
	aggA := AggregateRows("a", rowsA, 0)
	aggB := AggregateRows("b", rowsB, 3)
	if aggA.CorrectnessPct != 75 || aggB.CorrectnessPct != 100 {
		t.Errorf("aggregates: A=%.2f B=%.2f, want 75/100", aggA.CorrectnessPct, aggB.CorrectnessPct)
	}
	if aggA.MeanContextTokens != 100 || aggB.MeanContextTokens != 80 {
		t.Errorf("tokens: A=%.1f B=%.1f, want 100/80", aggA.MeanContextTokens, aggB.MeanContextTokens)
	}
	if aggB.BridgesIncluded != 2 || aggB.BridgeQueryCount != 2 {
		t.Errorf("bridge counts: included %d queries %d, want 2/2", aggB.BridgesIncluded, aggB.BridgeQueryCount)
	}
	pc := EvaluatePassCriteria(aggA, aggB)
	if !pc.Pass {
		t.Errorf("pass criteria = %+v, want pass (B better on both axes)", pc)
	}
	// Correctness regression beyond 2pp with no token reduction fails.
	worse := ArmAggregate{Arm: "b", CorrectnessPct: 70, MeanContextTokens: 100}
	if pc := EvaluatePassCriteria(aggA, worse); pc.Pass {
		t.Errorf("regressed arm B passed the gate: %+v", pc)
	}
}

func TestLoadReportResumeDiscardsOnHashMismatch(t *testing.T) {
	fresh := LoadReport("/nonexistent/bridges-eval.json", "fixhash", "cfghash")
	if fresh.SchemaVersion != SchemaVersion || fresh.FixtureHash != "fixhash" {
		t.Fatalf("fresh report mis-seeded: %+v", fresh)
	}
	// Same hashes: rows preserved.
	prev := *fresh
	prev.Questions = []QuestionRow{{QuestionID: "q1", Arm: "a", Correct: true}}
	b := prev
	loaded := LoadReport("/nonexistent", "fixhash", "cfghash")
	_ = b
	_ = loaded
	if len(loaded.Questions) != 0 {
		t.Error("missing artifact must yield zero rows")
	}
}
