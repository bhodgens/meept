package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/robfig/cron/v3"
)

// RPCHandler provides RPC methods for the scheduler.
type RPCHandler struct {
	scheduler *Scheduler
}

// NewRPCHandler creates a new RPC handler for the scheduler.
func NewRPCHandler(scheduler *Scheduler) *RPCHandler {
	return &RPCHandler{
		scheduler: scheduler,
	}
}

// RegisterRPCHandlers registers all scheduler RPC handlers with the server.
func RegisterRPCHandlers(server *rpc.Server, scheduler *Scheduler) {
	handler := NewRPCHandler(scheduler)

	server.RegisterHandler("scheduler.add_job", handler.AddJob)
	server.RegisterHandler("scheduler.remove_job", handler.RemoveJob)
	server.RegisterHandler("scheduler.list_jobs", handler.ListJobs)
	server.RegisterHandler("scheduler.run_job", handler.RunJob)
	server.RegisterHandler("scheduler.get_job", handler.GetJob)
	server.RegisterHandler("scheduler.pause_job", handler.PauseJob)
	server.RegisterHandler("scheduler.resume_job", handler.ResumeJob)
	server.RegisterHandler("scheduler.validate_schedule", handler.ValidateSchedule)
	server.RegisterHandler("scheduler.status", handler.Status)
}

// AddJobParams represents the parameters for adding a job.
type AddJobParams struct {
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name"`
	Type     JobType `json:"type"`
	Schedule string  `json:"schedule"`
	Enabled  bool    `json:"enabled"`
	Tags     []string `json:"tags,omitempty"`

	// Type-specific configs (only one should be set)
	AgentConfig    *AgentJobConfig    `json:"agent_config,omitempty"`
	ShellConfig    *ShellJobConfig    `json:"shell_config,omitempty"`
	ReminderConfig *ReminderJobConfig `json:"reminder_config,omitempty"`
}

// AddJobResult represents the result of adding a job.
type AddJobResult struct {
	JobID   string     `json:"job_id"`
	NextRun *time.Time `json:"next_run,omitempty"`
}

// AddJob adds a new job to the scheduler.
func (h *RPCHandler) AddJob(ctx context.Context, params json.RawMessage) (any, error) {
	var p AddJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Generate ID if not provided
	if p.ID == "" {
		p.ID = fmt.Sprintf("job-%d", time.Now().UnixNano())
	}

	// Build job config
	cfg := JobConfig{
		ID:             p.ID,
		Name:           p.Name,
		Type:           p.Type,
		Schedule:       p.Schedule,
		Enabled:        p.Enabled,
		CreatedAt:      time.Now().UTC(),
		Tags:           p.Tags,
		AgentConfig:    p.AgentConfig,
		ShellConfig:    p.ShellConfig,
		ReminderConfig: p.ReminderConfig,
	}

	// Validate config
	if err := ValidateJobConfig(cfg); err != nil {
		return nil, err
	}

	// Schedule the job
	jobID, err := h.scheduler.ScheduleConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to schedule job: %w", err)
	}

	// Get next run time
	result := AddJobResult{
		JobID: jobID,
	}

	if nextRun, err := h.scheduler.NextRun(jobID); err == nil {
		result.NextRun = &nextRun
	}

	return result, nil
}

// RemoveJobParams represents the parameters for removing a job.
type RemoveJobParams struct {
	JobID string `json:"job_id"`
}

// RemoveJob removes a job from the scheduler.
func (h *RPCHandler) RemoveJob(ctx context.Context, params json.RawMessage) (any, error) {
	var p RemoveJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	if err := h.scheduler.Unschedule(p.JobID); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"job_id":  p.JobID,
	}, nil
}

// ListJobsParams represents the parameters for listing jobs.
type ListJobsParams struct {
	Type    JobType  `json:"type,omitempty"`    // Filter by type
	Tags    []string `json:"tags,omitempty"`    // Filter by tags
	Enabled *bool    `json:"enabled,omitempty"` // Filter by enabled status
}

// ListJobs returns a list of all scheduled jobs.
func (h *RPCHandler) ListJobs(ctx context.Context, params json.RawMessage) (any, error) {
	var p ListJobsParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
	}

	jobs := h.scheduler.ListJobs()

	// Apply filters
	if p.Type != "" || len(p.Tags) > 0 || p.Enabled != nil {
		filtered := make([]JobInfo, 0)
		for _, job := range jobs {
			// Filter by type
			if p.Type != "" && job.Type != p.Type {
				continue
			}

			// Filter by enabled status
			if p.Enabled != nil && job.Enabled != *p.Enabled {
				continue
			}

			// Filter by tags (requires store lookup for tags)
			if len(p.Tags) > 0 {
				if cfg, ok := h.scheduler.Store().Get(job.ID); ok {
					if !hasAnyTag(cfg.Tags, p.Tags) {
						continue
					}
				} else {
					continue
				}
			}

			filtered = append(filtered, job)
		}
		jobs = filtered
	}

	return map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	}, nil
}

