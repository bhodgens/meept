package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// fakeOracle is a scripted Oracle for judge tests.
type fakeOracle struct {
	name string
	res  OracleResult
	err  error
}

func (f *fakeOracle) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f *fakeOracle) Check(_ context.Context, _ string) (OracleResult, error) {
	return f.res, f.err
}

func TestJudgeAllToolsOKOraclePass(t *testing.T) {
	steps := []Step{
		{Name: "read_file"},
		{Name: "edit_file"},
		{Name: "go_test"},
	}
	j, err := Judge(context.Background(), steps, &fakeOracle{res: OracleResult{Passed: true}}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if !j.Passed {
		t.Errorf("expected Passed=true, got %+v", j)
	}
	if j.FirstErrorStep != 0 {
		t.Errorf("expected FirstErrorStep=0, got %d", j.FirstErrorStep)
	}
	if j.OracleName != "fake" {
		t.Errorf("expected OracleName=fake, got %q", j.OracleName)
	}
}

func TestJudgeFirstOfThreeToolsFails(t *testing.T) {
	steps := []Step{
		{Name: "run_build", Err: "exit status 1", Failed: true},
		{Name: "read_file"},
		{Name: "edit_file"},
	}
	j, err := Judge(context.Background(), steps, &fakeOracle{res: OracleResult{Passed: true}}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if j.Passed {
		t.Errorf("expected Passed=false, got %+v", j)
	}
	if j.FirstErrorStep != 1 {
		t.Errorf("expected FirstErrorStep=1 (1-based), got %d", j.FirstErrorStep)
	}
	if j.Summary == "" {
		t.Errorf("expected non-empty summary for failure")
	}
}

func TestJudgeOracleFailsAfterCleanTools(t *testing.T) {
	steps := []Step{{Name: "go_test"}, {Name: "go_vet"}}
	j, err := Judge(context.Background(), steps, &fakeOracle{res: OracleResult{Passed: false, Output: "FAIL x"}}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if j.Passed {
		t.Errorf("expected Passed=false, got %+v", j)
	}
	// Outcome-only fail: clean tools, oracle failed -> len(steps)+1.
	if j.FirstErrorStep != len(steps)+1 {
		t.Errorf("expected FirstErrorStep=%d, got %d", len(steps)+1, j.FirstErrorStep)
	}
}

func TestJudgeOracleFailsMidTrajectoryKeepsToolStep(t *testing.T) {
	steps := []Step{{Name: "a"}, {Name: "b", Failed: true, Err: "boom"}, {Name: "c"}}
	j, err := Judge(context.Background(), steps, &fakeOracle{res: OracleResult{Passed: false}}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if j.Passed {
		t.Errorf("expected Passed=false, got %+v", j)
	}
	if j.FirstErrorStep != 2 {
		t.Errorf("expected FirstErrorStep=2 (first failing tool, not oracle sentinel), got %d", j.FirstErrorStep)
	}
}

func TestJudgeEmptyStepsFailingOracle(t *testing.T) {
	j, err := Judge(context.Background(), nil, &fakeOracle{res: OracleResult{Passed: false}}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if j.Passed {
		t.Errorf("expected Passed=false, got %+v", j)
	}
	// len(steps)+1 with zero steps yields the 1-based sentinel for the
	// outcome itself.
	if j.FirstErrorStep != 1 {
		t.Errorf("expected FirstErrorStep=1, got %d", j.FirstErrorStep)
	}
}

func TestJudgeEmptyStepsPassingOracle(t *testing.T) {
	// Documented choice: no steps with a passing oracle means the outcome
	// was achieved without any tool use; that is a pass, not a skip.
	j, err := Judge(context.Background(), nil, &fakeOracle{res: OracleResult{Passed: true}}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if !j.Passed {
		t.Errorf("expected Passed=true for empty steps + passing oracle, got %+v", j)
	}
	if j.FirstErrorStep != 0 {
		t.Errorf("expected FirstErrorStep=0, got %d", j.FirstErrorStep)
	}
}

func TestJudgeOracleErrorFailsClosed(t *testing.T) {
	j, err := Judge(context.Background(), []Step{{Name: "a"}}, &fakeOracle{err: errors.New("oracle exploded")}, t.TempDir())
	if err != nil {
		t.Fatalf("Judge should not surface oracle launch errors; got %v", err)
	}
	if j.Passed {
		t.Errorf("expected Passed=false when oracle errors, got %+v", j)
	}
	if j.FirstErrorStep != 2 {
		t.Errorf("expected FirstErrorStep=2 (len+1 outcome sentinel), got %d", j.FirstErrorStep)
	}
}

func TestStepsFromMessages(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "fix the bug"},
		{Role: llm.RoleAssistant, Content: "reading the handler"},
		{Role: llm.RoleTool, Name: "read_file", Content: "package main"},
		{Role: llm.RoleAssistant, Content: "editing"},
		{Role: llm.RoleTool, Name: "edit_file", Content: "patched", IsToolError: true},
		{Role: llm.RoleTool, Name: "go_test", Content: "exit status 1: FAIL pkg/x", IsToolError: true},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	steps := StepsFromMessages(msgs)
	if len(steps) != 3 {
		t.Fatalf("expected 3 tool steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].Name != "read_file" || steps[0].Failed {
		t.Errorf("step 0: expected clean read_file, got %+v", steps[0])
	}
	if steps[1].Name != "edit_file" || !steps[1].Failed {
		t.Errorf("step 1: expected failing edit_file, got %+v", steps[1])
	}
	if steps[2].Name != "go_test" || !steps[2].Failed || steps[2].Err == "" {
		t.Errorf("step 2: expected failing go_test with err text, got %+v", steps[2])
	}
}

func TestStepsFromMessagesFailureMarkers(t *testing.T) {
	// IsToolError is the primary signal; a recognized failure marker in the
	// content must also mark the step failed even when the flag is absent
	// (older serialized conversations have Content only).
	msgs := []llm.ChatMessage{
		{Role: llm.RoleTool, Name: "t", Content: "Error: exit status 1"},
		{Role: llm.RoleTool, Name: "u", Content: "command failed with exit code 2"},
		{Role: llm.RoleTool, Name: "v", Content: "Traceback (most recent call last):"},
		{Role: llm.RoleTool, Name: "w", Content: "all good here"},
		{Role: llm.RoleTool, Name: "x", Content: "error: something went wrong"},
	}
	steps := StepsFromMessages(msgs)
	if len(steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(steps))
	}
	for i, want := range []bool{true, true, true, false, true} {
		if steps[i].Failed != want {
			t.Errorf("step %d (%s): Failed=%v, want %v (content %q)", i, steps[i].Name, steps[i].Failed, want, steps[i].Err)
		}
	}
}

func TestStepsFromMessagesEmpty(t *testing.T) {
	if steps := StepsFromMessages(nil); steps != nil {
		t.Errorf("expected nil steps for nil messages, got %+v", steps)
	}
}

// TestJudgmentRoundTrip verifies the JSON shape written beside run records
// matches the C7 contract field names.
func TestJudgmentRoundTrip(t *testing.T) {
	j := TrajectoryJudgment{
		TrajectoryID:   "run-123",
		Passed:         false,
		FirstErrorStep: 3,
		Summary:        "go_test failed",
		OracleName:     "shell:go test ./...",
	}
	b, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"trajectory_id":"run-123","passed":false,"first_error_step":3,"summary":"go_test failed","oracle_name":"shell:go test ./..."}`
	if string(b) != want {
		t.Errorf("wire shape mismatch:\n got: %s\nwant: %s", b, want)
	}
	var back TrajectoryJudgment
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != j {
		t.Errorf("round trip mismatch: %+v != %+v", back, j)
	}
}

func TestSaveLoadJudgment(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)
	in := TrajectoryJudgment{TrajectoryID: "abc", Passed: true, OracleName: "fake"}
	if err := store.SaveJudgment(context.Background(), in); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}

	// Land next to the run record.
	if _, err := os.Stat(filepath.Join(dir, "abc.judgment.json")); err != nil {
		t.Fatalf("expected abc.judgment.json beside run record: %v", err)
	}

	out, err := store.LoadJudgment(context.Background(), "abc")
	if err != nil {
		t.Fatalf("LoadJudgment: %v", err)
	}
	if out != in {
		t.Errorf("got %+v, want %+v", out, in)
	}

	if _, err := store.LoadJudgment(context.Background(), "missing"); !errors.Is(err, ErrJudgmentNotFound) {
		t.Errorf("expected ErrJudgmentNotFound, got %v", err)
	}
}

func TestSaveJudgmentRejectsBadID(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	if err := store.SaveJudgment(context.Background(), TrajectoryJudgment{TrajectoryID: "../evil"}); err == nil {
		t.Errorf("expected path-traversal rejection")
	}
	if err := store.SaveJudgment(context.Background(), TrajectoryJudgment{}); err == nil {
		t.Errorf("expected empty-id rejection")
	}
}

func TestSaveJudgmentOverwrite(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)
	first := TrajectoryJudgment{TrajectoryID: "dup", Passed: true}
	second := TrajectoryJudgment{TrajectoryID: "dup", Passed: false, FirstErrorStep: 2}
	if err := store.SaveJudgment(context.Background(), first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := store.SaveJudgment(context.Background(), second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	out, err := store.LoadJudgment(context.Background(), "dup")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.Passed {
		t.Errorf("expected overwrite to win, got %+v", out)
	}
}

func TestSaveJudgmentContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewDiskStore(t.TempDir())
	if err := store.SaveJudgment(ctx, TrajectoryJudgment{TrajectoryID: "x"}); err == nil {
		t.Errorf("expected context cancellation to propagate")
	}
}
