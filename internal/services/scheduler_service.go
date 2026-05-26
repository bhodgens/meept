package services

import (
	"context"
	"time"

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

// ListJobsResponse contains job information for API responses.
type ListJobsResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"`
	Enabled   bool       `json:"enabled"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	NextRun   *time.Time `json:"next_run,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	RunCount  int64      `json:"run_count"`
	IsRunning bool       `json:"is_running"`
}

// ListJobs returns all scheduled jobs.
func (s *SchedulerService) ListJobs(ctx context.Context) ([]ListJobsResponse, error) {
	if s.scheduler == nil {
		return nil, wrapError("scheduler", "ListJobs", ErrUnavailable)
	}

	jobs := s.scheduler.ListJobs()
	result := make([]ListJobsResponse, len(jobs))
	for i, job := range jobs {
		result[i] = ListJobsResponse{
			ID:        job.ID,
			Name:      job.Name,
			Schedule:  job.Schedule,
			Enabled:   job.Enabled,
			LastRun:   job.LastRun,
			NextRun:   job.NextRun,
			LastError: job.LastError,
			RunCount:  job.RunCount,
			IsRunning: job.IsRunning,
		}
	}
	return result, nil
}

// AddJobRequest contains job creation parameters.
type AddJobRequest struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Schedule    string          `json:"schedule"`
	Type        string          `json:"type"` // "agent" or "shell"
	AgentConfig *AgentJobConfig `json:"agent_config,omitempty"`
	ShellConfig *ShellJobConfig `json:"shell_config,omitempty"`
	Enabled     bool            `json:"enabled,omitempty"`
}

// AgentJobConfig contains agent job configuration.
type AgentJobConfig struct {
	Prompt      string            `json:"prompt"`
	Context     map[string]string `json:"context,omitempty"`
	Model       string            `json:"model,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
}

// ShellJobConfig contains shell job configuration.
type ShellJobConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	WorkDir     string            `json:"work_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	TimeoutSecs int               `json:"timeout_secs,omitempty"`
	CaptureOut  bool              `json:"capture_output"`
}

// AddJobResponse contains job creation response.
type AddJobResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"`
	Enabled   bool       `json:"enabled"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	NextRun   *time.Time `json:"next_run,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	RunCount  int64      `json:"run_count"`
	IsRunning bool       `json:"is_running"`
}

// AddJob adds a new scheduled job.
func (s *SchedulerService) AddJob(ctx context.Context, req AddJobRequest) (*AddJobResponse, error) {
	if req.Name == "" {
		return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
	}
	if req.Schedule == "" {
		return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
	}
	if req.Type == "" {
		return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
	}
	if s.scheduler == nil {
		return nil, wrapError("scheduler", "AddJob", ErrUnavailable)
	}

	// Create job config
	cfg := scheduler.JobConfig{
		ID:       req.ID,
		Name:     req.Name,
		Schedule: req.Schedule,
		Type:     scheduler.JobType(req.Type),
		Enabled:  req.Enabled,
	}

	// Set type-specific config
	switch scheduler.JobType(req.Type) {
	case scheduler.JobTypeAgent:
		if req.AgentConfig == nil {
			return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
		}
		cfg.AgentConfig = &scheduler.AgentJobConfig{
			Prompt:      req.AgentConfig.Prompt,
			Context:     req.AgentConfig.Context,
			Model:       req.AgentConfig.Model,
			MaxTokens:   req.AgentConfig.MaxTokens,
			Temperature: req.AgentConfig.Temperature,
		}
	case scheduler.JobTypeShell:
		if req.ShellConfig == nil {
			return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
		}
		cfg.ShellConfig = &scheduler.ShellJobConfig{
			Command:    req.ShellConfig.Command,
			Args:       req.ShellConfig.Args,
			WorkDir:    req.ShellConfig.WorkDir,
			Env:        req.ShellConfig.Env,
			Timeout:    time.Duration(req.ShellConfig.TimeoutSecs) * time.Second,
			CaptureOut: req.ShellConfig.CaptureOut,
		}
	default:
		return nil, wrapError("scheduler", "AddJob", ErrInvalidInput)
	}

	// Schedule the job
	jobID, err := s.scheduler.ScheduleConfig(cfg)
	if err != nil {
		return nil, wrapError("scheduler", "AddJob", err)
	}

	// Get job info
	jobInfo, ok := s.scheduler.GetJob(jobID)
	if !ok {
		return nil, wrapError("scheduler", "AddJob", ErrNotFound)
	}

	return &AddJobResponse{
		ID:        jobID,
		Name:      jobInfo.Name,
		Schedule:  jobInfo.Schedule,
		Enabled:   jobInfo.Enabled,
		LastRun:   jobInfo.LastRun,
		NextRun:   jobInfo.NextRun,
		LastError: jobInfo.LastError,
		RunCount:  jobInfo.RunCount,
		IsRunning: jobInfo.IsRunning,
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

	return s.scheduler.Unschedule(req.ID)
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

	if req.Enabled {
		return s.scheduler.ResumeJob(req.ID)
	}
	return s.scheduler.PauseJob(req.ID)
}

// PauseJobRequest contains pause parameters.
type PauseJobRequest struct {
	ID string `json:"id"`
}

// PauseJob pauses a scheduled job.
func (s *SchedulerService) PauseJob(ctx context.Context, req PauseJobRequest) error {
	if req.ID == "" {
		return wrapError("scheduler", "PauseJob", ErrInvalidInput)
	}
	if s.scheduler == nil {
		return wrapError("scheduler", "PauseJob", ErrUnavailable)
	}

	return s.scheduler.PauseJob(req.ID)
}

// ResumeJobRequest contains resume parameters.
type ResumeJobRequest struct {
	ID string `json:"id"`
}

// ResumeJob resumes a paused job.
func (s *SchedulerService) ResumeJob(ctx context.Context, req ResumeJobRequest) error {
	if req.ID == "" {
		return wrapError("scheduler", "ResumeJob", ErrInvalidInput)
	}
	if s.scheduler == nil {
		return wrapError("scheduler", "ResumeJob", ErrUnavailable)
	}

	return s.scheduler.ResumeJob(req.ID)
}
