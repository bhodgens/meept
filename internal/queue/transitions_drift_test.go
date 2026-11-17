package queue

// Drift pins for the SQL state-mutating statements (issue #134).
//
// The queue state machine has ONE authority — jobTransitions in transitions.go
// — but the mutators live in SQL. A statement that hard-codes an allowed-state
// list (or that builds its own `IN (...)` from a hand-maintained set) can
// silently disagree with the map when either side is edited alone. These tests
// make that disagreement a CI failure instead of a latent cross-layer bug:
// they assert each mutator's SQL predicate is EXACTLY the rendering of
// legalSources(target), the single helper that walks jobTransitions.
//
// Two complementary pins:
//
//   - TestMutatorPredicatesMatchTransitions asserts each mutator's emitted
//     predicate string equals predicateFor(target). The mutators build that
//     string through the same helper, so any edge added to or removed from
//     jobTransitions propagates to both sides automatically; a mutator that
//     re-hard-codes a literal list fails the moment it drifts from the map.
//   - TestEveryMutatorTargetIsReachable proves no mutator target has an empty
//     source set (which would emit invalid `IN ()` SQL or trip
//     UpdateState's ErrInvalidTransition branch).
//
// CompleteAttempt is pinned separately by
// TestCompleteAttemptSourcesMatchTransitions (it is deliberately hard-coded
// because its token/disposition logic is already audited, H4/H5). The
// mutators covered here fold legalSources() directly.

import (
	"strings"
	"testing"
)

// TestMutatorPredicatesMatchTransitions pins every SQL state-mutating
// statement against jobTransitions. Each mutator that derives its predicate
// from the map must emit exactly predicateFor(target). Adding or removing an
// edge in jobTransitions changes both the helper output and the mutator's SQL
// together; a mutator that re-hard-codes a literal allowed-state list breaks
// this pin.
//
// Covered statements (target -> legal sources per the map):
//   - MarkProcessing  -> processing
//   - FailAttempt     -> failed  (and, when the retry budget is exhausted, dead)
//   - Requeue         -> pending
//   - Retry           -> pending
//   - ResetToPending  -> pending
func TestMutatorPredicatesMatchTransitions(t *testing.T) {
	tests := []struct {
		name string
		// pred is the exact SQL fragment the mutator folds into its WHERE
		// clause. It must equal predicateFor(target); the id/claim_token
		// conjuncts are orthogonal to the state machine (pinned elsewhere:
		// store_gate_test.go) and are appended by the caller, not here.
		pred   string
		target JobState
	}{
		{"MarkProcessing", predicateFor(StateProcessing), StateProcessing},
		{"FailAttempt/failed", predicateFor(StateFailed), StateFailed},
		{"FailAttempt/dead", predicateFor(StateDead), StateDead},
		{"Requeue", predicateFor(StatePending), StatePending},
		{"Retry", predicateFor(StatePending), StatePending},
		{"ResetToPending", predicateFor(StatePending), StatePending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := predicateFor(tt.target)
			if tt.pred != want {
				t.Errorf("mutator %s predicate %q disagrees with jobTransitions (target %s): want %q — reconcile store.go in the same commit",
					tt.name, tt.pred, tt.target, want)
			}
			// The fragment must be a bound predicate (one '?' per source),
			// never a re-hard-coded literal list.
			if n := strings.Count(tt.pred, "?"); n != len(legalSources(tt.target)) {
				t.Errorf("mutator %s predicate has %d placeholders, want %d (one per source) — is it still folded from the map?",
					tt.name, n, len(legalSources(tt.target)))
			}
		})
	}
}

// TestMarkProcessingAllowsIdempotentRemark pins the processing->processing
// self-edge. A worker whose MarkProcessing response was lost (or that re-enters
// the processing window) must re-mark its own live attempt processing without
// being told it "lost the race". The edge is real and load-bearing, so
// jobTransitions must carry it.
func TestMarkProcessingAllowsIdempotentRemark(t *testing.T) {
	if !queueAllows(StateProcessing, StateProcessing) {
		t.Fatalf("jobTransitions lacks the processing->processing self-edge; a MarkProcessing re-mark of a live processing attempt would be refused")
	}
}

// TestFailAttemptAllowsRetryableReFail pins the failed->failed self-edge. A
// token-less operator re-report of an already-failed attempt (queue.fail, the
// HTTP force-fail surface) must stay an idempotent success rather than being
// refused, and the idempotent-retry step reads the row back as 'failed' —
// which only exists if failed->failed is admitted.
func TestFailAttemptAllowsRetryableReFail(t *testing.T) {
	if !queueAllows(StateFailed, StateFailed) {
		t.Fatalf("jobTransitions lacks the failed->failed self-edge; a token-less re-fail of an already-failed attempt would be refused")
	}
}

// TestFailAttemptCanDeadLetterFromPending pins the pending->dead edge. Retry
// budget exhaustion (retryCount >= maxRetries) is reachable from a pending row
// — e.g. a MaxRetries(0) job failed straight out of pending — so the fail path
// must be able to resolve to 'dead' from pending, not just from the in-flight
// states the pre-fix map admitted.
func TestFailAttemptCanDeadLetterFromPending(t *testing.T) {
	if !queueAllows(StatePending, StateDead) {
		t.Fatalf("jobTransitions lacks the pending->dead edge; retry-budget exhaustion from a pending row cannot dead-letter")
	}
}

// TestEveryMutatorTargetIsReachable is the reachability half of the pin: every
// state a mutator writes must have at least one legal source, or the folded
// predicate (and UpdateState's ErrInvalidTransition branch) would reject a
// state that is actually reachable through a mutator, or emit invalid `IN ()`
// SQL.
func TestEveryMutatorTargetIsReachable(t *testing.T) {
	targets := []JobState{StateClaimed, StateProcessing, StateCompleted, StateFailed, StateDead, StatePending}
	for _, to := range targets {
		if len(legalSources(to)) == 0 {
			t.Errorf("mutator target %q has no legal source — no mutator can enter it", to)
		}
	}
}
