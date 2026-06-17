package services

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
)

// RuntimeService provides runtime management operations through the service layer.
type RuntimeService struct {
	manager *llm.RuntimeManager
}

// NewRuntimeService creates a runtime service.
func NewRuntimeService(manager *llm.RuntimeManager) *RuntimeService {
	return &RuntimeService{manager: manager}
}

// RuntimeStatusResponse is the response for runtime status queries.
type RuntimeStatusResponse struct {
	Runtimes []llm.RuntimeStatus `json:"runtimes"`
}

// Status returns the status of all managed runtimes.
func (s *RuntimeService) Status(ctx context.Context) (*RuntimeStatusResponse, error) {
	if s.manager == nil {
		return nil, wrapError("runtime", "Status", ErrUnavailable)
	}
	statuses := s.manager.Status()
	return &RuntimeStatusResponse{Runtimes: statuses}, nil
}

// StatusForProvider returns the status of a specific provider.
func (s *RuntimeService) StatusForProvider(ctx context.Context, providerID string) (*llm.RuntimeStatus, error) {
	if s.manager == nil {
		return nil, wrapError("runtime", "StatusForProvider", ErrUnavailable)
	}
	status, ok := s.manager.StatusForProvider(providerID)
	if !ok {
		return nil, wrapError("runtime", "StatusForProvider", fmt.Errorf("provider %s not found", providerID))
	}
	return &status, nil
}

// StartProvider starts a specific provider's runtime.
func (s *RuntimeService) StartProvider(ctx context.Context, providerID string) error {
	if s.manager == nil {
		return wrapError("runtime", "StartProvider", ErrUnavailable)
	}
	return s.manager.StartProvider(ctx, providerID)
}

// StopProvider stops a specific provider's runtime.
func (s *RuntimeService) StopProvider(ctx context.Context, providerID string) error {
	if s.manager == nil {
		return wrapError("runtime", "StopProvider", ErrUnavailable)
	}
	return s.manager.StopProvider(ctx, providerID)
}

// RestartProvider restarts a specific provider's runtime.
func (s *RuntimeService) RestartProvider(ctx context.Context, providerID string) error {
	if s.manager == nil {
		return wrapError("runtime", "RestartProvider", ErrUnavailable)
	}
	return s.manager.RestartProvider(ctx, providerID)
}
