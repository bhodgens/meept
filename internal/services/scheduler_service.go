package services

import (
	"context"

	"github.com/caimlas/meept/internal/scheduler"
)

// SchedulerService handles scheduler operations.
type SchedulerService struct {
	scheduler *scheduler.Scheduler
}

// NewSchedulerService creates a scheduler service.
func NewSchedulerService(s *scheduler.Scheduler) *SchedulerService {
	return &SchedulerService{scheduler: s}
}

// JobInfo contains job information for API responses.
type JobInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Enabled   bool   `json:"enabled"`
	LastRun   string `json:"last_run,omitempty"`
	NextRun   string `json:"next_run,omitempty"`
}

// ListJobs returns all scheduled jobs.
func (s *SchedulerService) ListJobs(ctx context.Context) ([]JobInfo, error) {
	if s.scheduler == nil {
		return nil, wrapError("scheduler", "ListJobs", ErrUnavailable)
	}

	// TODO: Get actual jobs from scheduler
	return []JobInfo{}, nil
}

// AddJobRequest contains job creation parameters.
type AddJobRequest struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Handler  string `json:"handler"`
	Enabled  bool   `json:"enabled,omitempty"`
}

// AddJob adds a new scheduled job.
func (s *SchedulerService) AddJob(ctx context.Context, req AddJobRequest) (*JobInfo, error) {
	if req.Name == "" {
		return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
	}
	if req.Schedule == "" {
		return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
	}
	if s.scheduler == nil {
		return nil, wrapError("scheduler", "AddJob", ErrUnavailable)
	}

	// TODO: Implement actual job addition
	return &JobInfo{
		ID:      req.Name,
		Name:    req.Name,
		Schedule: req.Schedule,
		Enabled: req.Enabled,
	}, nil
}

// RemoveJobRequest contains remove parameters.
type RemoveJobRequest struct {
	ID string `json:"id"`
}

// RemoveJob removes a scheduled job.
func (s *SchedulerService) RemoveJob(ctx context.Context, req RemoveJobRequest) error {
	if req.ID == "" {
		return wrapError("scheduler", "RemoveJob", ErrInvalidInput)
	}
	if s.scheduler == nil {
		return wrapError("scheduler", "RemoveJob", ErrUnavailable)
	}

	// TODO: Implement actual job removal
	return nil
}

// EnableJobRequest contains enable parameters.
type EnableJobRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// EnableJob enables or disables a scheduled job.
func (s *SchedulerService) EnableJob(ctx context.Context, req EnableJobRequest) error {
	if req.ID == "" {
		return wrapError("scheduler", "EnableJob", ErrInvalidInput)
	}
	if s.scheduler == nil {
		return wrapError("scheduler", "EnableJob", ErrUnavailable)
	}

	// TODO: Implement actual job enable/disable
	return nil
}
