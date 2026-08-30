// Package selfimprove_test proves the mandated OnlyJudged call shape with
// real eval.TrajectoryJudgment values. This must live in the external test
// package: internal/selfimprove cannot import internal/eval directly
// (eval → agent → selfimprove cycle), but the external test package can.
package selfimprove_test

import (
	"testing"

	"github.com/caimlas/meept/internal/eval"
	"github.com/caimlas/meept/internal/selfimprove"
)

func TestOnlyJudgedAcceptsEvalTrajectoryJudgments(t *testing.T) {
	patterns := []selfimprove.LearnedPattern{
		{ID: "p1", Metadata: map[string]any{"trajectory_id": "t1"}}, // pass -> kept
		{ID: "p2", Metadata: map[string]any{"trajectory_id": "t2"}}, // fail -> dropped
		{ID: "p3", Metadata: map[string]any{"trajectory_id": "t3"}}, // unjudged -> dropped
	}
	judgments := map[string]eval.TrajectoryJudgment{
		"t1": {TrajectoryID: "t1", Passed: true, OracleName: "shell:true"},
		"t2": {TrajectoryID: "t2", Passed: false, FirstErrorStep: 2},
	}

	got := selfimprove.OnlyJudged(patterns, judgments)
	if len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("expected only p1 promoted, got %+v", got)
	}
}
