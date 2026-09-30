package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/plan"
)

// PlanService handles plan lifecycle operations.
type PlanService struct {
	manager         *plan.PlanManager
	store           plan.PlanStore
	fallbackManager *plan.PlanManager // evolver sink; either may be nil
	fallbackStore   plan.PlanStore
}

// NewPlanService creates a plan service.
func NewPlanService(manager *plan.PlanManager, store plan.PlanStore) *PlanService {
	return &PlanService{manager: manager, store: store}
}

// SetEvolverSink wires the evolver sink manager + store. Either argument
// may be nil; nil values are ignored (setter nil-guard convention).
func (s *PlanService) SetEvolverSink(m *plan.PlanManager, store plan.PlanStore) {
	if m != nil {
		s.fallbackManager = m
	}
	if store != nil {
		s.fallbackStore = store
	}
}

// CreatePlanRequest contains plan creation parameters.
type CreatePlanRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	SessionID   string `json:"session_id"`
}

// ApprovePlanRequest contains plan approval parameters.
type ApprovePlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	By        string `json:"by"`
}

// RejectPlanRequest contains plan rejection parameters.
type RejectPlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	By        string `json:"by"`
	Reason    string `json:"reason,omitempty"`
}

// ConfirmPlanRequest contains plan confirmation parameters.
type ConfirmPlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	By        string `json:"by"`
}

// RevisePlanRequest contains plan revision parameters.
type RevisePlanRequest struct {
	PlanID    string `json:"plan_id"`
	SessionID string `json:"session_id"`
	Feedback  string `json:"feedback"`
}

// Create creates a new plan.
func (s *PlanService) Create(ctx context.Context, req CreatePlanRequest) (*plan.Plan, error) {
	if req.Title == "" {
		return nil, wrapError("plan", "Create", ErrInvalidInput)
	}
	if s.manager == nil {
		return nil, wrapError("plan", "Create", ErrUnavailable)
	}
	p, err := s.manager.CreatePlan(ctx, req.Title, req.Description, req.ProjectID, req.ProjectPath, req.SessionID)
	if err != nil {
		return nil, wrapError("plan", "Create", err)
	}
	return p, nil
}

