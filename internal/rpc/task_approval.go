package rpc

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskApprovalHandler exposes the strategic planner's approval gate to RPC
// clients (CLI/TUI/HTTP). Tasks that enter task.StateAwaitingApproval (plan
// size >= ApprovalStepThreshold, or interview-completed plans) were
// previously un-approvable from any external surface: StrategicPlanner.
// ApprovePlan existed but nothing called it. This handler closes that gap.
//
// Direct RegisterHandler, NOT a bus proxy: ApprovePlan persists pending
// steps and schedules them itself, so proxying through task.update would
// only flip the state field without persisting the plan.
type TaskApprovalHandler struct {
	// ApproveFunc performs the approval. Typically StrategicPlanner.
	// ApprovePlan; a func field keeps this package free of an agent import.
	ApproveFunc func(ctx context.Context, taskID string) error
}

// NewTaskApprovalHandler creates the handler. A nil ApproveFunc yields
// "approval service not available" errors on every call.
func NewTaskApprovalHandler(fn func(ctx context.Context, taskID string) error) *TaskApprovalHandler {
	return &TaskApprovalHandler{ApproveFunc: fn}
}

// RegisterTaskApprovalMethods registers the approval RPC methods.
func (h *TaskApprovalHandler) RegisterTaskApprovalMethods(server *Server) {
	server.RegisterHandler("task.approve", h.handleApprove)
	server.RegisterHandler("task.reject", h.handleReject)
}

func (h *TaskApprovalHandler) handleApprove(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if h.ApproveFunc == nil {
		return nil, fmt.Errorf("approval service not available")
	}
	if err := h.ApproveFunc(ctx, req.TaskID); err != nil {
		return nil, err
	}
	return map[string]any{"task_id": req.TaskID, "status": "approved"}, nil
}

func (h *TaskApprovalHandler) handleReject(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		TaskID string `json:"task_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if h.ApproveFunc == nil {
		return nil, fmt.Errorf("approval service not available")
	}
	if err := h.ApproveFunc(ctx, req.TaskID); err != nil {
		return nil, err
	}
	return map[string]any{"task_id": req.TaskID, "status": "rejected"}, nil
}
