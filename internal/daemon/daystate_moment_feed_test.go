package daemon

// 02-daystate/00T §F — moment day-state feed test gate (NEW file).
//
// Pins the daemon-glue contract (leaf 02/02 owns awareness_wiring.go):
// daystate.Compute -> daystate.Tone(persona) -> moment EmployeeDef
// fields -> moment.Provider.Render emits the day-state line verbatim in
// the FROZEN format:
//
//	Day state: behind (7 open, 2 overdue, 1 blocked goal). Keep it terse; surface blockers first.
//
// and omits the line entirely when the state is unavailable
// (all-sources-fail posture, master §Signals-source).
//
// CROSS-BANCHERED SYMBOLS (owned by sibling gate 01-moment, expected RED
// here until 01/01-02 land): moment.New, moment.Provider.Render,
// moment.EmployeeDef{Timezone, DayState, OpenCommitments, Overdue,
// BlockedGoals, Tone}. Their shapes are pinned by
// internal/moment/moment_test.go (the sibling's gate file); this test
// consumes them through the public surface only, never moment-internal
// seams. daystate.Compute/Tone/Signals are same-branch (02/02).

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/daystate"
	"github.com/caimlas/meept/internal/moment"
)

// feedClock pins 2026-11-09 21:02:00 UTC == 14:02 MST, Monday (matches
// the sibling gate's clock; Denver DST ended 2026-11-01).
var feedClock = time.Date(2026, 11, 9, 21, 2, 0, 0, time.UTC)

// renderDayState drives the frozen chain: Signals -> Compute -> Tone ->
// EmployeeDef -> Render, and returns the block.
func renderDayState(t *testing.T, sigs daystate.Signals, persona map[string]string) string {
	t.Helper()
	p := moment.New(func() time.Time { return feedClock }, nil, nil)
	st := daystate.Compute(sigs)
	def := &moment.EmployeeDef{
		Timezone:        "America/Denver",
		DayState:        string(st),
		OpenCommitments: sigs.OpenCommitments,
		Overdue:         sigs.Overdue,
		BlockedGoals:    sigs.BlockedGoals,
		Tone:            daystate.Tone(st, persona),
	}
	return p.Render(def)
}

// TestFeedDayStateLineContractFormat — behind case with the contract's
// exact numbers (7 open, 2 overdue, 1 blocked goal): the rendered block
// ends with the frozen day-state line, byte-exact.
func TestFeedDayStateLineContractFormat(t *testing.T) {
	sigs := daystate.Signals{OpenCommitments: 7, Overdue: 2, BlockedGoals: 1}
	got := renderDayState(t, sigs, nil)

	wantLine := "Day state: behind (7 open, 2 overdue, 1 blocked goal). Keep it terse; surface blockers first."
	if !strings.Contains(got, wantLine) {
		t.Fatalf("rendered block missing frozen day-state line:\n got: %q\nwant contains: %q", got, wantLine)
	}
	lines := strings.Split(got, "\n")
	if lines[len(lines)-1] != wantLine {
		t.Errorf("day-state line must be the final line; got lines %q", lines)
	}
	if !strings.HasPrefix(got, "# Moment\n") {
		t.Errorf("block must open with \"# Moment\" header; got %q", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("Render must not emit a trailing newline; got %q", got)
	}
}

// TestFeedDayStatePersonaTone — persona override flows through the feed:
// the state stays computed, the tone is the persona line.
func TestFeedDayStatePersonaTone(t *testing.T) {
	sigs := daystate.Signals{OpenCommitments: 12, Overdue: 0, BlockedGoals: 0}
	persona := map[string]string{"overloaded": "Name the debt, then stop talking."}
	got := renderDayState(t, sigs, persona)

	wantLine := "Day state: overloaded (12 open, 0 overdue, 0 blocked goal). Name the debt, then stop talking."
	if !strings.Contains(got, wantLine) {
		t.Fatalf("persona tone not rendered:\n got: %q\nwant contains: %q", got, wantLine)
	}
}

// TestFeedDayStateOmission — unavailable posture: empty DayState (the
// zero-Signals-with-all-sources-fail glue writes "") -> NO day-state
// line and no tone text in the block.
func TestFeedDayStateOmission(t *testing.T) {
	p := moment.New(func() time.Time { return feedClock }, nil, nil)
	got := p.Render(&moment.EmployeeDef{
		Timezone: "America/Denver",
		DayState: "",
		// stale counts must not leak without a state:
		OpenCommitments: 42,
		Tone:            "should not appear",
	})
	if strings.Contains(got, "Day state") || strings.Contains(got, "should not appear") {
		t.Fatalf("day-state section leaked with empty DayState: %q", got)
	}
	if !strings.HasPrefix(got, "# Moment\nIt is 14:02 MST, Monday.") {
		t.Errorf("time-only shape broken: %q", got)
	}
}

// TestFeedZeroSignalsClearShape — healthy-but-empty day: Compute(zero)
// is clear, and the line carries all-zero counts (state available,
// distinct from the omission posture above).
func TestFeedZeroSignalsClearShape(t *testing.T) {
	got := renderDayState(t, daystate.Signals{}, nil)
	wantLine := "Day state: clear (0 open, 0 overdue, 0 blocked goal). Steady day. Proactively offer next steps."
	if !strings.Contains(got, wantLine) {
		t.Fatalf("zero-signals clear line mismatch:\n got: %q\nwant contains: %q", got, wantLine)
	}
}