// Get retrieves a plan by ID.
func (s *PlanService) Get(ctx context.Context, planID string) (*plan.Plan, error) {
	if planID == "" {
		return nil, wrapError("plan", "Get", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("plan", "Get", ErrUnavailable)
	}
	p, err := s.store.GetPlan(ctx, planID)
	if err != nil && s.fallbackStore != nil {
		// Not in the shared store: try the evolver sink store.
		p, err = s.fallbackStore.GetPlan(ctx, planID)
	}
	if err != nil {
		return nil, wrapError("plan", "Get", err)
	}
	if p == nil {
		return nil, wrapError("plan", "Get", ErrNotFound)
	}
	return p, nil
}

// List returns plans for a project.
func (s *PlanService) List(ctx context.Context, projectID string, limit int) ([]*plan.Plan, error) {
	if s.store == nil {
		return nil, wrapError("plan", "List", ErrUnavailable)
	}
	if limit <= 0 {
		limit = 50
	}
	plans, err := s.store.ListPlans(ctx, projectID, limit)
	if err != nil {
		return nil, wrapError("plan", "List", err)
	}
	return plans, nil
}

// ListBySession returns plans linked to a session.
func (s *PlanService) ListBySession(ctx context.Context, sessionID string) ([]*plan.Plan, error) {
	if sessionID == "" {
		return nil, wrapError("plan", "ListBySession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("plan", "ListBySession", ErrUnavailable)
	}
	plans, err := s.store.ListPlansBySession(ctx, sessionID)
	if err != nil {
		return nil, wrapError("plan", "ListBySession", err)
	}
	return plans, nil
}

// Approve approves a pending plan.
func (s *PlanService) Approve(ctx context.Context, req ApprovePlanRequest) (*plan.Plan, error) {
	if req.PlanID == "" {
		return nil, wrapError("plan", "Approve", ErrInvalidInput)
	}
	if s.manager == nil || s.store == nil {
		return nil, wrapError("plan", "Approve", ErrUnavailable)
	}
	// Mirror rpc/plan.go handleApprove: track whether the sink manager
	// performed the transition, and read the plan back from the side
	// that actually holds it — a sink-only plan is invisible to the
	// shared store, so a shared-store read-back would return
	// "plan not found" on success.
	usedFallback := false
	err := s.manager.ApprovePlan(ctx, req.PlanID, req.SessionID, req.By)
	if err != nil && s.fallbackManager != nil {
		// Not in the shared store: try the evolver sink manager.
		if err2 := s.fallbackManager.ApprovePlan(ctx, req.PlanID, req.SessionID, req.By); err2 == nil {
			usedFallback = true
			err = nil
		} else {
			err = errors.Join(fmt.Errorf("shared: %w", err), fmt.Errorf("sink: %w", err2))
		}
	}
	if err != nil {
		return nil, wrapError("plan", "Approve", err)
	}
	if usedFallback {
		return s.fallbackManager.GetPlan(ctx, req.PlanID)
	}
	p, err := s.store.GetPlan(ctx, req.PlanID)
	if err != nil {
		return nil, wrapError("plan", "Approve", err)
	}
	return p, nil
}

// Reject rejects a pending plan.
func (s *PlanService) Reject(ctx context.Context, req RejectPlanRequest) (*plan.Plan, error) {
	if req.PlanID == "" {
		return nil, wrapError("plan", "Reject", ErrInvalidInput)
	}
	if s.manager == nil || s.store == nil {
		return nil, wrapError("plan", "Reject", ErrUnavailable)
	}
	// Mirror Approve: read back from the sink manager when it performed
	// the transition (a sink-only plan is invisible to the shared
	// store's read-back).
	usedFallback := false
	err := s.manager.RejectPlan(ctx, req.PlanID, req.SessionID, req.By, req.Reason)
	if err != nil && s.fallbackManager != nil {
		if err2 := s.fallbackManager.RejectPlan(ctx, req.PlanID, req.SessionID, req.By, req.Reason); err2 == nil {
			usedFallback = true
			err = nil
		} else {
			err = errors.Join(fmt.Errorf("shared: %w", err), fmt.Errorf("sink: %w", err2))
		}
	}
	if err != nil {
		return nil, wrapError("plan", "Reject", err)
	}
	if usedFallback {
		return s.fallbackManager.GetPlan(ctx, req.PlanID)
	}
	p, err := s.store.GetPlan(ctx, req.PlanID)
	if err != nil {
		return nil, wrapError("plan", "Reject", err)
	}
	return p, nil
}

// Confirm confirms a completed plan.
func (s *PlanService) Confirm(ctx context.Context, req ConfirmPlanRequest) (*plan.Plan, error) {
	if req.PlanID == "" {
		return nil, wrapError("plan", "Confirm", ErrInvalidInput)
	}
	if s.manager == nil || s.store == nil {
		return nil, wrapError("plan", "Confirm", ErrUnavailable)
	}
	// Mirror Approve: read back from the sink manager when it performed
	// the transition (a sink-only plan is invisible to the shared
	// store's read-back).
	usedFallback := false
	err := s.manager.ConfirmPlan(ctx, req.PlanID, req.SessionID, req.By)
	if err != nil && s.fallbackManager != nil {
		if err2 := s.fallbackManager.ConfirmPlan(ctx, req.PlanID, req.SessionID, req.By); err2 == nil {
			usedFallback = true
			err = nil
		} else {
			err = errors.Join(fmt.Errorf("shared: %w", err), fmt.Errorf("sink: %w", err2))
		}
	}
	if err != nil {
		return nil, wrapError("plan", "Confirm", err)
	}
	if usedFallback {
		return s.fallbackManager.GetPlan(ctx, req.PlanID)
	}
	p, err := s.store.GetPlan(ctx, req.PlanID)
	if err != nil {
		return nil, wrapError("plan", "Confirm", err)
	}
	return p, nil
}

// CountBySession returns counts of plans grouped by state for a session.
func (s *PlanService) CountBySession(ctx context.Context, sessionID string) (map[plan.PlanState]int, error) {
	if sessionID == "" {
		return nil, wrapError("plan", "CountBySession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("plan", "CountBySession", ErrUnavailable)
	}
	counts, err := s.store.CountPlansBySessionAndState(ctx, sessionID)
	if err != nil {
		return nil, wrapError("plan", "CountBySession", err)
	}
	return counts, nil
}

// Phases returns the phases (with produces/consumes artifacts) for a plan.
func (s *PlanService) Phases(ctx context.Context, planID string) ([]*plan.PlanPhase, error) {
	if planID == "" {
		return nil, wrapError("plan", "Phases", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("plan", "Phases", ErrUnavailable)
	}
	phases, err := s.store.GetPhases(ctx, planID)
	if err != nil {
		return nil, wrapError("plan", "Phases", err)
	}
	return phases, nil
}

// Handoffs returns structured handoffs associated with a plan's steps.
// MVP: returns nil — handoff content is currently embedded in step.AccumulatedContext,
// not persisted as separate records. Full handoff persistence is a follow-up.
// This method exists for API parity with the plans surface.
func (s *PlanService) Handoffs(ctx context.Context, planID string) ([]*agent.StepHandoff, error) {
	_ = ctx
	_ = planID
	// TODO(follow-up): query steps for planID, parse handoff markdown from
	// AccumulatedContext, return structured records. For now, return nil.
	return nil, nil
}

// Revise requests revision of a plan.
func (s *PlanService) Revise(ctx context.Context, req RevisePlanRequest) (*plan.Plan, error) {
	if req.PlanID == "" || req.Feedback == "" {
		return nil, wrapError("plan", "Revise", ErrInvalidInput)
	}
	if s.manager == nil || s.store == nil {
		return nil, wrapError("plan", "Revise", ErrUnavailable)
	}
	// Mirror Approve: read back from the sink manager when it performed
	// the transition (a sink-only plan is invisible to the shared
	// store's read-back).
	usedFallback := false
	err := s.manager.RevisePlan(ctx, req.PlanID, req.SessionID, req.Feedback)
	if err != nil && s.fallbackManager != nil {
		if err2 := s.fallbackManager.RevisePlan(ctx, req.PlanID, req.SessionID, req.Feedback); err2 == nil {
			usedFallback = true
			err = nil
		} else {
			err = errors.Join(fmt.Errorf("shared: %w", err), fmt.Errorf("sink: %w", err2))
		}
	}
	if err != nil {
		return nil, wrapError("plan", "Revise", err)
	}
	if usedFallback {
		return s.fallbackManager.GetPlan(ctx, req.PlanID)
	}
	p, err := s.store.GetPlan(ctx, req.PlanID)
	if err != nil {
		return nil, wrapError("plan", "Revise", err)
	}
	return p, nil
}
