package moment

// 00T-moment gate §1/§2 — plan 20261110-situational leaf 01-moment/00T.
// TESTS ONLY: this file is the executable spec for leaves 01/01 (pkg impl)
// and 01/02 (wiring). RED posture expected: internal/moment does not exist
// yet. internal/weather IS already implemented (sibling gate 03 landed
// d62f6df0), so the *weather.Outlook reference here compiles; the only
// undefined symbols are this package's own (New/Provider/EmployeeDef/
// Snapshot), exactly as the gate intends.
//
// Pinned seams — implementation leaf 01/01 MUST provide EXACTLY these:
//
//	type Snapshot struct {
//	    LocalNow      time.Time
//	    Weekday       string
//	    Label         string
//	    Weather       *weather.Outlook // nil when absent/disabled
//	    WeatherAlerts []string
//	    DayState      string // "clear|busy|behind|overloaded"; "" = unavailable
//	    Tone          string
//	}
//
//	// EmployeeDef mirrors the BotDefinition awareness fields (no bot import)
//	// PLUS the optional situational data the daemon attaches per render
//	// (master: "the moment RENDERER takes optional Weather+DayState inputs").
//	// Snapshot is the frozen contract shape; EmployeeDef's extra data fields
//	// (Weather/WeatherAlerts/DayState/OpenCommitments/Overdue/BlockedGoals/
//	// Tone) are PINNED BY THIS GATE — impl leaf 01/01 implements them as
//	// written here. Tone is pre-computed by the daemon (daystate.Tone with
//	// the persona override map); moment never imports daystate.
//	type EmployeeDef struct {
//	    Timezone        string            // IANA name; "" = defaultLoc below
//	    LocationLabel   string
//	    Weather         *weather.Outlook  // nil = no Weather line
//	    WeatherAlerts   []string          // empty = no "RISK:" suffix
//	    DayState        string            // "" = no Day-state line
//	    OpenCommitments int               // day-state line counts
//	    Overdue         int
//	    BlockedGoals    int
//	    Tone            string            // rendered after the counts parens
//	}
//
//	// Clock seam (house posture: injected func; pin name = "clock"):
//	//   New(clock func() time.Time, defaultLoc *time.Location, logger *slog.Logger) *Provider
//	// nil clock -> time.Now; nil defaultLoc -> UTC; nil logger -> slog.Default.
//	// defaultLoc is the fallback when EmployeeDef.Timezone == "" (scheduler tz
//	// passed by the daemon; gate §1 "pins the fallback arg" = this param).
//	func New(clock func() time.Time, defaultLoc *time.Location, logger *slog.Logger) *Provider
//	func (p *Provider) Render(def *EmployeeDef) string // nil Provider-safe: "" on nil receiver or nil def
//
// Render block format (frozen, master.md contract):
//
//	# Moment
//	It is 14:02 MST, Monday, at Rapid City shop.
//	Weather: 3C falling to -2C, rain 60%. RISK: cold — concrete pour at risk.
//	Day state: behind (7 open, 2 overdue, 1 blocked goal). Keep it terse; surface blockers first.
//
// Lines joined by "\n", NO trailing newline (the PromptBuilder adds section
// separators). Bad timezone name -> UTC fallback + one WARN log line naming
// the bad zone (message wording impl's choice; level WARN is pinned).
// NOTE (contract tension for the orchestrator): Snapshot.Weather pins
// *weather.Outlook, so internal/moment MUST import internal/weather —
// leaf 01/01's "imports stdlib only" checklist line can only mean "no
// bot/agent/config/daystate imports", not literally stdlib-only.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/weather"
)

// gateClock pins 2026-11-09 21:02:00 UTC == 14:02 MST Monday (DST ended
// 2026-11-01, so America/Denver is MST, UTC-7).
var gateClock = time.Date(2026, 11, 9, 21, 2, 0, 0, time.UTC)

func newTestProvider(t *testing.T, h slog.Handler) *Provider {
	t.Helper()
	var logger *slog.Logger
	if h != nil {
		logger = slog.New(h)
	}
	return New(func() time.Time { return gateClock }, nil, logger)
}

