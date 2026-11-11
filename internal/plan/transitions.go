package plan

import (
	"context"
	"errors"
	"fmt"
)

// ErrPlanNotModifiable is the typed guard error every table-refused or
// race-lost plan transition returns (leaf 03-plans-goals/04, C-P4/T2).
// Callers that already handled the old per-method "plan X is in state Y,
// expected Z" guard errors keep working unchanged (T5 error-shape parity):
// the sentinel wraps a same-shaped message via %w, no caller in the
// RPC/TUI/dispatcher paths string-matches those texts — they surface
// verbatim to the operator. It is deliberately NOT ErrPlanNotFound: a lost
// race or illegal edge means the row EXISTS and is simply not modifiable
// (TestManagerTransitionsUseCAS pins this distinction).
var ErrPlanNotModifiable = errors.New("plan state not modifiable")

// planTransitions is the SINGLE source of legal plan state transitions
// (contract C-P4, design amendment 2026-11-09): an illegal transition is
// unrepresentable instead of merely unguarded. Every enum has a row;
// terminal states are explicit empty to-sets, not missing keys.
//
// Shape notes:
//   - planning is the creation-entry state with no forward edges: CreatePlan
//     writes the row and parks it at draft through its own create-path write
//     (the only SetPlanState that stays in manager.go alongside the
//     reclaimer/lease primitives).
//   - confirmed is fully terminal.
//   - cancelled/failed are terminal for the forward lifecycle but revisable
//     (-> planning, the rework loop; audit F7: the failed->planning row is
//     what makes RevisePlan accept failed plans — leaf 06
//     TestRevisePlanAcceptsFailed tests it, this table implements it) and
//     confirmable (-> confirmed: an operator signs off a failed or cancelled
//     plan's outcome).
//   - synthesizing is the LEASE state (bughunt 2026-10-08): its edges are
//     the synthesis paths only — -> executing (expansion succeeded),
//     -> draft (claim released), -> failed (claim parked after
//     MaxSynthesisAttempts), -> approved (stranded-lease reclaim). Those
//     writes go through the dedicated Claim/Release/Park/Reclaim store
//     primitives, which carry the lease predicate inside their UPDATE;
//     lifecycle callers must not fire them (ApprovePlan pins its from-set
//     to pending_approval precisely so a normal approve can never stomp a
//     live synthesis lease through the synthesizing->approved row).
var planTransitions = map[PlanState][]PlanState{
	// planning is no longer a dead-end (bughunt 2026-11-10 B3): RevisePlan
	// legally moves plans INTO planning, so the row needs forward edges or
	// revised plans strand forever — SubmitPlan needs draft, ApprovePlan
	// needs pending_approval, and PruneStalePlans declines to touch them.
	//   planning -> draft: the revision rework completes (the pre-plan or
	//     revised content is written back to the draft lifecycle); the
	//     writer is PlanManager.CompletePlanningRevision.
	//   planning -> cancelled: age-based retention prunes a stale planning
	//     row (PruneStalePlans), mirroring its pending_approval handling.
	StatePlanning:        {StateDraft, StateCancelled},
	StateDraft:           {StatePendingApproval, StatePlanning, StateCancelled},
	StatePendingApproval: {StateApproved, StateCancelled, StatePlanning},
	StateApproved:        {StateSynthesizing, StateCancelled},
	StateSynthesizing:    {StateExecuting, StateDraft, StateFailed, StateApproved},
	StateExecuting:       {StateCompleted, StateCancelled, StateFailed},
	StateCompleted:       {StateConfirmed},
	StateFailed:          {StateConfirmed, StatePlanning},
	StateCancelled:       {StateConfirmed, StatePlanning},
	StateConfirmed:       {},
}

// transitionStateOrder fixes iteration order so the reverse lookup (and the
// derived error messages) are deterministic.
var transitionStateOrder = []PlanState{
	StatePlanning, StateDraft, StatePendingApproval, StateApproved,
	StateSynthesizing, StateExecuting, StateCompleted, StateConfirmed,
	StateCancelled, StateFailed,
}

