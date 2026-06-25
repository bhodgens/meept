package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/plan"
)

// PlanHandler provides native RPC methods for plan lifecycle operations.
// It calls PlanManager and PlanStore directly, bypassing the bus proxy
// pattern, so that CLI and TUI commands reach the plan subsystem even
// when no bus subscriber is running.
type PlanHandler struct {
	manager *plan.PlanManager
	store   plan.PlanStore
}

// NewPlanHandler creates a new plan handler. If manager or store is nil the
// registered methods return "plan service not available" errors.
func NewPlanHandler(manager *plan.PlanManager, store plan.PlanStore) *PlanHandler {
	return &PlanHandler{manager: manager, store: store}
}

// RegisterPlanMethods registers plan RPC methods on the server.
func (h *PlanHandler) RegisterPlanMethods(server *Server) {
	server.RegisterHandler("plan.create", h.handleCreate)
	server.RegisterHandler("plan.list", h.handleList)
	server.RegisterHandler("plan.get", h.handleGet)
	server.RegisterHandler("plan.approve", h.handleApprove)
	server.RegisterHandler("plan.reject", h.handleReject)
	server.RegisterHandler("plan.confirm", h.handleConfirm)
	server.RegisterHandler("plan.revise", h.handleRevise)
	server.RegisterHandler("plan.list_by_session", h.handleListBySession)
	server.RegisterHandler("plan.count_by_session", h.handleCountBySession)
	server.RegisterHandler("plan.get_phases", h.handleGetPhases)
}

// avail returns an error if the plan subsystem is not wired.
func (h *PlanHandler) avail() error {
	if h.manager == nil || h.store == nil {
		return fmt.Errorf("plan service not available")
	}
	return nil
}

func (h *PlanHandler) handleCreate(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		ProjectID   string `json:"project_id,omitempty"`
		ProjectPath string `json:"project_path,omitempty"`
		SessionID   string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	p, err := h.manager.CreatePlan(ctx, req.Title, req.Description, req.ProjectID, req.ProjectPath, req.SessionID)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (h *PlanHandler) handleList(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		ProjectID string `json:"project_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	plans, err := h.store.ListPlans(ctx, req.ProjectID, req.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"plans":     plans,
		RPCKeyCount: len(plans),
	}, nil
}

func (h *PlanHandler) handleGet(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	p, err := h.store.GetPlan(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("plan not found: %s", req.ID)
	}
	return p, nil
}

func (h *PlanHandler) handleApprove(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		PlanID    string `json:"plan_id"`
		SessionID string `json:"session_id"`
		By        string `json:"by"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := h.manager.ApprovePlan(ctx, req.PlanID, req.SessionID, req.By); err != nil {
		return nil, err
	}
	return h.store.GetPlan(ctx, req.PlanID)
}

func (h *PlanHandler) handleReject(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		PlanID    string `json:"plan_id"`
		SessionID string `json:"session_id"`
		By        string `json:"by"`
		Reason    string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := h.manager.RejectPlan(ctx, req.PlanID, req.SessionID, req.By, req.Reason); err != nil {
		return nil, err
	}
	return h.store.GetPlan(ctx, req.PlanID)
}

func (h *PlanHandler) handleConfirm(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		PlanID    string `json:"plan_id"`
		SessionID string `json:"session_id"`
		By        string `json:"by"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := h.manager.ConfirmPlan(ctx, req.PlanID, req.SessionID, req.By); err != nil {
		return nil, err
	}
	return h.store.GetPlan(ctx, req.PlanID)
}

func (h *PlanHandler) handleRevise(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		PlanID    string `json:"plan_id"`
		SessionID string `json:"session_id"`
		Feedback  string `json:"feedback"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := h.manager.RevisePlan(ctx, req.PlanID, req.SessionID, req.Feedback); err != nil {
		return nil, err
	}
	return h.store.GetPlan(ctx, req.PlanID)
}

func (h *PlanHandler) handleListBySession(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	plans, err := h.store.ListPlansBySession(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"plans": plans,
	}, nil
}

func (h *PlanHandler) handleCountBySession(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return h.store.CountPlansBySessionAndState(ctx, req.SessionID)
}

func (h *PlanHandler) handleGetPhases(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.avail(); err != nil {
		return nil, err
	}
	var req struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.PlanID == "" {
		return nil, fmt.Errorf("plan_id is required")
	}
	phases, err := h.store.GetPhases(ctx, req.PlanID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"phases": phases,
	}, nil
}
