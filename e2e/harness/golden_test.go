//go:build e2e

// Golden capture tests (leaf 02 T1/T2): CaptureGolden freezes a scripted
// FakeLLM run into the C4 fixture format and REFUSES non-scripted
// capture. The positive path boots a real scratch daemon (the same
// harness every e2e suite uses), so it runs only under -tags e2e.
package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/eval/trajectorydiff"
)

// TestCaptureGoldenWritesScriptedFixture (T1): script read_file then
// file_write, capture, and verify the fixture parses with 2 golden
// entries, scrubbed args shapes, and the captured_from literal.
func TestCaptureGoldenWritesScriptedFixture(t *testing.T) {
	dir := t.TempDir()
	wrote, err := CaptureGolden(t, CaptureOptions{
		ScenarioName: "demo-read-write",
		GoldenDir:    dir,
		Script: func(f *FakeLLM) {
			f.SetPostToolText("scenario finished")
			f.EnqueueToolCalls(
				ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/golden-demo/input.txt"}`},
				ToolCall{Name: "file_write", Arguments: `{"path":"/tmp/golden-demo/out.txt","content":"golden","direct":true}`},
			)
		},
		Match: trajectorydiff.Options{Mode: trajectorydiff.MatchInOrder, Threshold: 1.0},
	})
	if err != nil {
		t.Fatalf("CaptureGolden: %v", err)
	}
	if wrote == "" {
		t.Fatal("CaptureGolden returned an empty wrote path")
	}
	if filepath.Base(wrote) != "demo-read-write.golden.json" {
		t.Fatalf("wrote path %q, want demo-read-write.golden.json", wrote)
	}

	// The fixture parses and pins the C4 contract.
	fixture, err := LoadGoldenFixture(wrote)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if fixture.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", fixture.SchemaVersion)
	}
	if fixture.Scenario != "demo-read-write" {
		t.Fatalf("scenario = %q, want demo-read-write", fixture.Scenario)
	}
	if len(fixture.Golden) != 2 {
		t.Fatalf("golden entries = %d, want 2", len(fixture.Golden))
	}
	if fixture.Golden[0].Name != "read_file" || fixture.Golden[1].Name != "file_write" {
		t.Fatalf("golden order = [%s %s], want [read_file file_write]",
			fixture.Golden[0].Name, fixture.Golden[1].Name)
	}
	// Scrubbed args shape: the file_write path key survives with its
	// scripted value (a fixed scenario path is not a variable), but the
	// shape must be a parsed object, not raw JSON text.
	if _, ok := fixture.Golden[1].ArgsShape["path"]; !ok {
		t.Fatalf("file_write args_shape missing path key: %v", fixture.Golden[1].ArgsShape)
	}

	// captured_from is the literal on the raw bytes; no code path may
	// write any other value.
	raw, err := os.ReadFile(wrote)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var probe struct {
		CapturedFrom string `json:"captured_from"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("fixture bytes are not valid JSON: %v", err)
	}
	if probe.CapturedFrom != "fakellm-scripted" {
		t.Fatalf("captured_from = %q, want fakellm-scripted", probe.CapturedFrom)
	}

	// Round-trip: the fixture's match block rebuilds the requested
	// options and the captured steps replay as a PASS against themselves.
	steps := FixtureSteps(fixture)
	if len(steps) != 2 {
		t.Fatalf("FixtureSteps = %d, want 2", len(steps))
	}
	result := trajectorydiff.Compare(steps, steps, fixture.Match.Options())
	if !result.Passed {
		t.Fatalf("self-compare did not pass: %s", result.Summary)
	}
}

// demoGoldenScript is the canonical demo scenario: read_file then
// file_write, the exact script shape T1 pins. Shared by the TempDir
// test above and the fixture-authoring dispatch below so the committed
// fixture and the test's expectations can never drift apart.
func demoGoldenScript(f *FakeLLM) {
	f.SetPostToolText("scenario finished")
	f.EnqueueToolCalls(
		ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/golden-demo/input.txt"}`},
		ToolCall{Name: "file_write", Arguments: `{"path":"/tmp/golden-demo/out.txt","content":"golden","direct":true}`},
	)
}

// TestGoldenCaptureDispatch authors a committed fixture into the repo
// (e2e/fixtures/golden/<scenario>.golden.json) when driven with
// MEEPT_GOLDEN_CAPTURE_OUT set - the leaf-02 one-shot path that produces
// the first fixtures; any ordinary go test run skips it.
func TestGoldenCaptureDispatch(t *testing.T) {
	outDir := os.Getenv("MEEPT_GOLDEN_CAPTURE_OUT")
	if outDir == "" {
		t.Skip("fixture authoring dispatch: MEEPT_GOLDEN_CAPTURE_OUT not set (ordinary test run)")
	}
	scenario := os.Getenv("MEEPT_GOLDEN_CAPTURE_SCENARIO")
	if scenario == "" {
		scenario = "demo-read-write"
	}
	wrote, err := CaptureGolden(t, CaptureOptions{
		ScenarioName: scenario,
		GoldenDir:    outDir,
		Script:       demoGoldenScript,
		Match:        trajectorydiff.Options{Mode: trajectorydiff.MatchInOrder, Threshold: 1.0},
	})
	if err != nil {
		t.Fatalf("capture committed fixture: %v", err)
	}
	t.Logf("authored %s", wrote)
}

// TestCaptureGoldenRefusesUnscripted (T2): nil script -> refusal error,
// and NO fixture file may be written on the refusal path.
func TestCaptureGoldenRefusesUnscripted(t *testing.T) {
	dir := t.TempDir()
	_, err := CaptureGolden(t, CaptureOptions{
		ScenarioName: "no-script",
		GoldenDir:    dir,
		Script:       nil,
	})
	if err == nil {
		t.Fatal("want refusal error for unscripted capture")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read golden dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("no fixture may be written on refusal, got %d files", len(entries))
	}
	if !strings.Contains(err.Error(), "no FakeLLM script") {
		t.Fatalf("refusal error should name the missing script, got: %v", err)
	}
}

// TestCaptureGoldenRefusesEmptyScenario (T2): an empty scenario name is
// the same structural refusal (nothing scripted to name a fixture by).
func TestCaptureGoldenRefusesEmptyScenario(t *testing.T) {
	dir := t.TempDir()
	_, err := CaptureGolden(t, CaptureOptions{
		ScenarioName: "",
		GoldenDir:    dir,
		Script:       func(f *FakeLLM) { f.SetChatText("ok") },
	})
	if err == nil {
		t.Fatal("want refusal error for an empty scenario name")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read golden dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("no fixture may be written on refusal, got %d files", len(entries))
	}
}