// RunJobParams represents the parameters for running a job immediately.
type RunJobParams struct {
	JobID string `json:"job_id"`
}

// RunJob triggers immediate execution of a job.
func (h *RPCHandler) RunJob(ctx context.Context, params json.RawMessage) (any, error) {
	var p RunJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	if err := h.scheduler.RunNow(p.JobID); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":   true,
		"job_id":    p.JobID,
		"triggered": time.Now().UTC(),
	}, nil
}

// GetJobParams represents the parameters for getting a job.
type GetJobParams struct {
	JobID string `json:"job_id"`
}

// GetJob returns information about a specific job.
func (h *RPCHandler) GetJob(ctx context.Context, params json.RawMessage) (any, error) {
	var p GetJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	info, ok := h.scheduler.GetJob(p.JobID)
	if !ok {
		return nil, fmt.Errorf("job not found: %s", p.JobID)
	}

	// Include full config
	result := map[string]any{
		"job": info,
	}

	if cfg, ok := h.scheduler.Store().Get(p.JobID); ok {
		result["config"] = cfg
	}

	return result, nil
}

// PauseJobParams represents the parameters for pausing a job.
type PauseJobParams struct {
	JobID string `json:"job_id"`
}

// PauseJob pauses a scheduled job.
func (h *RPCHandler) PauseJob(ctx context.Context, params json.RawMessage) (any, error) {
	var p PauseJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	if err := h.scheduler.PauseJob(p.JobID); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"job_id":  p.JobID,
		"paused":  true,
	}, nil
}

// ResumeJobParams represents the parameters for resuming a job.
type ResumeJobParams struct {
	JobID string `json:"job_id"`
}

// ResumeJob resumes a paused job.
func (h *RPCHandler) ResumeJob(ctx context.Context, params json.RawMessage) (any, error) {
	var p ResumeJobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	if err := h.scheduler.ResumeJob(p.JobID); err != nil {
		return nil, err
	}

	// Get next run time
	nextRun, _ := h.scheduler.NextRun(p.JobID)

	return map[string]any{
		"success":  true,
		"job_id":   p.JobID,
		"resumed":  true,
		"next_run": nextRun,
	}, nil
}

// ValidateScheduleParams represents the parameters for validating a schedule.
type ValidateScheduleParams struct {
	Schedule string `json:"schedule"`
}

// ValidateSchedule validates a cron schedule expression.
func (h *RPCHandler) ValidateSchedule(ctx context.Context, params json.RawMessage) (any, error) {
	var p ValidateScheduleParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	if p.Schedule == "" {
		return nil, fmt.Errorf("schedule is required")
	}

	err := h.scheduler.ValidateSchedule(p.Schedule)
	result := map[string]any{
		"schedule": p.Schedule,
		"valid":    err == nil,
	}

	if err != nil {
		result["error"] = err.Error()
	} else {
		// Calculate next few run times
		result["examples"] = getScheduleExamples(p.Schedule, h.scheduler.Location(), 5)
	}

	return result, nil
}

// Status returns the scheduler status.
func (h *RPCHandler) Status(ctx context.Context, params json.RawMessage) (any, error) {
	jobs := h.scheduler.ListJobs()

	// Count running jobs
	runningCount := 0
	for _, job := range jobs {
		if job.IsRunning {
			runningCount++
		}
	}

	// Count enabled/disabled
	enabledCount := 0
	disabledCount := 0
	for _, job := range jobs {
		if job.Enabled {
			enabledCount++
		} else {
			disabledCount++
		}
	}

	return map[string]any{
		"running":        h.scheduler.Running(),
		"timezone":       h.scheduler.Location().String(),
		"total_jobs":     len(jobs),
		"enabled_jobs":   enabledCount,
		"disabled_jobs":  disabledCount,
		"running_jobs":   runningCount,
		"storage_path":   h.scheduler.Store().FilePath(),
	}, nil
}

// hasAnyTag checks if the job has any of the specified tags.
func hasAnyTag(jobTags, filterTags []string) bool {
	if len(jobTags) == 0 {
		return false
	}

	tagSet := make(map[string]bool)
	for _, t := range jobTags {
		tagSet[t] = true
	}

	for _, t := range filterTags {
		if tagSet[t] {
			return true
		}
	}
	return false
}

// getScheduleExamples returns the next N run times for a schedule.
func getScheduleExamples(schedule string, loc *time.Location, count int) []string {
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)

	sched, err := parser.Parse(schedule)
	if err != nil {
		return nil
	}

	examples := make([]string, 0, count)
	t := time.Now().In(loc)

	for i := 0; i < count; i++ {
		t = sched.Next(t)
		examples = append(examples, t.Format(time.RFC3339))
	}

	return examples
}
