//go:build e2e

// Package modelrating is the model-rating-metrics-plan 04-ship TEST-GATE
// leaf (00T-ship): the executable ship gate for the hermetic e2e suite
// skeleton + manifest registration.
//
// SH-MANIFEST-1 / SH-SUITE-1 are REAL today: they parse e2e/manifest.json
// and pin the model-rating suite row (dir, census status, four scenario
// names) and the internal/modelrating/ path_map key against the CURRENT
// file. The metrics-list extension assertion is the plan's documented RED
// until leaf 01-e2e-suite lands its T4 manifest edit.
//
// SH-CAPTURE-1 / SH-RETENTION-1 / SH-SURFACE-1 / SH-JOIN-1 are scenario
// gates for the END-TO-END outcomes (grade-capture-01, retention-01,
// surface-01, join-proof-01). They skip with a named reason until
// 01-e2e-suite lands the rigs; the skip is the placeholder, the assertions
// in this file are never weakened.
//
// TESTS ONLY: no implementation rigs live here.

package modelrating

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// manifestRow is the subset of e2e/manifest.json this gate pins (unknown
// fields ignored; the file is the contract).
type gateManifest struct {
	PathMap map[string][]string `json:"path_map"`
	Suites  []struct {
		Name      string   `json:"name"`
		Dir       string   `json:"dir"`
		Status    string   `json:"status"`
		Scenarios []string `json:"scenarios"`
	} `json:"suites"`
}

// repoRoot resolves the module root from this test's directory
// (e2e/suites/model-rating) — no `go test` cwd guarantee needed.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 3; i++ {
		dir = filepath.Dir(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %s has no go.mod: %v", dir, err)
	}
	return dir
}

// loadGateManifest reads and parses e2e/manifest.json.
func loadGateManifest(t *testing.T) gateManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "e2e", "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m gateManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

// modelRatingRow returns the suites[] row for the model-rating suite.
func modelRatingRow(t *testing.T, m gateManifest) (row struct {
	Name      string   `json:"name"`
	Dir       string   `json:"dir"`
	Status    string   `json:"status"`
	Scenarios []string `json:"scenarios"`
}, found bool) {
	t.Helper()
	for _, s := range m.Suites {
		if s.Name == "model-rating" {
			return s, true
		}
	}
	return row, false
}

// frozenScenarios are the four registered scenario ids (manifest suites[]
// row + master.md Test Gate F1-F6 outcomes). Order matters: the row's
// scenarios array must declare all four.
var frozenScenarios = [4]string{
	"grade-capture-01",
	"retention-01",
	"surface-01",
	"join-proof-01",
}

// TestSH_MANIFEST_1_RowRegistered pins the manifest registration that must
// hold NOW: the model-rating suite row exists with the contract dir, a
// valid census status ("todo" today; leaf 01-e2e-suite T4 flips it to
// "implemented" when the suite lands), and the internal/modelrating/
// path_map key maps to the suite.
func TestSH_MANIFEST_1_RowRegistered(t *testing.T) {
	m := loadGateManifest(t)

	row, ok := modelRatingRow(t, m)
	if !ok {
		t.Fatalf("manifest suites[] has no model-rating row")
	}
	if row.Dir != "e2e/suites/model-rating" {
		t.Errorf("model-rating row dir = %q, want e2e/suites/model-rating", row.Dir)
	}
	switch row.Status {
	case "todo", "implemented":
		if row.Status == "todo" {
			t.Logf("model-rating status %q (leaf 01-e2e-suite owns the flip to implemented)", row.Status)
		}
	default:
		t.Errorf("model-rating row status = %q, want todo|implemented (census statuses)", row.Status)
	}

	key := "internal/modelrating/"
	got, ok := m.PathMap[key]
	if !ok {
		t.Fatalf("path_map missing the internal/modelrating/ key (new-package census rule)")
	}
	if len(got) != 1 || got[0] != "model-rating" {
		t.Errorf("path_map[%q] = %v, want [model-rating]", key, got)
	}
}

