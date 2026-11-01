//go:build e2e

// Suite trajectory-golden: the hermetic golden-sequence gate (leaf 03 of
// the trajectory-golden-ci plan). A scripted FakeLLM scenario replays
// through a real scratch daemon, the actual tool-call sequence is
// captured via the leaf-02 harness path, and trajectorydiff grades it
// against the committed C4 fixture using the fixture's own "match"
// block. The gate is BLOCKING inside this suite: a sequence regression
// fails the test, with no retry loops (a hermetic scripted run that
// flakes is a harness bug, not noise).
//
// The negative drill deliberately corrupts one fixture entry in memory
// (reordered sequence) and proves the gate FAILS with a StepDiff-bearing
// summary -- the in-test RED drill that shows the gate bites without a
// second fixture. Nothing on disk is touched; the pristine fixture is
// re-proven afterwards (the "restore" half of the drill).
package trajectorygolden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/eval/trajectorydiff"
)

// fixtureName is the committed fixture this suite gates against
// (e2e/fixtures/golden/<fixtureName>.golden.json).
const fixtureName = "trajectory-golden-basic"

// trajectoryGoldenBasicScript is the canonical suite scenario:
// read_file -> file_write -> read_file verify. It is the EXACT script
// both the fixture-authoring dispatch and every suite replay run, so a
// regenerated fixture and a live replay can never drift apart.
func trajectoryGoldenBasicScript(f *harness.FakeLLM) {
	f.SetPostToolText("scenario finished")
	f.EnqueueToolCalls(
		harness.ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/meept-trajectory-golden-basic/input.txt"}`},
		harness.ToolCall{Name: "file_write", Arguments: `{"path":"/tmp/meept-trajectory-golden-basic/out.txt","content":"golden","direct":true}`},
		harness.ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/meept-trajectory-golden-basic/out.txt"}`},
	)
}

// committedFixturePath resolves the committed fixture from the repo root
// (walked up from the test's working directory -- go test runs a package
// from its own source dir; no repo-root cwd assumption).
func committedFixturePath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "e2e", "fixtures", "golden", fixtureName+".golden.json")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to filesystem root without finding go.mod")
		}
		dir = parent
	}
}

// replayActual captures the scripted scenario through the leaf-02
// CaptureGolden path (scratch daemon + FakeLLM) into a throwaway dir and
// returns the freshly recorded sequence as normalized steps. The written
// fixture is incidental; its golden list IS the actual sequence.
func replayActual(t *testing.T) []trajectorydiff.NormalizedCall {
	t.Helper()
	dir := t.TempDir()
	wrote, err := harness.CaptureGolden(t, harness.CaptureOptions{
		ScenarioName: fixtureName,
		GoldenDir:    dir,
		Script:       trajectoryGoldenBasicScript,
	})
	if err != nil {
		t.Fatalf("replay via CaptureGolden: %v", err)
	}
	actual, err := harness.LoadGoldenFixture(wrote)
	if err != nil {
		t.Fatalf("load replayed fixture: %v", err)
	}
	return harness.FixtureSteps(actual)
}

// loadCommitted loads and validates the committed golden fixture (the
// C4 reader pins schema_version 1 and the fakellm-scripted literal).
func loadCommitted(t *testing.T) *harness.GoldenFixture {
	t.Helper()
	fixture, err := harness.LoadGoldenFixture(committedFixturePath(t))
	if err != nil {
		t.Fatalf("load committed fixture: %v", err)
	}
	if len(fixture.Golden) < 3 {
		t.Fatalf("committed fixture has %d golden entries, want >= 3", len(fixture.Golden))
	}
	return fixture
}

// TestTrajectoryGolden_GoldenPathSequencePasses: given the committed
// golden and the identical script, replay + Compare must PASS. Options
// come from the fixture's "match" block, never hardcoded.
func TestTrajectoryGolden_GoldenPathSequencePasses(t *testing.T) {
	committed := loadCommitted(t)
	golden := harness.FixtureSteps(committed)
	actual := replayActual(t)

	res := trajectorydiff.Compare(golden, actual, committed.Match.Options())
	if !res.Passed {
		t.Fatalf("golden path sequence regressed against %s: %s", fixtureName, res.Summary)
	}
}

// TestTrajectoryGolden_SequenceRegressionFails: the negative RED drill.
// Deliberately corrupt one fixture entry -- swap the write and its verify
// read, the canonical verify-after-acting regression -- and prove the
// gate FAILS with a named first diff and a StepDiff-bearing result. Then
// restore (re-compare with the pristine entry order) and prove PASS, so
// the drill itself demonstrates both verdicts against one fixture.
func TestTrajectoryGolden_SequenceRegressionFails(t *testing.T) {
	committed := loadCommitted(t)
	golden := harness.FixtureSteps(committed)
	actual := replayActual(t)
	opts := committed.Match.Options()

	// Corrupt in memory ONLY: reorder the last two golden entries. The
	// committed fixture on disk is never modified.
	corrupted := append([]trajectorydiff.NormalizedCall(nil), golden...)
	corrupted[len(corrupted)-2], corrupted[len(corrupted)-1] =
		corrupted[len(corrupted)-1], corrupted[len(corrupted)-2]

	bad := trajectorydiff.Compare(corrupted, actual, opts)
	if bad.Passed {
		t.Fatalf("corrupted sequence must FAIL the gate, got PASS: %s", bad.Summary)
	}
	if bad.FirstDiff == 0 {
		t.Fatalf("failing result must name a first diff, got FirstDiff=0: %+v", bad)
	}
	if !strings.Contains(bad.Summary, "FAIL") {
		t.Fatalf("summary should carry the FAIL verdict: %q", bad.Summary)
	}
	hasUnmatched := false
	for _, step := range bad.Steps {
		if !step.Matched {
			hasUnmatched = true
			break
		}
	}
	if !hasUnmatched {
		t.Fatalf("failing result must carry at least one unmatched StepDiff: %+v", bad.Steps)
	}

	// Restore: the pristine entry order against the same actual
	// sequence is a PASS -- the drill corrupted nothing persistent.
	good := trajectorydiff.Compare(golden, actual, opts)
	if !good.Passed {
		t.Fatalf("restored fixture must PASS again: %s", good.Summary)
	}
}

// TestTrajectoryGoldenFixtureDispatch authors the committed fixture into
// the repo (e2e/fixtures/golden/<fixtureName>.golden.json) when driven
// with MEEPT_TRAJECTORY_GOLDEN_OUT set -- the leaf-03 one-shot authoring
// path; any ordinary test run (including make e2e-fast) skips it.
func TestTrajectoryGoldenFixtureDispatch(t *testing.T) {
	outDir := os.Getenv("MEEPT_TRAJECTORY_GOLDEN_OUT")
	if outDir == "" {
		t.Skip("fixture authoring dispatch: MEEPT_TRAJECTORY_GOLDEN_OUT not set (ordinary test run)")
	}
	wrote, err := harness.CaptureGolden(t, harness.CaptureOptions{
		ScenarioName: fixtureName,
		GoldenDir:    outDir,
		Script:       trajectoryGoldenBasicScript,
		Match:        trajectorydiff.Options{Mode: trajectorydiff.MatchInOrder, Threshold: 1.0},
	})
	if err != nil {
		t.Fatalf("author committed fixture: %v", err)
	}
	t.Logf("authored %s", wrote)
}
