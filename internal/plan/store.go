package plan

import "context"

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
