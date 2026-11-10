// Package daystate — 02-daystate/00T test gate (§A Compute ladder truth
// table, §B Tone built-ins + persona override).
//
// FROZEN contract (master.md + 00T-daystate.md, verbatim):
//
//	type Signals struct {
//	    OpenCommitments, Overdue, ActiveGoals, BlockedGoals int
//	    CallsToday, CallsFailedToday, ActivityItemsToday    int
//	}
//	type State string
//	const (StateClear State="clear"; StateBusy State="busy"; StateBehind="behind"; StateOverloaded="overloaded")
//	// overloaded: Overdue>=5 || BlockedGoals>=2 || OpenCommitments>=12
//	// behind:     !(state below busy) — i.e. Overdue>=1 || OpenCommitments>6 (residual case)
//	// busy:       OpenCommitments in 3..6 && Overdue==0 && BlockedGoals==0
//	// clear:      OpenCommitments<=2 && Overdue==0 && BlockedGoals==0
//	func Compute(s Signals) State
//	func Tone(st State, persona map[string]string) string
//	type Source interface { Signals(ctx context.Context, employeeID string) (Signals, error) }
//
// Compute is a PURE function of (OpenCommitments, Overdue, BlockedGoals):
// ActiveGoals, CallsToday, CallsFailedToday, ActivityItemsToday never
// move the ladder (pinned by the noise cases below). No I/O, no LLM.
package daystate

import (
	"context"
	"testing"
)

// §A — Compute ladder truth table over the frozen boundary numbers.
func TestComputeTruthTable(t *testing.T) {
	cases := []struct {
		name string
		s    Signals
		want State
	}{
		// clear: OpenCommitments<=2 && Overdue==0 && BlockedGoals==0
		{"clear_zero", Signals{OpenCommitments: 0, Overdue: 0, BlockedGoals: 0}, StateClear},
		{"clear_one", Signals{OpenCommitments: 1, Overdue: 0, BlockedGoals: 0}, StateClear},
		{"clear_two_boundary", Signals{OpenCommitments: 2, Overdue: 0, BlockedGoals: 0}, StateClear},

		// busy: OpenCommitments in 3..6 && Overdue==0 && BlockedGoals==0
		{"busy_three_boundary", Signals{OpenCommitments: 3, Overdue: 0, BlockedGoals: 0}, StateBusy},
		{"busy_four", Signals{OpenCommitments: 4, Overdue: 0, BlockedGoals: 0}, StateBusy},
		{"busy_six_boundary", Signals{OpenCommitments: 6, Overdue: 0, BlockedGoals: 0}, StateBusy},

		// behind (residual): Overdue>=1 || OpenCommitments>6, short of overloaded
		{"behind_open_seven", Signals{OpenCommitments: 7, Overdue: 0, BlockedGoals: 0}, StateBehind},
		{"behind_open_eleven_boundary", Signals{OpenCommitments: 11, Overdue: 0, BlockedGoals: 0}, StateBehind},
		{"behind_overdue_one_from_clear", Signals{OpenCommitments: 0, Overdue: 1, BlockedGoals: 0}, StateBehind},
		{"behind_overdue_one_in_busy_band", Signals{OpenCommitments: 3, Overdue: 1, BlockedGoals: 0}, StateBehind},
		{"behind_overdue_one_at_six", Signals{OpenCommitments: 6, Overdue: 1, BlockedGoals: 0}, StateBehind},
		{"behind_overdue_four_boundary", Signals{OpenCommitments: 2, Overdue: 4, BlockedGoals: 0}, StateBehind},
		{"behind_blocked_one_boundary", Signals{OpenCommitments: 2, Overdue: 0, BlockedGoals: 1}, StateBehind},
		{"behind_blocked_one_in_busy_band", Signals{OpenCommitments: 5, Overdue: 0, BlockedGoals: 1}, StateBehind},

		// overloaded: Overdue>=5 || BlockedGoals>=2 || OpenCommitments>=12
		{"overloaded_overdue_five_boundary", Signals{OpenCommitments: 0, Overdue: 5, BlockedGoals: 0}, StateOverloaded},
		{"overloaded_overdue_six", Signals{OpenCommitments: 3, Overdue: 6, BlockedGoals: 0}, StateOverloaded},
		{"overloaded_blocked_two_boundary", Signals{OpenCommitments: 0, Overdue: 0, BlockedGoals: 2}, StateOverloaded},
		{"overloaded_blocked_three", Signals{OpenCommitments: 1, Overdue: 0, BlockedGoals: 3}, StateOverloaded},
		{"overloaded_open_twelve_boundary", Signals{OpenCommitments: 12, Overdue: 0, BlockedGoals: 0}, StateOverloaded},
		{"overloaded_open_huge", Signals{OpenCommitments: 100, Overdue: 0, BlockedGoals: 0}, StateOverloaded},
		{"overloaded_all_three_ors", Signals{OpenCommitments: 12, Overdue: 5, BlockedGoals: 2}, StateOverloaded},

		// Priority pin: any overloaded OR wins over behind/busy/clear.
		{"overloaded_wins_over_behind", Signals{OpenCommitments: 12, Overdue: 1, BlockedGoals: 1}, StateOverloaded},

		// The state is a pure function of the three ladder fields: the
		// remaining Signals fields must NEVER move it.
		{"noise_clear", Signals{OpenCommitments: 2, Overdue: 0, BlockedGoals: 0,
			ActiveGoals: 50, CallsToday: 99, CallsFailedToday: 40, ActivityItemsToday: 77}, StateClear},
		{"noise_busy", Signals{OpenCommitments: 4, Overdue: 0, BlockedGoals: 0,
			ActiveGoals: 50, CallsToday: 99, CallsFailedToday: 40, ActivityItemsToday: 77}, StateBusy},
		{"noise_behind", Signals{OpenCommitments: 8, Overdue: 0, BlockedGoals: 0,
			ActiveGoals: 50, CallsToday: 99, CallsFailedToday: 40, ActivityItemsToday: 77}, StateBehind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compute(tc.s); got != tc.want {
				t.Errorf("Compute(%+v) = %q; want %q", tc.s, got, tc.want)
			}
		})
	}
}

