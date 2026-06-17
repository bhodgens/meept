package services

import (
	"context"

	"github.com/caimlas/meept/internal/plan"
)

// PlanService handles plan lifecycle operations.
type PlanService struct {
	manager *plan.PlanManager
	store   plan.PlanStore
}

// NewPlanService creates a plan service.
func NewPlanService(manager *plan.PlanManager, store plan.PlanStore) *PlanService {
	return &PlanService{manager: manager, store: store}
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
	if err := s.manager.ApprovePlan(ctx, req.PlanID, req.SessionID, req.By); err != nil {
		return nil, wrapError("plan", "Approve", err)
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
	if err := s.manager.RejectPlan(ctx, req.PlanID, req.SessionID, req.By, req.Reason); err != nil {
		return nil, wrapError("plan", "Reject", err)
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
	if err := s.manager.ConfirmPlan(ctx, req.PlanID, req.SessionID, req.By); err != nil {
		return nil, wrapError("plan", "Confirm", err)
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

// Revise requests revision of a plan.
func (s *PlanService) Revise(ctx context.Context, req RevisePlanRequest) (*plan.Plan, error) {
	if req.PlanID == "" || req.Feedback == "" {
		return nil, wrapError("plan", "Revise", ErrInvalidInput)
	}
	if s.manager == nil || s.store == nil {
		return nil, wrapError("plan", "Revise", ErrUnavailable)
	}
	if err := s.manager.RevisePlan(ctx, req.PlanID, req.SessionID, req.Feedback); err != nil {
		return nil, wrapError("plan", "Revise", err)
	}
	p, err := s.store.GetPlan(ctx, req.PlanID)
	if err != nil {
		return nil, wrapError("plan", "Revise", err)
	}
	return p, nil
}
