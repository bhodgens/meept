package rpc

import (
	"context"
	"encoding/json"

	"github.com/caimlas/meept/pkg/security"
)

// SecurityHandler provides RPC methods for permission checking.
type SecurityHandler struct {
	checker *security.PermissionChecker
}

// NewSecurityHandler creates a new security handler with the given config.
func NewSecurityHandler(cfg security.Config) *SecurityHandler {
	return &SecurityHandler{
		checker: security.NewPermissionChecker(cfg),
	}
}

// RegisterSecurityMethods registers security RPC methods on the server.
func (h *SecurityHandler) RegisterSecurityMethods(server *Server) {
	// Check a single permission
	server.RegisterHandler("security.check_permission", h.handleCheckPermission)

	// Evaluate shell command risk
	server.RegisterHandler("security.evaluate_shell_risk", h.handleEvaluateShellRisk)

	// Check if text is financial
	server.RegisterHandler("security.is_financial", h.handleIsFinancial)

	// Check path access
	server.RegisterHandler("security.check_path", h.handleCheckPath)

	// Batch permission check
	server.RegisterHandler("security.check_batch", h.handleCheckBatch)
}

// CheckPermissionRequest is the request for security.check_permission.
type CheckPermissionRequest struct {
	Action  string            `json:"action"`
	Details map[string]string `json:"details"`
}

// CheckPermissionResponse is the response for security.check_permission.
type CheckPermissionResponse struct {
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason"`
	EffectiveRisk string `json:"effective_risk"`
	NeedsConfirm  bool   `json:"needs_confirm"`
}

func (h *SecurityHandler) handleCheckPermission(ctx context.Context, params json.RawMessage) (any, error) {
	var req CheckPermissionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	result := h.checker.CheckPermission(req.Action, req.Details)
	return CheckPermissionResponse{
		Allowed:       result.Allowed,
		Reason:        result.Reason,
		EffectiveRisk: result.EffectiveRisk.String(),
		NeedsConfirm:  result.NeedsConfirm,
	}, nil
}

// EvaluateShellRiskRequest is the request for security.evaluate_shell_risk.
type EvaluateShellRiskRequest struct {
	Command string `json:"command"`
}

// EvaluateShellRiskResponse is the response for security.evaluate_shell_risk.
type EvaluateShellRiskResponse struct {
	RiskLevel string `json:"risk_level"`
}

func (h *SecurityHandler) handleEvaluateShellRisk(ctx context.Context, params json.RawMessage) (any, error) {
	var req EvaluateShellRiskRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	risk := security.EvaluateShellRisk(req.Command)
	return EvaluateShellRiskResponse{
		RiskLevel: risk.String(),
	}, nil
}

// IsFinancialRequest is the request for security.is_financial.
type IsFinancialRequest struct {
	Text string `json:"text"`
}

// IsFinancialResponse is the response for security.is_financial.
type IsFinancialResponse struct {
	IsFinancial bool `json:"is_financial"`
}

func (h *SecurityHandler) handleIsFinancial(ctx context.Context, params json.RawMessage) (any, error) {
	var req IsFinancialRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	return IsFinancialResponse{
		IsFinancial: security.IsFinancial(req.Text),
	}, nil
}

// CheckPathRequest is the request for security.check_path.
type CheckPathRequest struct {
	Path string `json:"path"`
}

// CheckPathResponse is the response for security.check_path.
type CheckPathResponse struct {
	Allowed bool `json:"allowed"`
}

func (h *SecurityHandler) handleCheckPath(ctx context.Context, params json.RawMessage) (any, error) {
	var req CheckPathRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	return CheckPathResponse{
		Allowed: h.checker.CheckPath(req.Path),
	}, nil
}

// BatchCheckRequest is the request for security.check_batch.
type BatchCheckRequest struct {
	Checks []CheckPermissionRequest `json:"checks"`
}

// BatchCheckResponse is the response for security.check_batch.
type BatchCheckResponse struct {
	Results []CheckPermissionResponse `json:"results"`
}

func (h *SecurityHandler) handleCheckBatch(ctx context.Context, params json.RawMessage) (any, error) {
	var req BatchCheckRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	results := make([]CheckPermissionResponse, len(req.Checks))
	for i, check := range req.Checks {
		result := h.checker.CheckPermission(check.Action, check.Details)
		results[i] = CheckPermissionResponse{
			Allowed:       result.Allowed,
			Reason:        result.Reason,
			EffectiveRisk: result.EffectiveRisk.String(),
			NeedsConfirm:  result.NeedsConfirm,
		}
	}

	return BatchCheckResponse{Results: results}, nil
}
