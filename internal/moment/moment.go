// Package moment renders the per-employee "# Moment" awareness block — the
// volatile situational section (local time, optional weather + trade risk
// alerts, optional day-state + tone) that the daemon appends to prompts at
// turn-assembly time (plan 20261110-situational, master.md contract).
//
// The package reads NO config and imports neither bot/agent/config/daystate:
// the daemon decides whether to build a Provider and passes all situational
// data through the EmployeeDef argument (01-moment/01 self-check: the only
// non-stdlib import is internal/weather, which the frozen Snapshot shape
// requires — see the gate header note in moment_test.go).
package moment

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/weather"
)

// Snapshot is the frozen contract shape of one rendered moment (master.md;
// pinned by gate §1 TestSnapshotFieldShape). The daemon never has to build
// one — Render derives it internally — but it is exported as the data
// snapshot other components (and tests) can reference.
type Snapshot struct {
	LocalNow      time.Time        // employee-local wall clock
	Weekday       string           // full weekday name ("Monday")
	Label         string           // LocationLabel ('' ok)
	Weather       *weather.Outlook // nil when absent/disabled
	WeatherAlerts []string         // computed thresholds, may be empty
	DayState      string           // "clear|busy|behind|overloaded"; "" = unavailable
	Tone          string           // persona tone line for DayState ('' when none)
}

// EmployeeDef mirrors the BotDefinition awareness fields (no bot import)
// plus the optional situational data the daemon attaches per render. Field
// shapes are PINNED BY THE GATE (moment_test.go header): Weather nil = no
// Weather line, WeatherAlerts empty = no "RISK:" suffix, DayState "" = no
// Day-state line; counts and Tone only render alongside a non-empty
// DayState. Tone is pre-computed by the daemon (daystate.Tone with the
// persona override map); moment never imports daystate.
type EmployeeDef struct {
	Timezone        string // IANA name; "" = defaultLoc below
	LocationLabel   string
	Weather         *weather.Outlook
	WeatherAlerts   []string
	DayState        string
	OpenCommitments int
	Overdue         int
	BlockedGoals    int
	Tone            string
}

// Provider renders moment blocks for employees. It carries the clock seam
// (injected func; nil = time.Now) and the fallback location used when
// EmployeeDef.Timezone is empty (nil defaultLoc = UTC; the full fallback
// chain def.Timezone -> scheduler tz -> UTC lives in the daemon wiring).
// A nil *Provider (the disabled-daemon shape) is render-safe: "" output.
type Provider struct {
	clock      func() time.Time
	defaultLoc *time.Location
	logger     *slog.Logger
}

// New builds a Provider. nil clock -> time.Now; nil defaultLoc -> UTC;
// nil logger -> slog.Default.
func New(clock func() time.Time, defaultLoc *time.Location, logger *slog.Logger) *Provider {
	if clock == nil {
		clock = time.Now
	}
	if defaultLoc == nil {
		defaultLoc = time.UTC
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{clock: clock, defaultLoc: defaultLoc, logger: logger}
}

// Render assembles the frozen block:
//
//	# Moment
//	It is 14:02 MST, Monday, at Rapid City shop.
//	Weather: 3C falling to -2C, rain 60%. RISK: cold — concrete pour at risk.
//	Day state: behind (7 open, 2 overdue, 1 blocked goal). Keep it terse; surface blockers first.
//
// Lines are joined with "\n" with NO trailing newline (the PromptBuilder
// adds section separators). Sections whose data is absent are omitted
// entirely. A bad Timezone name renders UTC and logs exactly one WARN
// naming the bad zone. nil receiver / nil def return "".
func (p *Provider) Render(def *EmployeeDef) string {
	if p == nil || def == nil {
		return ""
	}
	snap := p.snapshot(def)

	lines := []string{"# Moment"}

	timeLine := fmt.Sprintf("It is %s, %s", snap.LocalNow.Format("15:04 MST"), snap.Weekday)
	if snap.Label != "" {
		timeLine += ", at " + snap.Label
	}
	lines = append(lines, timeLine+".")

	if snap.Weather != nil {
		weatherLine := fmt.Sprintf("Weather: %gC falling to %gC, rain %d%%.",
			snap.Weather.NowC, snap.Weather.TodayLowC, snap.Weather.RainProbToday)
		if len(snap.WeatherAlerts) > 0 {
			weatherLine += " RISK: " + strings.Join(snap.WeatherAlerts, "; ") + "."
		}
		lines = append(lines, weatherLine)
	}

	if snap.DayState != "" {
		stateLine := fmt.Sprintf("Day state: %s (%d open, %d overdue, %d blocked goal).",
			snap.DayState, def.OpenCommitments, def.Overdue, def.BlockedGoals)
		if snap.Tone != "" {
			stateLine += " " + snap.Tone
		}
		lines = append(lines, stateLine)
	}

	return strings.Join(lines, "\n")
}

// snapshot derives the frozen Snapshot shape from the def through the
// clock/timezone seam. Pure derivation; no I/O.
func (p *Provider) snapshot(def *EmployeeDef) Snapshot {
	now := p.clock().In(p.resolveLoc(def))
	return Snapshot{
		LocalNow:      now,
		Weekday:       now.Weekday().String(),
		Label:         def.LocationLabel,
		Weather:       def.Weather,
		WeatherAlerts: def.WeatherAlerts,
		DayState:      def.DayState,
		Tone:          def.Tone,
	}
}

// resolveLoc implements the package-side half of the fallback chain:
// def.Timezone -> Provider defaultLoc -> UTC. An unparseable zone name
// falls back to UTC with exactly one WARN naming the bad zone (the level
// is pinned by gate §1; message wording is impl's choice).
func (p *Provider) resolveLoc(def *EmployeeDef) *time.Location {
	if def.Timezone == "" {
		return p.defaultLoc
	}
	loc, err := time.LoadLocation(def.Timezone)
	if err != nil {
		// The bad zone name goes in the MESSAGE (the gate's capture
		// handler records only "LEVEL message"; attrs are not joined).
		p.logger.Warn(fmt.Sprintf("moment: invalid timezone %q, rendering UTC: %v", def.Timezone, err))
		return time.UTC
	}
	return loc
}
