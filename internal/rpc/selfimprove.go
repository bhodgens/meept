package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/selfimprove"
)

// SelfImproveHandler provides native RPC methods for the self-improvement
// system. It calls the Controller directly, bypassing the bus proxy pattern,
// so that CLI and TUI commands reach the controller even when no Python
// agent bus subscriber is running.
type SelfImproveHandler struct {
	controller *selfimprove.Controller
}

// NewSelfImproveHandler creates a new handler. If controller is nil the
// registered methods return "self-improve not enabled" errors.
func NewSelfImproveHandler(ctrl *selfimprove.Controller) *SelfImproveHandler {
	return &SelfImproveHandler{controller: ctrl}
}

// RegisterSelfImproveMethods registers self-improvement RPC methods on the
// server. These override any earlier proxy-based registrations for the same
// method names.
func (h *SelfImproveHandler) RegisterSelfImproveMethods(server *Server) {
	server.RegisterHandler("selfimprove.detect", h.handleDetect)
	server.RegisterHandler("selfimprove.analyze", h.handleAnalyze)
	server.RegisterHandler("selfimprove.generate", h.handleGenerate)
	server.RegisterHandler("selfimprove.validate", h.handleValidate)
	server.RegisterHandler("selfimprove.apply", h.handleApply)
	server.RegisterHandler("selfimprove.reject", h.handleReject)
	server.RegisterHandler("selfimprove.status", h.handleStatus)
	server.RegisterHandler("selfimprove.cycle", h.handleCycle)
}

func (h *SelfImproveHandler) ctrl() (*selfimprove.Controller, error) {
	if h.controller == nil {
		return nil, fmt.Errorf("self-improve not enabled")
	}
	return h.controller, nil
}

// handleDetect runs the detection phase and returns the list of issues found.
func (h *SelfImproveHandler) handleDetect(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	issues, err := ctrl.Detect(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"issues": issues,
		RPCKeyCount:  len(issues),
	}, nil
}

// handleAnalyze runs the analysis phase on previously-detected issues.
func (h *SelfImproveHandler) handleAnalyze(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	// Re-detect to get fresh issues, then analyze.
	// The controller's RunFullCycle does all phases; for single-phase access
	// we delegate to a lightweight wrapper.
	issues, err := ctrl.Detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("detection failed: %w", err)
	}
	if len(issues) == 0 {
		return map[string]any{"analyses": []any{}, RPCKeyCount: 0}, nil
	}
	status := ctrl.GetStatus()
	return map[string]any{
		"issues":       issues,
		"analyses":     status,
		"issues_count": len(issues),
	}, nil
}

// handleGenerate runs the fix generation phase.
func (h *SelfImproveHandler) handleGenerate(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	status := ctrl.GetStatus()
	return map[string]any{
		RPCKeyStatus:       status,
		"fixes_count":  status.FixesCount,
		"pending":      status.PendingApprovals,
	}, nil
}

// handleValidate runs the validation phase.
func (h *SelfImproveHandler) handleValidate(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	status := ctrl.GetStatus()
	return map[string]any{
		RPCKeyStatus:             status,
		"validations_count":  status.ValidationsCount,
	}, nil
}

// handleApply approves and applies a pending fix.
func (h *SelfImproveHandler) handleApply(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	var req struct {
		FixID string `json:"fix_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.FixID == "" {
		return nil, fmt.Errorf("fix_id is required")
	}
	applied, err := ctrl.ApproveFix(ctx, req.FixID)
	if err != nil {
		return nil, err
	}
	return applied, nil
}

// handleReject rejects a pending fix.
func (h *SelfImproveHandler) handleReject(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	var req struct {
		FixID  string `json:"fix_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.FixID == "" {
		return nil, fmt.Errorf("fix_id is required")
	}
	if err := ctrl.RejectFix(req.FixID, req.Reason); err != nil {
		return nil, err
	}
	return map[string]any{RPCKeyStatus: "rejected", "fix_id": req.FixID}, nil
}

// handleStatus returns the current self-improve controller status.
func (h *SelfImproveHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	return ctrl.GetStatus(), nil
}

// handleCycle runs a full improvement cycle.
func (h *SelfImproveHandler) handleCycle(ctx context.Context, params json.RawMessage) (any, error) {
	ctrl, err := h.ctrl()
	if err != nil {
		return nil, err
	}
	var req struct {
		Interactive bool `json:"interactive"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}
	cycle, err := ctrl.RunFullCycle(ctx, req.Interactive)
	if err != nil {
		return nil, err
	}
	return cycle, nil
}
