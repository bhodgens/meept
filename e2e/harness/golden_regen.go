// golden_regen.go - suite-first golden regeneration (leaf 02 T3): the
// investigate-then-regenerate workflow behind `make golden-regenerate`.
// Policy R8: a golden diff is investigated, never blind-regenerated.
//
// Entry points:
//
//	RegenerateGolden(t, opts)  - one scenario, suite-first
//	GoldenRegenFromEnv(t)      - env-driven dispatch the Makefile calls
//	                             through TestGoldenRegenDispatch
//	RegenerateScenarioScript   - the owning suite registers its exact
//	                             CaptureOptions so regen replays the
//	                             IDENTICAL script the suite replays
package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Golden regen env keys (read by GoldenRegenFromEnv, set by the
// Makefile golden-regenerate targets).
const (
	EnvRegenScenario  = "MEEPT_GOLDEN_REGEN_SCENARIO"   // one scenario, or "all"
	EnvRegenGoldenDir = "MEEPT_GOLDEN_REGEN_GOLDEN_DIR" // fixture dir
	EnvRegenSuiteDir  = "MEEPT_GOLDEN_REGEN_SUITE_DIR"  // owning suite package dir
	EnvRegenForce     = "MEEPT_GOLDEN_REGEN_FORCE"      // "1" skips the investigation print
)

// RegenOptions configures RegenerateGolden.
type RegenOptions struct {
	Scenario  string // fixture stem, e.g. trajectory-golden-basic
	GoldenDir string // fixture dir, e.g. e2e/fixtures/golden
	SuiteDir  string // owning e2e suite package dir (the gate)
	Force     bool   // skip the investigation print (NEVER the suite gate)
	GoTest    string // go binary override (default "go"); tests point it at a stub
}

// regenRegistry maps scenario name -> the exact CaptureOptions the
// owning suite captures with. Suites register via
// RegenerateScenarioScript (leaf 03 wiring); an unregistered scenario
// is a refusal, never a guessed script.
var regenRegistry = map[string]CaptureOptions{}

// RegenerateScenarioScript registers the canonical CaptureOptions for a
// scenario. The owning suite calls this so golden-regenerate replays
// the identical script the suite replays - fixture and suite can never
// drift apart by construction.
func RegenerateScenarioScript(opts CaptureOptions) {
	regenRegistry[opts.ScenarioName] = opts
}

// RegenerateGolden re-runs the owning trajectory-golden suite FIRST,
// prints the replay report for investigation, and only then rewrites
// the fixture through a fresh scripted capture. The suite run is
// non-negotiable: when the suite cannot run (missing dir, build or
// test failure) the fixture is NOT overwritten and the returned error
// names the refusal. Force skips only the investigation print - never
// the suite gate.
func RegenerateGolden(t testing.TB, opts RegenOptions) error {
	t.Helper()

	if opts.Scenario == "" {
		return fmt.Errorf("golden-regenerate: no scenario given; usage: make golden-regenerate SCENARIO=<name>")
	}
	fixturePath := filepath.Join(opts.GoldenDir, opts.Scenario+".golden.json")
	if _, err := os.Stat(fixturePath); err != nil {
		return fmt.Errorf("golden-regenerate: no fixture for scenario %q at %s; nothing to regenerate", opts.Scenario, fixturePath)
	}
	if info, err := os.Stat(opts.SuiteDir); err != nil || !info.IsDir() {
		return fmt.Errorf("golden-regenerate: suite dir %s does not exist yet (the trajectory-golden suite owns scenario %q); refusing to regenerate without a runnable suite", opts.SuiteDir, opts.Scenario)
	}
	script, ok := regenRegistry[opts.Scenario]
	if !ok {
		return fmt.Errorf("golden-regenerate: scenario %q has no registered regeneration script; the owning suite must register one via RegenerateScenarioScript", opts.Scenario)
	}

	// 1. Suite first: the suite replays the script and compares against
	// the committed fixture. A red suite means investigate, not regen.
	goBin := opts.GoTest
	if goBin == "" {
		goBin = "go"
	}
	pkg := "./" + filepath.ToSlash(filepath.Clean(opts.SuiteDir))
	cmd := exec.Command(goBin, "test", "-tags", "e2e", "-count=1", "-p", "2", pkg)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Logf("golden-regenerate: suite %s FAILED against the committed golden - investigate before regenerating:\n%s", pkg, out)
		return fmt.Errorf("golden-regenerate: suite %s failed; refusing to regenerate %s (investigate-then-regenerate, never blind-regen)", pkg, fixturePath)
	}
	t.Logf("golden-regenerate: suite %s passed against the committed golden:\n%s", pkg, strings.TrimRight(string(out), "\n"))

	// 2. Investigation print (skipped only by Force): state the intent
	// before any bytes move, so the decision to regenerate is visible
	// in the transcript.
	if !opts.Force {
		t.Logf("golden-regenerate: investigate mode - suite green; recapturing %s from the identical registered script", fixturePath)
	}

	// 3. Recapture: a fresh scripted replay rewrites the fixture.
	// CaptureGolden refuses non-scripted sources by construction, so
	// the regenerated fixture keeps the fakellm-scripted literal.
	script.GoldenDir = opts.GoldenDir
	wrote, captureErr := CaptureGolden(t, script)
	if captureErr != nil {
		return fmt.Errorf("golden-regenerate: recapture %s: %w", fixturePath, captureErr)
	}
	t.Logf("golden-regenerate: rewrote %s from a fresh scripted replay", wrote)
	return nil
}

// GoldenRegenFromEnv reads the Makefile-provided env and regenerates
// one scenario ("all" = every fixture in GoldenDir). Returns the list
// of regenerated fixture paths.
func GoldenRegenFromEnv(t testing.TB) []string {
	t.Helper()
	scenario := os.Getenv(EnvRegenScenario)
	if scenario == "" {
		t.Fatalf("golden-regenerate: %s is empty; usage: make golden-regenerate SCENARIO=<name>", EnvRegenScenario)
	}
	goldenDir := os.Getenv(EnvRegenGoldenDir)
	if goldenDir == "" {
		t.Fatalf("golden-regenerate: %s is empty", EnvRegenGoldenDir)
	}
	suiteDir := os.Getenv(EnvRegenSuiteDir)
	if suiteDir == "" {
		t.Fatalf("golden-regenerate: %s is empty", EnvRegenSuiteDir)
	}
	force := os.Getenv(EnvRegenForce) == "1"

	var scenarios []string
	if scenario == "all" {
		entries, err := os.ReadDir(goldenDir)
		if err != nil {
			t.Fatalf("golden-regenerate: read golden dir %s: %v", goldenDir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if path, lookErr := filepath.Match("*.golden.json", name); lookErr == nil && path {
				scenarios = append(scenarios, strings.TrimSuffix(name, ".golden.json"))
			}
		}
		if len(scenarios) == 0 {
			t.Fatalf("golden-regenerate: no *.golden.json fixtures in %s", goldenDir)
		}
	} else {
		scenarios = []string{scenario}
	}

	var regenerated []string
	for _, name := range scenarios {
		if err := RegenerateGolden(t, RegenOptions{
			Scenario:  name,
			GoldenDir: goldenDir,
			SuiteDir:  suiteDir,
			Force:     force,
		}); err != nil {
			t.Fatalf("%v", err)
		}
		regenerated = append(regenerated, filepath.Join(goldenDir, name+".golden.json"))
	}
	return regenerated
}
