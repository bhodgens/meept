package services

import (
	"context"

	"github.com/caimlas/meept/internal/selfimprove"
)

// SelfImproveService handles self-improvement operations.
type SelfImproveService struct {
	controller *selfimprove.Controller
}

// NewSelfImproveService creates a self-improve service.
func NewSelfImproveService(ctrl *selfimprove.Controller) *SelfImproveService {
	return &SelfImproveService{controller: ctrl}
}

// StatusResponse contains self-improvement status.
type StatusResponse struct {
	Enabled       bool   `json:"enabled"`
	LastCycle     string `json:"last_cycle,omitempty"`
	SkillsLearned int    `json:"skills_learned"`
	PendingTasks  int    `json:"pending_tasks"`
}

// Status returns self-improvement status.
func (s *SelfImproveService) Status(ctx context.Context) (*StatusResponse, error) {
	if s.controller == nil {
		return &StatusResponse{
			Enabled: false,
		}, nil
	}

	cs := s.controller.GetStatus()
	sr := &StatusResponse{
		Enabled:      true,
		PendingTasks: cs.IssuesCount,
	}
	if cs.CurrentCycle != nil {
		sr.LastCycle = cs.CurrentCycle.ID
	}
	return sr, nil
}

// TriggerRequest contains trigger parameters.
type TriggerRequest struct {
	Force bool `json:"force,omitempty"`
}

// Trigger starts a self-improvement cycle.
func (s *SelfImproveService) Trigger(ctx context.Context, req TriggerRequest) error {
	if s.controller == nil {
		return wrapError("selfimprove", "Trigger", ErrUnavailable)
	}

	_, err := s.controller.RunFullCycle(ctx, !req.Force)
	return wrapError("selfimprove", "Trigger", err)
}

// CancelRequest contains cancel parameters.
type CancelRequest struct {
	CycleID string `json:"cycle_id"`
}

// Cancel stops an ongoing self-improvement cycle.
func (s *SelfImproveService) Cancel(ctx context.Context, req CancelRequest) error {
	if req.CycleID == "" {
		return wrapError("selfimprove", "Cancel", ErrInvalidInput)
	}
	if s.controller == nil {
		return wrapError("selfimprove", "Cancel", ErrUnavailable)
	}

	return wrapError("selfimprove", "Cancel", s.controller.Stop())
}

// Analyze runs analysis for improvements (alias for Detect).
func (s *SelfImproveService) Analyze(ctx context.Context) error {
	if s.controller == nil {
		return wrapError("selfimprove", "Analyze", ErrUnavailable)
	}
	// Detect is the analyze phase
	_, err := s.controller.Detect(ctx)
	return wrapError("selfimprove", "Analyze", err)
}

// GenerateImprovementRequest contains generate parameters.
type GenerateImprovementRequest struct {
	ImprovementID string `json:"improvement_id"`
}

// Generate creates improvements (part of cycle).
// Note: ImprovementID is not supported by the underlying controller's RunFullCycle,
// which always generates improvements for all detected issues. If ImprovementID is
// set, an error is returned to avoid giving a false impression of targeted generation.
func (s *SelfImproveService) Generate(ctx context.Context, req GenerateImprovementRequest) error {
	if s.controller == nil {
		return wrapError("selfimprove", "Generate", ErrUnavailable)
	}
	if req.ImprovementID != "" {
		return wrapError("selfimprove", "Generate", ErrInvalidInput)
	}
	// Generation happens in the full cycle
	// This endpoint triggers a focused cycle
	_, err := s.controller.RunFullCycle(ctx, false)
	return wrapError("selfimprove", "Generate", err)
}

// ValidateImprovementRequest contains validate parameters.
type ValidateImprovementRequest struct {
	ImprovementID string `json:"improvement_id"`
}

// Validate validates an improvement.
func (s *SelfImproveService) Validate(ctx context.Context, req ValidateImprovementRequest) (any, error) {
	if s.controller == nil {
		return nil, wrapError("selfimprove", "Validate", ErrUnavailable)
	}

	// Run validation phase; results are cached on the controller.
	validations, err := s.controller.Validate(ctx)
	if err != nil {
		return nil, wrapError("selfimprove", "Validate", err)
	}

	// If a specific improvement ID was requested, filter to that fix.
	if req.ImprovementID != "" {
		for _, v := range validations {
			if v.FixID == req.ImprovementID {
				return map[string]any{
					"validated":     v.Success,
					"id":            v.FixID,
					"tests_passed":  v.TestsPassed,
					"tests_failed":  v.TestsFailed,
					"build_success": v.BuildSuccess,
					"errors":        v.Errors,
				}, nil
			}
		}
		return map[string]any{
			"validated": false,
			"id":        req.ImprovementID,
			"message":   "fix not found in validation results",
		}, nil
	}

	// Return all validation results.
	results := make([]map[string]any, 0, len(validations))
	for _, v := range validations {
		results = append(results, map[string]any{
			"validated":     v.Success,
			"id":            v.FixID,
			"tests_passed":  v.TestsPassed,
			"tests_failed":  v.TestsFailed,
			"build_success": v.BuildSuccess,
			"errors":        v.Errors,
		})
	}
	return map[string]any{"validations": results, "count": len(results)}, nil
}

// ApplyImprovementRequest contains apply parameters.
type ApplyImprovementRequest struct {
	ImprovementID string `json:"improvement_id"`
}

// Apply applies an approved improvement.
func (s *SelfImproveService) Apply(ctx context.Context, req ApplyImprovementRequest) error {
	if s.controller == nil {
		return wrapError("selfimprove", "Apply", ErrUnavailable)
	}
	// Use ApproveFix which applies the fix
	_, err := s.controller.ApproveFix(ctx, req.ImprovementID)
	return wrapError("selfimprove", "Apply", err)
}

// RejectImprovementRequest contains reject parameters.
type RejectImprovementRequest struct {
	ImprovementID string `json:"improvement_id"`
	Reason        string `json:"reason"`
}

// Reject rejects a proposed improvement.
func (s *SelfImproveService) Reject(ctx context.Context, req RejectImprovementRequest) error {
	if req.ImprovementID == "" {
		return wrapError("selfimprove", "Reject", ErrInvalidInput)
	}
	if s.controller == nil {
		return wrapError("selfimprove", "Reject", ErrUnavailable)
	}
	return wrapError("selfimprove", "Reject", s.controller.RejectFix(req.ImprovementID, req.Reason))
}
