// report.go renders a HitlRegimeReport to JSON + a markdown summary
// (leaf T4). Every rendered cell traces to a Metrics computation - the
// renderer fabricates nothing: it formats the C7 struct's own fields, and
// cells that would require a number the metrics layer never produced render
// as "-" instead of a plausible zero.
package hitl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReportFileName / SummaryFileName are the renderer's output file names
// inside the caller-provided directory.
const (
	ReportFileName  = "hitl-regime-report.json"
	SummaryFileName = "hitl-regime-summary.md"
)

// ---------------------------------------------------------------------------
// JSON rendering
// ---------------------------------------------------------------------------

// RenderJSON serializes the report. The bytes round-trip into
// HitlRegimeReport with identical field names (pinned by report_test.go).
func RenderJSON(r HitlRegimeReport) ([]byte, error) {
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = time.Now().UTC()
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("hitl: render report json: %w", err)
	}
	return append(b, '\n'), nil
}

// ---------------------------------------------------------------------------
// Markdown summary
// ---------------------------------------------------------------------------

// MarkdownSummary renders the per-regime table, one row per regime (all
// rows share the scenario when the report carries a single scenario; mixed
// scenarios label the row). Wall-clock is marked environment-relative per
// the leaf Notes: comparative across regimes in the SAME run, never
// absolute.
func MarkdownSummary(r HitlRegimeReport) string {
	var sb strings.Builder
	sb.WriteString("# HITL regime evaluation report\n\n")
	if !r.GeneratedAt.IsZero() {
		fmt.Fprintf(&sb, "generated: %s\n\n", r.GeneratedAt.UTC().Format(time.RFC3339))
	}
	sb.WriteString("wall_clock_seconds is environment-relative: compare across regimes in the same run, never against other runs.\n\n")

	// One row per regime (C7). Reports carrying multiple scenarios get a
	// scenario column; single-scenario reports label the scenario in the
	// header.
	scenarios := scenarioSet(r)
	multi := len(scenarios) > 1

	sb.WriteString("| regime | scenario | completion_rate | wall_clock_s | interrupt_count | intervention_precision | escape_rate (by risk class) |\n")
	sb.WriteString("|--------|----------|-----------------|--------------|-----------------|------------------------|-----------------------------|\n")
	for i := range r.Scenarios {
		m := r.Scenarios[i]
		scenarioCell := strings.Join(scenarios, ", ")
		if multi {
			scenarioCell = m.ScenarioID
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %d | %s | %s |\n",
			m.Regime,
			scenarioCell,
			rateCell(m.CompletionRate, m.TerminalRecorded),
			wallClockCell(m.WallClockSeconds),
			m.InterruptCount,
			precisionCell(m),
			escapeCell(m.EscapeRate),
		)
	}

	// Supporting detail: precision counting basis, timeout share, and the
	// always-allow split (R0 B.3: report precision and escape rate
	// TOGETHER; either alone is gameable).
	sb.WriteString("\n## detail\n\n")
	sb.WriteString("| regime | interrupts (changed / total) | timed-out interrupts | escapes incl. always | escapes excl. always |\n")
	sb.WriteString("|--------|------------------------------|----------------------|----------------------|----------------------|\n")
	for i := range r.Scenarios {
		m := r.Scenarios[i]
		fmt.Fprintf(&sb, "| %s | %d / %d | %d | %s | %s |\n",
			m.Regime,
			m.InterruptsChangedOutcome, m.InterruptsTotal,
			m.TimedOutInterrupts,
			escapeCell(m.EscapeRateIncludesAlways),
			escapeCell(m.EscapeRateExcludesAlways),
		)
	}

	fmt.Fprintf(&sb, "\nescape oracle: %s\n\n", oracleLabel(r))
	sb.WriteString("metric provenance: " + provenanceLabel(r) + "\n")
	return sb.String()
}

// scenarioSet returns the sorted distinct scenario ids in the report.
func scenarioSet(r HitlRegimeReport) []string {
	seen := map[string]bool{}
	out := []string{}
	for i := range r.Scenarios {
		id := r.Scenarios[i].ScenarioID
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// rateCell renders completion rate; "-" when no terminal was recorded (the
// attempt never closed - rendering 0.0 would fabricate a measurement).
func rateCell(v float64, terminalRecorded bool) string {
	if !terminalRecorded {
		return "-"
	}
	return fmt.Sprintf("%.3f", v)
}

// wallClockCell renders the environment-relative duration.
func wallClockCell(v float64) string {
	return fmt.Sprintf("%.3f", v)
}

// precisionCell renders intervention precision with its counting basis:
// total == 0 renders "n/a (0 interventions)" (R0 B.3: 0 interventions over
// a run is a red flag, not success - the cell names the absence instead of
// a fake 1.0).
func precisionCell(m RegimeMetrics) string {
	if m.InterruptsTotal == 0 {
		return "n/a (0 interventions)"
	}
	return fmt.Sprintf("%.3f (%d/%d)", m.InterventionPrecision, m.InterruptsChangedOutcome, m.InterruptsTotal)
}

// escapeCell renders the per-class map deterministically (sorted keys) or
// "-" when the metrics layer produced no classification.
func escapeCell(m map[string]float64) string {
	if len(m) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.3f", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// oracleLabel returns the report's oracle identity; a mixed report renders
// the per-row set.
func oracleLabel(r HitlRegimeReport) string {
	seen := map[string]bool{}
	out := []string{}
	for i := range r.Scenarios {
		o := r.Scenarios[i].EscapeOracle
		if o != "" && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	if len(out) == 0 {
		return OracleLabel
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}

// provenanceLabel returns the report's provenance caveats.
func provenanceLabel(r HitlRegimeReport) string {
	seen := map[string]bool{}
	out := []string{}
	for i := range r.Scenarios {
		p := r.Scenarios[i].MetricProvenance
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return MetricProvenanceNote
	}
	sort.Strings(out)
	return strings.Join(out, " | ")
}

// ---------------------------------------------------------------------------
// Writers
// ---------------------------------------------------------------------------

// WriteReport writes the JSON report and the markdown summary into dir
// (created if missing). Returns the two written paths. No fabricated
// numbers: both files render from the report struct alone.
func WriteReport(dir string, r HitlRegimeReport) (jsonPath, mdPath string, err error) {
	if dir == "" {
		return "", "", fmt.Errorf("hitl: empty report dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("hitl: create report dir: %w", err)
	}
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = time.Now().UTC()
	}
	jsonPath = filepath.Join(dir, ReportFileName)
	mdPath = filepath.Join(dir, SummaryFileName)

	b, err := RenderJSON(r)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, b, 0o600); err != nil {
		return "", "", fmt.Errorf("hitl: write %s: %w", ReportFileName, err)
	}
	if err := os.WriteFile(mdPath, []byte(MarkdownSummary(r)), 0o600); err != nil {
		return "", "", fmt.Errorf("hitl: write %s: %w", SummaryFileName, err)
	}
	return jsonPath, mdPath, nil
}
