package queue

import (
	"sort"
	"strings"
	"testing"
)

func TestQueueAllows(t *testing.T) {
	tests := []struct {
		name string
		from JobState
		to   JobState
		want bool
	}{
		{"pending to claimed", StatePending, StateClaimed, true},
		{"pending to failed", StatePending, StateFailed, true},
		{"pending to processing", StatePending, StateProcessing, false},
		{"pending to dead", StatePending, StateDead, true}, // issue #134: exhaustion dead-letters from pending
		{"claimed to processing", StateClaimed, StateProcessing, true},
		{"claimed to completed", StateClaimed, StateCompleted, true},
		{"claimed to failed", StateClaimed, StateFailed, true},
		{"claimed to pending", StateClaimed, StatePending, true},
		{"claimed to dead", StateClaimed, StateDead, true}, // issue #134: exhaustion dead-letters from claimed
		{"processing to completed", StateProcessing, StateCompleted, true},
		{"processing to failed", StateProcessing, StateFailed, true},
		{"processing to pending", StateProcessing, StatePending, true},
		{"processing to dead", StateProcessing, StateDead, true},
		{"processing to processing", StateProcessing, StateProcessing, true}, // issue #134: idempotent re-mark
		{"failed to pending", StateFailed, StatePending, true},
		{"failed to dead", StateFailed, StateDead, true},
		{"failed to failed", StateFailed, StateFailed, true}, // issue #134: idempotent re-fail
		{"completed to pending", StateCompleted, StatePending, false},
		{"dead to pending", StateDead, StatePending, false},
		{"dead to dead", StateDead, StateDead, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queueAllows(tt.from, tt.to); got != tt.want {
				t.Errorf("queueAllows(%v, %v) = %v; want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// TestLegalSources pins the source sets the guards build their SQL predicates
// from. A change here changes what Store accepts, so it is reviewed as a
// state-machine change, not a refactor.
func TestLegalSources(t *testing.T) {
	tests := []struct {
		to   JobState
		want []JobState // sorted for comparison
	}{
		{StatePending, []JobState{StateClaimed, StateFailed, StateProcessing}},
		{StateClaimed, []JobState{StatePending}},
		{StateProcessing, []JobState{StateClaimed, StateProcessing}},
		{StateCompleted, []JobState{StateClaimed, StateProcessing}},
		{StateFailed, []JobState{StateClaimed, StateFailed, StatePending, StateProcessing}},
		{StateDead, []JobState{StateClaimed, StateFailed, StatePending, StateProcessing}},
	}
	for _, tt := range tests {
		t.Run(string(tt.to), func(t *testing.T) {
			got := legalSources(tt.to)
			sortStates(got)
			if len(got) != len(tt.want) {
				t.Fatalf("legalSources(%v) = %v, want %v", tt.to, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("legalSources(%v) = %v, want %v", tt.to, got, tt.want)
				}
			}
		})
	}
}

// TestEveryStateIsReachable proves UpdateState's ErrInvalidTransition branch
// is defensive only: no target state has an empty source set, so no correct
// caller can trip it. If someone ever adds a state nobody can enter, this
// test fails instead of that state silently refusing every write.
func TestEveryStateIsReachable(t *testing.T) {
	all := []JobState{StatePending, StateClaimed, StateProcessing, StateCompleted, StateFailed, StateDead}
	for _, state := range all {
		if len(legalSources(state)) == 0 {
			t.Errorf("state %q has no legal source — it can never be entered", state)
		}
	}
}

// TestCompleteAttemptSourcesMatchTransitions is the drift pin promised in
// Store.CompleteAttempt: that method hardcodes `state IN ('claimed',
// 'processing')` rather than building the predicate from the edge table,
// because its token predicate and disposition logic are already audited
// (H4/H5) and rewriting them for symmetry buys nothing. This test makes the
// coupling loud: extend the edge table to admit another source of 'completed'
// and this fails, forcing the hardcoded predicate to be revisited in the same
// commit.
func TestCompleteAttemptSourcesMatchTransitions(t *testing.T) {
	const hardcoded = "claimed, processing"
	sources := legalSources(StateCompleted)
	sortStates(sources)
	got := make([]string, len(sources))
	for i, s := range sources {
		got[i] = string(s)
	}
	if joined := strings.Join(got, ", "); joined != hardcoded {
		t.Errorf("Store.CompleteAttempt hardcodes \"state IN ('%s)\" but jobTransitions now says [%s] — update the predicate in store.go in the same commit",
			hardcoded, joined)
	}
}

func sortStates(states []JobState) {
	sort.Slice(states, func(i, j int) bool { return states[i] < states[j] })
}
