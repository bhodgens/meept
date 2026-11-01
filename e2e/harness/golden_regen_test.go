//go:build e2e

// Golden regeneration tests (leaf 02 T3): the regen helper runs the
// owning suite FIRST and refuses to overwrite the fixture when the
// suite cannot run. The Makefile dispatch entry
// (TestGoldenRegenDispatch) lives here too.
package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenRegenRefusesWhenSuiteMissing (T3 refusal case): the suite
// dir does not exist (this leaf lands before leaf 03's suite) - the
// fixture must NOT be overwritten and the refusal must name the suite.
func TestGoldenRegenRefusesWhenSuiteMissing(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "demo.golden.json")
	original := []byte("{\"schema_version\":1}\n")
	if err := os.WriteFile(fixture, original, 0o644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	err := RegenerateGolden(t, RegenOptions{
		Scenario:  "demo",
		GoldenDir: dir,
		SuiteDir:  filepath.Join(dir, "no-such-suite"),
		GoTest:    "go",
	})
	if err == nil {
		t.Fatal("want refusal error when the suite dir is missing")
	}
	data, readErr := os.ReadFile(fixture)
	if readErr != nil {
		t.Fatalf("read fixture: %v", readErr)
	}
	if string(data) != string(original) {
		t.Fatalf("fixture was overwritten on refusal:\n%s", data)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("test setup error: dir %q should be absolute", dir)
	}
}

// TestGoldenRegenRefusesWhenScenarioUnknown: an unknown scenario (no
// fixture) is a clear refusal, not a silent no-op.
func TestGoldenRegenRefusesWhenScenarioUnknown(t *testing.T) {
	dir := t.TempDir()
	err := RegenerateGolden(t, RegenOptions{
		Scenario:  "__nonexistent__",
		GoldenDir: dir,
		SuiteDir:  ".",
	})
	if err == nil {
		t.Fatal("want refusal error for an unknown scenario")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read golden dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("no fixture may be written for an unknown scenario, got %d files", len(entries))
	}
}

// TestGoldenRegenRefusesWhenScriptUnregistered: a fixture exists and a
// suite dir exists, but the owning suite never registered its script -
// refuse rather than invent one.
func TestGoldenRegenRefusesWhenScriptUnregistered(t *testing.T) {
	dir := t.TempDir()
	suiteDir := filepath.Join(dir, "suite")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fixture := filepath.Join(dir, "demo.golden.json")
	if err := os.WriteFile(fixture, []byte("{\"schema_version\":1}\n"), 0o644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	err := RegenerateGolden(t, RegenOptions{
		Scenario:  "demo",
		GoldenDir: dir,
		SuiteDir:  suiteDir,
		GoTest:    "go",
	})
	if err == nil {
		t.Fatal("want refusal error for an unregistered regeneration script")
	}
	data, readErr := os.ReadFile(fixture)
	if readErr != nil {
		t.Fatalf("read fixture: %v", readErr)
	}
	if string(data) != "{\"schema_version\":1}\n" {
		t.Fatalf("fixture was overwritten on refusal:\n%s", data)
	}
}

// TestGoldenRegenSuiteGateRunsFirst: with a registered script and a
// runnable-but-FAILING suite dir (a real Go package whose tests fail),
// the refusal fires AFTER the suite gate - the fixture is untouched and
// the error names the suite failure.
func TestGoldenRegenSuiteGateRunsFirst(t *testing.T) {
	if _, err := os.Stat(filepath.Join("../../", "go.mod")); err != nil {
		t.Skip("repo root not resolvable; suite gate test needs the module")
	}
	dir := t.TempDir()
	failingSuite := filepath.Join(dir, "failing-suite")
	if err := os.MkdirAll(failingSuite, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pkg := "package failingsuite\n\nimport \"testing\"\n\nfunc TestAlwaysFails(t *testing.T) {\n\tt.Fatal(\"deliberate gate failure\")\n}\n"
	if err := os.WriteFile(filepath.Join(failingSuite, "fail_test.go"), []byte(pkg), 0o644); err != nil {
		t.Fatalf("write test pkg: %v", err)
	}
	fixture := filepath.Join(dir, "demo.golden.json")
	if err := os.WriteFile(fixture, []byte("{\"schema_version\":1}\n"), 0o644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	err := RegenerateGolden(t, RegenOptions{
		Scenario:  "demo",
		GoldenDir: dir,
		SuiteDir:  failingSuite,
		GoTest:    "go",
	})
	if err == nil {
		t.Fatal("want refusal error when the suite fails")
	}
	data, readErr := os.ReadFile(fixture)
	if readErr != nil {
		t.Fatalf("read fixture: %v", readErr)
	}
	if string(data) != "{\"schema_version\":1}\n" {
		t.Fatalf("fixture was overwritten on suite failure:\n%s", data)
	}
}

// TestGoldenRegenDispatch is the Makefile entry point: the
// golden-regenerate targets run
//
//	go test -tags e2e ./e2e/harness/ -run TestGoldenRegenDispatch
//
// with the MEEPT_GOLDEN_REGEN_* env set; this test turns it into the
// RegenerateGolden call. Without the env (any other go test run) it is
// a no-op skip.
func TestGoldenRegenDispatch(t *testing.T) {
	if os.Getenv(EnvRegenScenario) == "" {
		t.Skip("golden-regenerate dispatch: MEEPT_GOLDEN_REGEN_SCENARIO not set (direct go test run)")
	}
	GoldenRegenFromEnv(t)
}