// planAllows reports whether from -> to is a table member.
func planAllows(from, to PlanState) bool {
	for _, t := range planTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// planFromStatesFor derives the table states that may reach `to` (reverse
// lookup; 10 rows, a scan is free). Used for the guard error message so the
// operator sees every legal from-state for the edge they tried.
func planFromStatesFor(to PlanState) []PlanState {
	var from []PlanState
	for _, s := range transitionStateOrder {
		if planAllows(s, to) {
			from = append(from, s)
		}
	}
	return from
}

// transition is the single guarded entry point for plan state changes
// (contract C-P4): read the current state once, require table membership,
// then commit through the C-P1 CAS (UpdatePlanStateWhere) with the OBSERVED
// state as the from-set. The read above is for the table check and the
// message only — the guard is the UPDATE's state predicate.
//
// Why the CAS pins the observed state instead of the whole derived from-set:
// with a wide IN(...) set, CancelPlan->ConfirmPlan on one completed plan
// would BOTH succeed (cancel moves completed->cancelled; confirm's derived
// set contains cancelled, so the "loser" wins a second CAS — last-writer-
// wins with two happy callers, the exact silent clobber the migration
// exists to end). Pinning the observed state makes the second racer's
// predicate match zero rows: exactly one winner, loser gets
// ErrPlanNotModifiable, final row is the winner's state
// (TestManagerTransitionsUseCAS).
//
// A non-member edge or a lost race returns an ErrPlanNotModifiable-wrapped
// error and leaves the row untouched.
func (m *PlanManager) transition(ctx context.Context, planID string, to PlanState) error {
	return m.transitionFrom(ctx, planID, nil, to)
}

// transitionFrom is transition() with an optional caller-narrowed set of
// accepted observed states. `restrict` must be a subset of the table's
// edges into `to`; its purpose is to EXCLUDE edges the table legitimately
// has but this call site must never fire:
//   - ApprovePlan pins pending_approval so an operator approve cannot use
//     the synthesizing->approved reclaim row to stomp a live lease.
//   - Revise/Reject/Submit pin the from-sets their pre-migration guards
//     accepted (re-derived at dispatch; the table's draft->planning row
//     exists for the rework path, not the revise command).
func (m *PlanManager) transitionFrom(ctx context.Context, planID string, restrict []PlanState, to PlanState) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("transition plan %s: %w", planID, err)
	}
	observed := plan.State
	if !planAllows(observed, to) {
		return fmt.Errorf(
			"%w: plan %s is in state %s, expected one of %v",
			ErrPlanNotModifiable, planID, observed, planFromStatesFor(to))
	}
	if restrict != nil && !containsState(restrict, observed) {
		return fmt.Errorf(
			"%w: plan %s is in state %s, expected one of %v",
			ErrPlanNotModifiable, planID, observed, restrict)
	}
	won, err := m.store.UpdatePlanStateWhere(ctx, planID, []PlanState{observed}, to)
	if err != nil {
		return fmt.Errorf("transition plan %s to %s: %w", planID, to, err)
	}
	if !won {
		// The row moved (or vanished) between the read and the CAS —
		// exactly one racer wins; the loser is told via the typed error.
		return fmt.Errorf(
			"%w: plan %s left state %s before the transition to %s committed",
			ErrPlanNotModifiable, planID, observed, to)
	}
	return nil
}

func containsState(set []PlanState, s PlanState) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

// updatePlanColumns is the T4 targeted-update seam: apply metadata columns
// to the CURRENT row rather than to a pre-transition in-memory copy. It
// re-reads after the caller's CAS won, runs mutate (which must own only its
// own columns — State is deliberately out of bounds), and writes the fresh
// row back. Because the re-read carries the row's live task_id /
// synthesis_lease_until / goal_id values, a ConfirmPlan or ApprovePlan
// tail write can no longer clobber columns a concurrent lease holder
// stamped (the old full-row UpdatePlan from a stale copy did exactly that).
func (m *PlanManager) updatePlanColumns(ctx context.Context, planID string, mutate func(*Plan)) error {
	fresh, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("re-read plan %s for column update: %w", planID, err)
	}
	mutate(fresh)
	if err := m.store.UpdatePlan(ctx, fresh); err != nil {
		return fmt.Errorf("update plan %s columns: %w", planID, err)
	}
	return nil
}