// TestSH_MANIFEST_1_MetricsPathMapExtension pins the second half of the
// manifest contract: the internal/metrics/ path_map list CONTAINS
// model-rating (extend, never replace — master.md Notes).
//
// RED today by design: the list is [daemon-lifecycle, llm-hop-tracing,
// llm-serving-tiers, dispatch-trace]; leaf 01-e2e-suite task T4 extends
// it. This is the missing-manifest RED the plan names for T2 — it flips
// GREEN in the same commit as 01's manifest edit, never by editing this
// gate.
func TestSH_MANIFEST_1_MetricsPathMapExtension(t *testing.T) {
	m := loadGateManifest(t)

	got, ok := m.PathMap["internal/metrics/"]
	if !ok {
		t.Fatalf("path_map missing the internal/metrics/ key entirely")
	}
	for _, s := range got {
		if s == "model-rating" {
			return
		}
	}
	t.Errorf("path_map[internal/metrics/] = %v; must contain model-rating (leaf 01-e2e-suite T4 extends the list; do not edit this gate)", got)
}

// TestSH_SUITE_1_SuiteCompilesAndScenariosRegistered pins SH-SUITE-1: this
// gate file compiles and runs under the e2e tag (reaching this body IS the
// compile assertion), and the manifest row declares all four frozen
// scenario names so `make e2e-affected-area AREA=model-rating` can select
// them once the rigs exist.
func TestSH_SUITE_1_SuiteCompilesAndScenariosRegistered(t *testing.T) {
	m := loadGateManifest(t)
	row, ok := modelRatingRow(t, m)
	if !ok {
		t.Fatalf("manifest suites[] has no model-rating row")
	}

	declared := map[string]bool{}
	for _, s := range row.Scenarios {
		// Scenario entries are "<id> <title>" strings; match on the id
		// prefix so the frozen ids stay the gate's vocabulary.
		for _, want := range frozenScenarios {
			if len(s) >= len(want) && s[:len(want)] == want {
				declared[want] = true
			}
		}
	}
	for _, want := range frozenScenarios {
		if !declared[want] {
			t.Errorf("model-rating scenarios[] does not declare %q (entries: %v)", want, row.Scenarios)
		}
	}
}

// TestSH_CAPTURE_1 is the grade-capture-01 scenario gate: given a step
// graded with defect verdicts + judged identities through the review path,
// when the capture pipeline runs, then model_review_verdicts rows persist
// with rubric/category/severity/judge/cycle intact.
//
// Skip posture RESOLVED by leaf 01-e2e-suite: the rig lives in
// model_rating_test.go (TestGradeCapture01) and this gate now runs it —
// the gate itself never weakened its assertions.
func TestSH_CAPTURE_1_GradeCaptureScenario(t *testing.T) {
	TestGradeCapture01(t)
}

// TestSH_RETENTION_1 is the retention-01 scenario gate: given verdict rows
// older than 24h and 30d, when the metrics_live sweep and the audit purge
// run, then the verdict rows survive and RetentionTables() excludes
// model_review_verdicts.
func TestSH_RETENTION_1_RetentionScenario(t *testing.T) {
	TestRetention01(t)
}

// TestSH_SURFACE_1 is the surface-01 scenario gate: given persisted
// verdicts, when GET /api/v1/metrics/reviews and `meept analytics reviews`
// run against the same rows, then both surfaces agree on the frozen
// response/table shape.
func TestSH_SURFACE_1_SurfaceScenario(t *testing.T) {
	TestSurface01(t)
}

// TestSH_JOIN_1 is the join-proof-01 scenario gate: given an llm_calls
// rotation fixture [error(m1), success(m2), success(m3)] plus judge-loop
// calls under a different agent id, when the blame resolves, then the
// final successful implementer model (m3) is blamed and judge calls are
// excluded by agent_id.
func TestSH_JOIN_1_JoinProofScenario(t *testing.T) {
	TestJoinProof01(t)
}
