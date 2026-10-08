package plan

import (
	"context"
	"time"
)

// PlanStore persists plan metadata, phases, sessions, and signoffs.
type PlanStore interface {
	// Plan CRUD
	CreatePlan(ctx context.Context, p *Plan) error
	GetPlan(ctx context.Context, id string) (*Plan, error)
	UpdatePlan(ctx context.Context, p *Plan) error
	DeletePlan(ctx context.Context, id string) error
	ListPlans(ctx context.Context, projectID string, limit int) ([]*Plan, error)
	ListPlansBySession(ctx context.Context, sessionID string) ([]*Plan, error)
	ListPlansByState(ctx context.Context, state PlanState, limit int) ([]*Plan, error)
	SetPlanState(ctx context.Context, id string, state PlanState) error

	// ClaimPlanSynthesis atomically claims a plan for task expansion: it moves
	// the plan into StateSynthesizing and stamps a lease, but only if the plan
	// is still claimable and no live lease is outstanding. Returns true only for
	// the caller that won the claim. This is what makes Synthesize safe to
	// retry — without it a crash between CreateTask and the task_id write left
	// the plan indistinguishable from a never-expanded one, and the
	// auto-approver re-expanded it forever (bughunt 2026-10-08: 797 plans ->
	// 20.7M orphan tasks at ~14k/s).
	ClaimPlanSynthesis(ctx context.Context, id string, lease time.Duration) (bool, error)

	// ReleasePlanSynthesis returns a claimed plan to StateDraft after a failed
	// expansion and increments its attempt counter.
	ReleasePlanSynthesis(ctx context.Context, id string) error

	// ParkPlanSynthesis moves an over-retry plan to StateFailed so the pump
	// cannot spin on it.
	ParkPlanSynthesis(ctx context.Context, id string) error

	// UpdatePlanStateConditional atomically transitions a plan from
	// fromState to toState. Returns true if the transition succeeded
	// (i.e., exactly one row was affected), false if the plan was not
	// in the expected state (another caller won the race).
	UpdatePlanStateConditional(ctx context.Context, id string, fromState, toState PlanState) (bool, error)

	// Phase operations
	CreatePhase(ctx context.Context, p *PlanPhase) error
	GetPhases(ctx context.Context, planID string) ([]*PlanPhase, error)
	UpdatePhase(ctx context.Context, p *PlanPhase) error
	SetPhaseState(ctx context.Context, id string, state PhaseState) error
	IncrementPhaseProgress(ctx context.Context, phaseID string, field string, delta int) error

	// Session linking
	LinkSession(ctx context.Context, planID, sessionID string) error
	UnlinkSession(ctx context.Context, planID, sessionID string) error
	GetPlansForSession(ctx context.Context, sessionID string) ([]*Plan, error)

	// Signoff operations
	CreateSignoff(ctx context.Context, s *PlanSignoff) error
	GetSignoffs(ctx context.Context, planID string) ([]*PlanSignoff, error)
	GetRevisionCount(ctx context.Context, planID string) (int, error)

	// Counts
	CountPlansBySessionAndState(ctx context.Context, sessionID string) (map[PlanState]int, error)
}