// --- §1 Render format + timezone handling -------------------------------

func TestRenderContractGoldenBlock(t *testing.T) {
	p := newTestProvider(t, nil)
	got := p.Render(&EmployeeDef{
		Timezone:        "America/Denver",
		LocationLabel:   "Rapid City shop",
		Weather:         &weather.Outlook{NowC: 3, TodayHighC: 5, TodayLowC: -2, RainProbToday: 60},
		WeatherAlerts:   []string{"cold — concrete pour at risk"},
		DayState:        "behind",
		OpenCommitments: 7,
		Overdue:         2,
		BlockedGoals:    1,
		Tone:            "Keep it terse; surface blockers first.",
	})
	want := strings.Join([]string{
		"# Moment",
		"It is 14:02 MST, Monday, at Rapid City shop.",
		"Weather: 3C falling to -2C, rain 60%. RISK: cold — concrete pour at risk.",
		"Day state: behind (7 open, 2 overdue, 1 blocked goal). Keep it terse; surface blockers first.",
	}, "\n")
	if got != want {
		t.Fatalf("golden block mismatch:\n got: %q\nwant: %q", got, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("Render must not emit a trailing newline, got %q", got)
	}
}

func TestRenderTimeAndWeekday(t *testing.T) {
	p := newTestProvider(t, nil)
	got := p.Render(&EmployeeDef{Timezone: "America/Denver"})
	want := "# Moment\nIt is 14:02 MST, Monday."
	if got != want {
		t.Fatalf("time line mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// Empty Timezone falls back to the defaultLoc constructor arg (scheduler tz
// in production; the daemon-owned fallback chain def.Timezone -> scheduler
// tz -> UTC lives in awareness_wiring, the package only honors the param).
func TestRenderEmptyTimezoneUsesDefaultLoc(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	p := New(func() time.Time { return gateClock }, denver, nil)
	got := p.Render(&EmployeeDef{})
	want := "# Moment\nIt is 14:02 MST, Monday."
	if got != want {
		t.Fatalf("defaultLoc fallback mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// Bad timezone name -> UTC rendering + WARN capture naming the bad zone.
func TestRenderBadTimezoneUTCWithWarn(t *testing.T) {
	cap := &momentCapHandler{}
	p := New(func() time.Time { return gateClock }, nil, slog.New(cap))
	got := p.Render(&EmployeeDef{Timezone: "Mars/Phobos", LocationLabel: "Rapid City shop"})
	want := "# Moment\nIt is 21:02 UTC, Monday, at Rapid City shop."
	if got != want {
		t.Fatalf("bad-tz must render UTC:\n got: %q\nwant: %q", got, want)
	}
	if cap.countLevel(slog.LevelWarn) != 1 {
		t.Fatalf("expected exactly one WARN for the bad zone, got %d lines: %v", cap.countLevel(slog.LevelWarn), cap.lines)
	}
	if !strings.Contains(cap.joined(), "Mars/Phobos") {
		t.Errorf("WARN must name the bad zone, got: %v", cap.lines)
	}
}

// nil-safety: a nil *Provider (the disabled-daemon shape) renders "".
func TestNilProviderSafe(t *testing.T) {
	var p *Provider
	if got := p.Render(&EmployeeDef{Timezone: "America/Denver"}); got != "" {
		t.Errorf("nil Provider Render = %q, want empty string", got)
	}
	nn := newTestProvider(t, nil)
	if got := nn.Render(nil); got != "" {
		t.Errorf("Render(nil def) = %q, want empty string", got)
	}
}

// --- §2 omission rules ---------------------------------------------------

func TestRenderOmissionRules(t *testing.T) {
	tests := []struct {
		name string
		def  *EmployeeDef
		want string
	}{
		{
			// Full-omit: everything nil except time -> exactly two lines.
			name: "time-only two lines",
			def:  &EmployeeDef{Timezone: "America/Denver"},
			want: "# Moment\nIt is 14:02 MST, Monday.",
		},
		{
			// No Label -> no ", at ..." clause (same two-line shape as
			// full-omit: the omission rule pinned separately so a
			// label-dropping regression names itself).
			name: "no label drops at-clause",
			def:  &EmployeeDef{Timezone: "America/Denver", LocationLabel: ""},
			want: "# Moment\nIt is 14:02 MST, Monday.",
		},
		{
			name: "label renders at-clause",
			def:  &EmployeeDef{Timezone: "America/Denver", LocationLabel: "Rapid City shop"},
			want: "# Moment\nIt is 14:02 MST, Monday, at Rapid City shop.",
		},
		{
			// Outlook present, alerts empty -> weather line, NO RISK suffix.
			name: "outlook without alerts omits RISK",
			def: &EmployeeDef{
				Timezone: "America/Denver",
				Weather:  &weather.Outlook{NowC: 3, TodayHighC: 5, TodayLowC: -2, RainProbToday: 60},
			},
			want: "# Moment\nIt is 14:02 MST, Monday.\nWeather: 3C falling to -2C, rain 60%.",
		},
		{
			name: "empty DayState drops state+tone lines",
			def: &EmployeeDef{
				Timezone:        "America/Denver",
				DayState:        "",
				OpenCommitments: 99, // counts must not leak without a state
				Tone:            "should not appear",
			},
			want: "# Moment\nIt is 14:02 MST, Monday.",
		},
		{
			// DayState set with empty Tone -> state line still renders
			// (tone is optional decoration after the counts).
			name: "day state without tone",
			def: &EmployeeDef{
				Timezone: "America/Denver",
				DayState: "busy",
			},
			want: "# Moment\nIt is 14:02 MST, Monday.\nDay state: busy (0 open, 0 overdue, 0 blocked goal).",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, nil)
			got := p.Render(tc.def)
			if got != tc.want {
				t.Fatalf("mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// Stable-absent-behavior pin (master): the block WITHOUT weather/daystate
// is exactly the two/three-line shape — assert no partial leakage of the
// optional sections when their data is absent.
func TestRenderNeverLeavesWeatherOrDayStateWithoutData(t *testing.T) {
	p := newTestProvider(t, nil)
	got := p.Render(&EmployeeDef{Timezone: "America/Denver", LocationLabel: "Shop"})
	if strings.Contains(got, "Weather") || strings.Contains(got, "Day state") || strings.Contains(got, "RISK") {
		t.Fatalf("absent sections leaked into %q", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("full-omit block must be exactly two lines, got %q", got)
	}
}

// Snapshot field-shape pin (frozen contract): construction below must
// compile with EXACTLY these field names/types.
func TestSnapshotFieldShape(t *testing.T) {
	w := &weather.Outlook{NowC: 3, TodayHighC: 5, TodayLowC: -2, WindKph: 55, RainProbToday: 60, Fetched: gateClock}
	s := Snapshot{
		LocalNow:      gateClock,
		Weekday:       "Monday",
		Label:         "Rapid City shop",
		Weather:       w,
		WeatherAlerts: []string{"high wind — roofing work at risk"},
		DayState:      "overloaded",
		Tone:          "Escalate: name the top 3 overdue items and who owes them.",
	}
	if s.Weather.NowC != 3 || s.WeatherAlerts[0] == "" || s.DayState != "overloaded" {
		t.Fatal("Snapshot fields did not round-trip")
	}
}

// momentCapHandler records "LEVEL message" lines (moment- prefixed to avoid
// collisions with sibling gate capture types in the same tree).
type momentCapHandler struct {
	lines []string
}

func (h *momentCapHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *momentCapHandler) Handle(_ context.Context, r slog.Record) error {
	h.lines = append(h.lines, r.Level.String()+" "+r.Message)
	return nil
}

func (h *momentCapHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *momentCapHandler) WithGroup(string) slog.Handler      { return h }

func (h *momentCapHandler) countLevel(l slog.Level) int {
	n := 0
	for _, line := range h.lines {
		if strings.HasPrefix(line, l.String()+" ") {
			n++
		}
	}
	return n
}

func (h *momentCapHandler) joined() string { return strings.Join(h.lines, "\n") }