// §A — the State constant VALUES are the frozen wire strings (” means
// unavailable downstream; the four states are these exact bytes).
func TestStateConstValues(t *testing.T) {
	if string(StateClear) != "clear" {
		t.Errorf("StateClear = %q; want \"clear\"", string(StateClear))
	}
	if string(StateBusy) != "busy" {
		t.Errorf("StateBusy = %q; want \"busy\"", string(StateBusy))
	}
	if string(StateBehind) != "behind" {
		t.Errorf("StateBehind = %q; want \"behind\"", string(StateBehind))
	}
	if string(StateOverloaded) != "overloaded" {
		t.Errorf("StateOverloaded = %q; want \"overloaded\"", string(StateOverloaded))
	}
}

// §B — Tone: the four built-in lines are byte-exact per the frozen
// contract.
func TestToneBuiltins(t *testing.T) {
	builtins := map[State]string{
		StateClear:      "Steady day. Proactively offer next steps.",
		StateBusy:       "Loaded but clean. Prioritize; batch small asks.",
		StateBehind:     "Keep it terse; surface blockers first.",
		StateOverloaded: "Escalate: name the top 3 overdue items and who owes them.",
	}
	for st, want := range builtins {
		if got := Tone(st, nil); got != want {
			t.Errorf("Tone(%q, nil) = %q; want %q", st, got, want)
		}
	}
}

// §B — persona override wins for the state it names; every other state
// falls back to its built-in line. Keys are the State wire strings
// (BotDefinition.PersonaDayState is map[string]string).
func TestTonePersonaOverride(t *testing.T) {
	persona := map[string]string{"behind": "Boss mode: terse, blockers first."}
	if got := Tone(StateBehind, persona); got != "Boss mode: terse, blockers first." {
		t.Errorf("persona override did not win: Tone(behind, persona) = %q", got)
	}
	if got := Tone(StateClear, persona); got != "Steady day. Proactively offer next steps." {
		t.Errorf("non-overridden state lost its built-in: Tone(clear, persona) = %q", got)
	}
	if got := Tone(StateBusy, persona); got != "Loaded but clean. Prioritize; batch small asks." {
		t.Errorf("non-overridden state lost its built-in: Tone(busy, persona) = %q", got)
	}
	if got := Tone(StateOverloaded, persona); got != "Escalate: name the top 3 overdue items and who owes them." {
		t.Errorf("non-overridden state lost its built-in: Tone(overloaded, persona) = %q", got)
	}
}

// §A/§B — the Source interface is the frozen daemon seam shape:
// Signals(ctx, employeeID) (Signals, error). Compile-level pin only; the
// daemon composite implementation is gated in internal/daemon (leaf §E).
type sourceShapeCheck struct{}

func (sourceShapeCheck) Signals(_ context.Context, _ string) (Signals, error) {
	return Signals{}, nil
}

var _ Source = sourceShapeCheck{}
