package hitl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reportFixture builds a three-regime report for scenario S with every C7
// field populated (and the supporting detail fields the renderer needs).
func reportFixture() HitlRegimeReport {
	gen := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	mk := func(regime string, precision float64, total int, escape map[string]float64) RegimeMetrics {
		return RegimeMetrics{
			Regime:                   regime,
			ScenarioID:               "S",
			CompletionRate:           0.75,
			WallClockSeconds:         12.5,
			InterruptCount:           total,
			InterventionPrecision:    precision,
			EscapeRate:               escape,
			EscapeRateIncludesAlways: escape,
			EscapeRateExcludesAlways: escape,
			EscapeRateAlwaysActions:  1,
			InterruptsChangedOutcome: 1,
			InterruptsTotal:          total,
			TimedOutInterrupts:       1,
			TerminalRecorded:         true,
			AutonomousActions:        20,
			EscapeOracle:             OracleLabel,
			MetricProvenance:         MetricProvenanceNote,
		}
	}
	return HitlRegimeReport{
		Scenarios: []RegimeMetrics{
			mk(RegimePerAction.String(), 0.25, 4, map[string]float64{}),
			mk(RegimePlanGateLead.String(), 0.5, 2, map[string]float64{}),
			mk(RegimeAutonomousSampled.String(), 0, 0, map[string]float64{"immutable": 0.5, "base_rule": 0.0}),
		},
		GeneratedAt: gen,
	}
}

func TestReportJSONRoundTripsHitlRegimeReport(t *testing.T) {
	r := reportFixture()
	b, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var back HitlRegimeReport
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.Scenarios) != 3 {
		t.Fatalf("decoded %d scenarios, want 3", len(back.Scenarios))
	}
	for i := range r.Scenarios {
		want, got := r.Scenarios[i], back.Scenarios[i]
		if want.Regime != got.Regime || want.ScenarioID != got.ScenarioID {
			t.Fatalf("scenario %d identity drift: %+v vs %+v", i, want, got)
		}
		if !FloatEq(want.CompletionRate, got.CompletionRate) ||
			!FloatEq(want.WallClockSeconds, got.WallClockSeconds) ||
			!FloatEq(want.InterventionPrecision, got.InterventionPrecision) {
			t.Fatalf("scenario %d float drift: %+v vs %+v", i, want, got)
		}
		if want.InterruptCount != got.InterruptCount {
			t.Fatalf("scenario %d interrupt drift: %d vs %d", i, want.InterruptCount, got.InterruptCount)
		}
		for class, v := range want.EscapeRate {
			gv, ok := got.EscapeRate[class]
			if !ok || !FloatEq(v, gv) {
				t.Fatalf("scenario %d escape drift on %s: %v vs %v", i, class, v, gv)
			}
		}
	}
}

func TestReportMarkdownOneRowPerRegime(t *testing.T) {
	md := MarkdownSummary(reportFixture())
	for _, regime := range AllRegimes {
		if !strings.Contains(md, "| "+regime.String()+" |") {
			t.Fatalf("markdown missing a row for regime %s:\n%s", regime, md)
		}
	}
	// Every cell traces to a Metrics computation: the escape values are
	// the fixture's own numbers, formatted.
	if !strings.Contains(md, "immutable=0.500") {
		t.Fatalf("markdown missing the immutable escape cell:\n%s", md)
	}
	if !strings.Contains(md, "0.750") {
		t.Fatalf("markdown missing the completion cell:\n%s", md)
	}
	if !strings.Contains(md, "0.250 (1/4)") {
		t.Fatalf("markdown missing the precision cell with its counting basis:\n%s", md)
	}
	// The header row precedes the regime rows (table shape).
	header := "| regime | scenario | completion_rate |"
	if !strings.Contains(md, header) {
		t.Fatalf("markdown missing the table header:\n%s", md)
	}
	if !strings.Contains(md, "environment-relative") {
		t.Fatal("markdown must mark wall-clock as environment-relative (leaf Notes)")
	}
	if !strings.Contains(md, OracleLabel) || !strings.Contains(md, "meept-defined") {
		t.Fatalf("markdown missing the oracle/provenance caveats:\n%s", md)
	}
}

func TestReportMarkdownHonestEmptyCells(t *testing.T) {
	// A regime run that recorded no terminal, no interventions, and no
	// escape classification must render "-" / "n/a" cells, never plausible
	// zeros.
	r := HitlRegimeReport{Scenarios: []RegimeMetrics{{
		Regime:           RegimeAutonomousSampled.String(),
		ScenarioID:       "S",
		CompletionRate:   0,
		TerminalRecorded: false,
		InterruptsTotal:  0,
		EscapeRate:       map[string]float64{},
	}},
		GeneratedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	md := MarkdownSummary(r)
	if strings.Contains(md, "| autonomous_sampled | S | 0.000") {
		t.Fatalf("unrecorded terminal rendered as a fabricated 0.000:\n%s", md)
	}
	if !strings.Contains(md, "n/a (0 interventions)") {
		t.Fatalf("zero-intervention precision must explain the absence (R0 B.3 red-flag rule):\n%s", md)
	}
	if !strings.Contains(md, " - |") && !strings.Contains(md, "| - |") {
		t.Fatalf("empty escape classification must render as -:\n%s", md)
	}
}

func TestWriteReportJSONAndMarkdownToDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "report-out")
	jsonPath, mdPath, err := WriteReport(dir, reportFixture())
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	if jsonPath != filepath.Join(dir, ReportFileName) || mdPath != filepath.Join(dir, SummaryFileName) {
		t.Fatalf("paths = %s / %s, want the pinned names under %s", jsonPath, mdPath, dir)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var back HitlRegimeReport
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("written json does not round-trip: %v", err)
	}
	if len(back.Scenarios) != 3 {
		t.Fatalf("written json carries %d scenarios, want 3", len(back.Scenarios))
	}
	mdRaw, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	md := string(mdRaw)
	if !strings.HasPrefix(md, "# HITL regime evaluation report") {
		t.Fatalf("markdown missing its title:\n%s", md)
	}
	if !strings.Contains(md, "plan_gate_lead") {
		t.Fatalf("markdown missing the plan gate row:\n%s", md)
	}
}

func TestWriteReportRejectsEmptyDir(t *testing.T) {
	if _, _, err := WriteReport("", reportFixture()); err == nil {
		t.Fatal("empty dir must be refused")
	}
}

func TestMarkdownDeterministicAcrossRenders(t *testing.T) {
	a := MarkdownSummary(reportFixture())
	b := MarkdownSummary(reportFixture())
	if a != b {
		t.Fatalf("markdown not deterministic:\n--- A ---\n%s\n--- B ---\n%s", a, b)
	}
}
