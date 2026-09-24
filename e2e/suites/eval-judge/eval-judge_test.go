//go:build e2e

// Suite eval-judge: the deterministic trajectory judge — graded verdicts
// over recorded tool steps with contentLooksFailed honesty (markers count
// as failure even without the error flag), oracle fail-closed behavior,
// and the DiskStore judgment round-trip.
package evaljudge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/eval"
	"github.com/caimlas/meept/internal/llm"
)

// passOracle fails open ONLY when its file exists (a real success
// criterion against a workdir).
type passOracle struct{ name string }

func (p passOracle) Name() string { return p.name }
func (p passOracle) Check(_ context.Context, workdir string) (eval.OracleResult, error) {
	if _, err := os.Stat(filepath.Join(workdir, "artifact.txt")); err != nil {
		return eval.OracleResult{Passed: false, Output: "artifact.txt missing"}, nil
	}
	return eval.OracleResult{Passed: true, Output: "artifact present"}, nil
}

// brokenOracle always fails to launch (the fail-closed input).
type brokenOracle struct{}

func (brokenOracle) Name() string { return "broken" }
func (brokenOracle) Check(context.Context, string) (eval.OracleResult, error) {
	return eval.OracleResult{}, errors.New("oracle runtime unavailable")
}

// TestEvalJudge_GradesTrajectoryWithHonestContentFailure covers
// eval-judge-01: the judge grades a recorded trajectory; a tool result
// whose content carries a failure marker is honestly failed even when the
// IsToolError flag is absent, and FirstErrorStep points at the exact step.
func TestEvalJudge_GradesTrajectoryWithHonestContentFailure(t *testing.T) {
	ctx := context.Background()
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "artifact.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	// StepsFromMessages: only tool-role messages become steps; the
	// IsToolError flag and content markers both fail honestly.
	msgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "please deploy"},
		{Role: llm.RoleAssistant, Content: "running deploy"},
		{Role: llm.RoleTool, Name: "shell", Content: "build succeeded\nexit status 0"}, // marker mid-line fails...
		{Role: llm.RoleTool, Name: "deploy", Content: "all green"},
	}
	steps := eval.StepsFromMessages(msgs)
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want the 2 tool rows only", len(steps))
	}
	// Honest content failure: "exit status 0" line-STARTS with a marker.
	if !steps[0].Failed {
		t.Fatalf("contentLooksFailed missed the failure marker: %+v", steps[0])
	}
	if steps[1].Failed || steps[1].Err != "" {
		t.Fatalf("clean tool result must not be failed or carry Err: %+v", steps[1])
	}

	// Judge: the failed step pins FirstErrorStep at 1 (1-based), the
	// earlier signal wins over the passing oracle.
	judgment, err := eval.Judge(ctx, steps, passOracle{"artifact"}, workdir)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if judgment.Passed {
		t.Fatal("trajectory with a failed tool step must not pass")
	}
	if judgment.FirstErrorStep != 1 {
		t.Fatalf("FirstErrorStep = %d, want 1", judgment.FirstErrorStep)
	}
	if !strings.Contains(judgment.Summary, "step 1") {
		t.Fatalf("summary should name the failing step: %q", judgment.Summary)
	}

	// All-clean steps + passing oracle = pass, FirstErrorStep 0.
	clean := []eval.Step{{Name: "shell"}, {Name: "deploy"}}
	passed, err := eval.Judge(ctx, clean, passOracle{"artifact"}, workdir)
	if err != nil {
		t.Fatalf("Judge clean: %v", err)
	}
	if !passed.Passed || passed.FirstErrorStep != 0 {
		t.Fatalf("clean trajectory: %+v", passed)
	}

	// Outcome-only fail: clean tools but the oracle criterion unmet →
	// FirstErrorStep = len(steps)+1 (the outcome sentinel).
	emptyDir := t.TempDir()
	outcomeFail, err := eval.Judge(ctx, clean, passOracle{"artifact"}, emptyDir)
	if err != nil {
		t.Fatalf("Judge outcome-only: %v", err)
	}
	if outcomeFail.Passed {
		t.Fatal("failing oracle must fail the judgment")
	}
	if outcomeFail.FirstErrorStep != len(clean)+1 {
		t.Fatalf("outcome-only FirstErrorStep = %d, want %d", outcomeFail.FirstErrorStep, len(clean)+1)
	}

	// Fail closed: a BROKEN oracle can never mark a trajectory passed.
	broken, err := eval.Judge(ctx, clean, brokenOracle{}, workdir)
	if err != nil {
		t.Fatalf("Judge broken oracle: %v", err)
	}
	if broken.Passed {
		t.Fatal("broken oracle failed open — the judgment marked a trajectory passed")
	}
	if broken.FirstErrorStep != len(clean)+1 {
		t.Fatalf("broken-oracle sentinel = %d, want %d", broken.FirstErrorStep, len(clean)+1)
	}
	if !strings.Contains(broken.Summary, "oracle") {
		t.Fatalf("broken-oracle summary should name the oracle failure: %q", broken.Summary)
	}

	// DiskStore judgment round-trip beside the run record.
	store := eval.NewDiskStore(t.TempDir())
	judgment.TrajectoryID = "traj-e2e-1"
	judgment.OracleName = "artifact"
	if err := store.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
	loaded, err := store.LoadJudgment(ctx, judgment.TrajectoryID)
	if err != nil {
		t.Fatalf("LoadJudgment: %v", err)
	}
	if loaded.Passed != judgment.Passed || loaded.FirstErrorStep != judgment.FirstErrorStep {
		t.Fatalf("judgment round-trip drifted: %+v vs %+v", loaded, judgment)
	}
	if _, err := store.LoadJudgment(ctx, "never-recorded"); !errors.Is(err, eval.ErrJudgmentNotFound) {
		t.Fatalf("unknown judgment = %v, want ErrJudgmentNotFound", err)
	}
}
