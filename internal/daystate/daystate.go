// Package daystate computes an employee's workload "day state" from a
// flat Signals struct and maps it to a tone line. Pure stdlib: no I/O,
// no model calls, no network — the daemon composite source (internal/daemon
// awareness_wiring.go) gathers the signals over the ledger / goal /
// voice-call / activity stores and feeds Compute + Tone into the moment
// renderer (plan 20261110-situational, master.md frozen contract).
package daystate

import "context"

// Signals is the frozen contract shape: the three ladder fields plus
// context fields that NEVER move the state (Compute is a pure function
// of OpenCommitments, Overdue, BlockedGoals — pinned by gate §A).
type Signals struct {
	OpenCommitments, Overdue, ActiveGoals, BlockedGoals int
	CallsToday, CallsFailedToday, ActivityItemsToday    int
}

// State is the four-rung day-state ladder; the string values are the
// frozen wire tokens ("" downstream means "unavailable", gate §A).
type State string

const (
	StateClear      State = "clear"
	StateBusy       State = "busy"
	StateBehind     State = "behind"
	StateOverloaded State = "overloaded"
)

// Frozen ladder thresholds (master.md: "owner-tunable constants,
// FROZEN here"). The truth table in daystate_test.go pins every
// boundary these numbers encode.
const (
	// OverloadedOverdueMin: >= this many overdue commitments -> overloaded.
	OverloadedOverdueMin = 5
	// OverloadedBlockedGoalsMin: >= this many blocked goals -> overloaded.
	OverloadedBlockedGoalsMin = 2
	// OverloadedOpenCommitmentsMin: >= this many open -> overloaded.
	OverloadedOpenCommitmentsMin = 12

	// BehindOverdueMin: >= this many overdue (short of overloaded) -> behind.
	BehindOverdueMin = 1
	// BehindBlockedGoalsMin: >= this many blocked (short of overloaded) -> behind.
	BehindBlockedGoalsMin = 1
	// BehindOpenCommitmentsMin: > 6 open (i.e. >= 7, short of overloaded) -> behind.
	BehindOpenCommitmentsMin = 7

	// BusyOpenCommitmentsMin: >= this many open, clean -> busy.
	// (busy band is BusyOpenCommitmentsMin..BehindOpenCommitmentsMin-1.)
	BusyOpenCommitmentsMin = 3
	// ClearOpenCommitmentsMax: <= this many open, clean -> clear.
	ClearOpenCommitmentsMax = 2
)

// Compute runs the deterministic ladder. Priority: the overloaded ORs
// win over everything; then the residual behind case (Overdue>=1 ||
// BlockedGoals>=1 || OpenCommitments>6); then the clean busy band
// (3..6 open); otherwise clear (<=2 open, nothing overdue/blocked).
// ActiveGoals, CallsToday, CallsFailedToday and ActivityItemsToday
// never move the ladder.
func Compute(s Signals) State {
	switch {
	case s.Overdue >= OverloadedOverdueMin ||
		s.BlockedGoals >= OverloadedBlockedGoalsMin ||
		s.OpenCommitments >= OverloadedOpenCommitmentsMin:
		return StateOverloaded
	case s.Overdue >= BehindOverdueMin ||
		s.BlockedGoals >= BehindBlockedGoalsMin ||
		s.OpenCommitments >= BehindOpenCommitmentsMin:
		return StateBehind
	case s.OpenCommitments >= BusyOpenCommitmentsMin:
		return StateBusy
	default:
		return StateClear
	}
}

// builtinTone is the frozen built-in tone line per state (gate §B,
// byte-exact).
var builtinTone = map[State]string{
	StateClear:      "Steady day. Proactively offer next steps.",
	StateBusy:       "Loaded but clean. Prioritize; batch small asks.",
	StateBehind:     "Keep it terse; surface blockers first.",
	StateOverloaded: "Escalate: name the top 3 overdue items and who owes them.",
}

// Tone returns the persona override for st when the map names it (keys
// are the State wire strings — BotDefinition.PersonaDayState is
// map[string]string), else the built-in line. An unknown state has no
// built-in and yields "" (the moment renderer omits the tone suffix on
// "").
func Tone(st State, persona map[string]string) string {
	if persona != nil {
		if line, ok := persona[string(st)]; ok && line != "" {
			return line
		}
	}
	return builtinTone[st]
}

// Source is the frozen daemon seam: one call gathers the signals for an
// employee. The composite implementation in internal/daemon is
// read-only and bounded (2s budget per underlying source, fail-soft per
// source; master D-contract: daystate compute NEVER blocks a turn).
type Source interface {
	Signals(ctx context.Context, employeeID string) (Signals, error)
}
