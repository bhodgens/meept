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
	Enabled      bool   `json:"enabled"`
	LastCycle    string `json:"last_cycle,omitempty"`
	SkillsLearned int   `json:"skills_learned"`
	PendingTasks int   `json:"pending_tasks"`
}

// Status returns self-improvement status.
func (s *SelfImproveService) Status(ctx context.Context) (*StatusResponse, error) {
	if s.controller == nil {
		return &StatusResponse{
			Enabled: false,
		}, nil
	}

	// TODO: Get actual status from controller
	return &StatusResponse{
		Enabled: true,
	}, nil
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

	// TODO: Implement actual trigger logic
	// For now, just return nil to indicate success
	return nil
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

	// TODO: Implement actual cancel logic
	return nil
}
