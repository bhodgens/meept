package selfimprove

import (
	"testing"
)

// fakeJudgment is a local judgment stand-in for filter tests. The real
// eval.TrajectoryJudgment call shape is proven in comm/http tests, which can
// import both packages without a cycle.
type fakeJudgment struct {
	passed bool
}

func (f fakeJudgment) JudgedPassed() bool { return f.passed }

func judgedTestPattern(id, trajID string) LearnedPattern {
	return LearnedPattern{
		ID:       id,
		Type:     PatternTypeStrategy,
		Status:   PatternStatusPending,
		Metadata: map[string]any{"trajectory_id": trajID},
	}
}

func TestOnlyJudged(t *testing.T) {
	patterns := []LearnedPattern{
		judgedTestPattern("p1", "t1"),                                       // judged pass -> promoted
		judgedTestPattern("p2", "t2"),                                       // judged fail -> dropped
		judgedTestPattern("p3", "t3"),                                       // unjudged (absent from map) -> dropped
		judgedTestPattern("p4", ""),                                         // empty trajectory_id -> dropped
		{ID: "p5", Type: PatternTypeStrategy, Status: PatternStatusPending}, // no metadata -> dropped
		judgedTestPattern("p6", "t6"),                                       // t6 absent from judgment map -> dropped
		{ID: "p7", Type: PatternTypeStrategy, Status: PatternStatusPending,
			Metadata: map[string]any{"trajectory_id": 42}}, // non-string trajectory_id -> dropped
	}
	judgments := map[string]fakeJudgment{
		"t1": {passed: true},
		"t2": {passed: false},
	}

	got := OnlyJudged(patterns, judgments)
	if len(got) != 1 {
		t.Fatalf("expected 1 promoted pattern, got %d: %+v", len(got), got)
	}
	if got[0].ID != "p1" {
		t.Errorf("expected p1 promoted, got %s", got[0].ID)
	}
}

func TestOnlyJudgedEmptyJudgments(t *testing.T) {
	patterns := []LearnedPattern{judgedTestPattern("p1", "t1")}
	got := OnlyJudged(patterns, map[string]fakeJudgment(nil))
	if got == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty result with no judgments, got %+v", got)
	}
}

func TestOnlyJudgedEmptyPatterns(t *testing.T) {
	got := OnlyJudged(nil, map[string]fakeJudgment{"t1": {passed: true}})
	if got == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %+v", got)
	}
}
