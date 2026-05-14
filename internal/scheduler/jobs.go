// Package scheduler provides cron-based job scheduling for the meept daemon.
package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Job represents a schedulable task.
type Job interface {
	// ID returns the unique identifier for this job.
	ID() string
	// Name returns a human-readable name for this job.
	Name() string
	// Schedule returns the cron expression for this job.
	Schedule() string
	// Execute runs the job with the given context.
	Execute(ctx context.Context) error
	// Type returns the job type (agent, shell, reminder).
	Type() JobType
	// Config returns the job configuration for persistence.
	Config() JobConfig
}

// JobType represents the type of a job.
type JobType string

const (
	JobTypeAgent        JobType = "agent"
	JobTypeShell        JobType = "shell"
	JobTypeReminder     JobType = "reminder"
	JobTypeOptimization JobType = "optimization" // Memory graph optimization
	JobTypeSecurity     JobType = "security"     // Security scans
	JobTypeLearning     JobType = "learning"     // Learning consolidation
)

// JobConfig is the serializable configuration for a job.
type JobConfig struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        JobType           `json:"type"`
	Schedule    string            `json:"schedule"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   time.Time         `json:"created_at"`
	LastRunAt   *time.Time        `json:"last_run_at,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
	RunCount    int64             `json:"run_count"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`

	// Type-specific configuration
	AgentConfig        *AgentJobConfig        `json:"agent_config,omitempty"`
	ShellConfig        *ShellJobConfig        `json:"shell_config,omitempty"`
	ReminderConfig     *ReminderJobConfig     `json:"reminder_config,omitempty"`
	OptimizationConfig *OptimizationJobConfig `json:"optimization_config,omitempty"`
	SecurityConfig     *SecurityJobConfig     `json:"security_config,omitempty"`
	LearningConfig     *LearningJobConfig     `json:"learning_config,omitempty"`
}

// AgentJobConfig holds configuration for agent jobs.
type AgentJobConfig struct {
	Prompt      string            `json:"prompt"`
	Context     map[string]string `json:"context,omitempty"`
	Model       string            `json:"model,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
}

// ShellJobConfig holds configuration for shell jobs.
type ShellJobConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	WorkDir     string            `json:"work_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	CaptureOut  bool              `json:"capture_output"`
}

// ReminderJobConfig holds configuration for reminder jobs.
type ReminderJobConfig struct {
	Message    string   `json:"message"`
	Channels   []string `json:"channels,omitempty"` // e.g., ["telegram", "notification"]
	Priority   string   `json:"priority,omitempty"` // low, normal, high, urgent
	RepeatUntil *time.Time `json:"repeat_until,omitempty"`
}

// OptimizationJobConfig holds configuration for optimization jobs.
type OptimizationJobConfig struct {
	Target string `json:"target"` // "memory_graph", "memory_consolidate", "all"
}

// SecurityJobConfig holds configuration for security scan jobs.
type SecurityJobConfig struct {
	ScanTypes []string `json:"scan_types"` // "permissions", "patterns", "secrets"
}

// LearningJobConfig holds configuration for learning pipeline jobs.
type LearningJobConfig struct {
	Action string `json:"action"` // "consolidate", "decay_stale", "detect_contradictions"
}

// JobInfo contains information about a scheduled job.
type JobInfo struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         JobType    `json:"type"`
	Schedule     string     `json:"schedule"`
	Enabled      bool       `json:"enabled"`
	NextRun      *time.Time `json:"next_run,omitempty"`
	LastRun      *time.Time `json:"last_run,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	RunCount     int64      `json:"run_count"`
	IsRunning    bool       `json:"is_running"`
}

// JobResult holds the result of a job execution.
type JobResult struct {
	JobID     string        `json:"job_id"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`
	Success   bool          `json:"success"`
	Output    string        `json:"output,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// baseJob provides common job functionality.
type baseJob struct {
	id       string
	name     string
	schedule string
	jobType  JobType
	config   JobConfig
}

func (b *baseJob) ID() string       { return b.id }
func (b *baseJob) Name() string     { return b.name }
func (b *baseJob) Schedule() string { return b.schedule }
func (b *baseJob) Type() JobType    { return b.jobType }
func (b *baseJob) Config() JobConfig { return b.config }

// AgentJob triggers a chat request with a prompt.
type AgentJob struct {
	baseJob
	prompt  string
	context map[string]string
	model   string
	bus     *bus.MessageBus
}

// NewAgentJob creates a new agent job.
func NewAgentJob(cfg JobConfig, msgBus *bus.MessageBus) (*AgentJob, error) {
	if cfg.AgentConfig == nil {
		return nil, fmt.Errorf("agent job requires agent_config")
	}
	if cfg.AgentConfig.Prompt == "" {
		return nil, fmt.Errorf("agent job requires a prompt")
	}

	return &AgentJob{
		baseJob: baseJob{
			id:       cfg.ID,
			name:     cfg.Name,
			schedule: cfg.Schedule,
			jobType:  JobTypeAgent,
			config:   cfg,
		},
		prompt:  cfg.AgentConfig.Prompt,
		context: cfg.AgentConfig.Context,
		model:   cfg.AgentConfig.Model,
		bus:     msgBus,
	}, nil
}

// Execute sends a chat request to the agent via the message bus.
func (j *AgentJob) Execute(ctx context.Context) error {
	payload := map[string]any{
		"prompt":     j.prompt,
		"source":     "scheduler",
		SchedulerKeyJobID:     j.id,
		"job_name":   j.name,
	}

	if j.context != nil {
		payload["context"] = j.context
	}
	if j.model != "" {
		payload["model"] = j.model
	}
	if j.config.AgentConfig.MaxTokens > 0 {
		payload["max_tokens"] = j.config.AgentConfig.MaxTokens
	}
	if j.config.AgentConfig.Temperature > 0 {
		payload["temperature"] = j.config.AgentConfig.Temperature
	}

	msg, err := models.NewBusMessage(models.MessageTypeRequest, "scheduler."+j.id, payload)
	if err != nil {
		return fmt.Errorf("failed to create bus message: %w", err)
	}

	// Publish to agent.chat topic for the front agent to pick up
	delivered := j.bus.Publish("agent.chat", msg)
	if delivered == 0 {
		return fmt.Errorf("no subscribers for agent.chat topic")
	}

	return nil
}

// ShellJob runs a shell command.
type ShellJob struct {
	baseJob
	command string
	args    []string
	workDir string
	env     map[string]string
	timeout time.Duration
	capture bool
	bus     *bus.MessageBus
}

// NewShellJob creates a new shell job.
func NewShellJob(cfg JobConfig, msgBus *bus.MessageBus) (*ShellJob, error) {
	if cfg.ShellConfig == nil {
		return nil, fmt.Errorf("shell job requires shell_config")
	}
	if cfg.ShellConfig.Command == "" {
		return nil, fmt.Errorf("shell job requires a command")
	}

	timeout := cfg.ShellConfig.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute // Default timeout
	}

	return &ShellJob{
		baseJob: baseJob{
			id:       cfg.ID,
			name:     cfg.Name,
			schedule: cfg.Schedule,
			jobType:  JobTypeShell,
			config:   cfg,
		},
		command: cfg.ShellConfig.Command,
		args:    cfg.ShellConfig.Args,
		workDir: cfg.ShellConfig.WorkDir,
		env:     cfg.ShellConfig.Env,
		timeout: timeout,
		capture: cfg.ShellConfig.CaptureOut,
		bus:     msgBus,
	}, nil
}

// Execute runs the shell command.
func (j *ShellJob) Execute(ctx context.Context) error {
	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	// Build command
	//nolint:gosec // validated input
	cmd := exec.CommandContext(execCtx, j.command, j.args...)

	if j.workDir != "" {
		cmd.Dir = j.workDir
	}

	// Set environment
	if len(j.env) > 0 {
		env := make([]string, 0, len(j.env))
		for k, v := range j.env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = append(cmd.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	if j.capture {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	// Publish result event
	result := JobResult{
		JobID:     j.id,
		StartTime: startTime,
		EndTime:   time.Now(),
		Duration:  duration,
		Success:   err == nil,
	}

	if j.capture {
		result.Output = strings.TrimSpace(stdout.String())
		if stderr.Len() > 0 {
			result.Output += "\n" + strings.TrimSpace(stderr.String())
		}
	}

	if err != nil {
		result.Error = err.Error()
	}

	// Publish job completion event
	if j.bus != nil {
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+j.id, result)
		j.bus.Publish("scheduler.job.completed", msg)
	}

	return err
}

// ReminderJob sends a reminder message.
type ReminderJob struct {
	baseJob
	message  string
	channels []string
	priority string
	bus      *bus.MessageBus
}

// NewReminderJob creates a new reminder job.
func NewReminderJob(cfg JobConfig, msgBus *bus.MessageBus) (*ReminderJob, error) {
	if cfg.ReminderConfig == nil {
		return nil, fmt.Errorf("reminder job requires reminder_config")
	}
	if cfg.ReminderConfig.Message == "" {
		return nil, fmt.Errorf("reminder job requires a message")
	}

	channels := cfg.ReminderConfig.Channels
	if len(channels) == 0 {
		channels = []string{"notification"} // Default channel
	}

	priority := cfg.ReminderConfig.Priority
	if priority == "" {
		priority = "normal"
	}

	return &ReminderJob{
		baseJob: baseJob{
			id:       cfg.ID,
			name:     cfg.Name,
			schedule: cfg.Schedule,
			jobType:  JobTypeReminder,
			config:   cfg,
		},
		message:  cfg.ReminderConfig.Message,
		channels: channels,
		priority: priority,
		bus:      msgBus,
	}, nil
}

// Execute sends the reminder to configured channels.
func (j *ReminderJob) Execute(ctx context.Context) error {
	payload := map[string]any{
		"message":   j.message,
		SchedulerKeyJobID:    j.id,
		"job_name":  j.name,
		"priority":  j.priority,
		"channels":  j.channels,
		"timestamp": time.Now().UTC(),
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+j.id, payload)
	if err != nil {
		return fmt.Errorf("failed to create bus message: %w", err)
	}

	// Publish to each configured channel
	var lastErr error
	totalDelivered := 0
	for _, channel := range j.channels {
		topic := "reminder." + channel
		delivered := j.bus.Publish(topic, msg)
		totalDelivered += delivered
		if delivered == 0 {
			lastErr = fmt.Errorf("no subscribers for topic: %s", topic)
		}
	}

	// Also publish a general reminder event
	j.bus.Publish("scheduler.reminder", msg)

	if totalDelivered == 0 && lastErr != nil {
		return lastErr
	}

	return nil
}

// MemoryOptimizer is the interface for memory graph optimization operations.
// Implementations may return additional result types; callers should use
// MemoryOptimizerFunc adapters when the concrete type has a richer return signature.
type MemoryOptimizer interface {
	UpdateGraphMetrics(ctx context.Context) error
	ConsolidateMemory(ctx context.Context) error
}

// LearningConsolidator is the interface for learning pipeline consolidation.
// Implementations may return additional result types; callers should use
// LearningConsolidatorFunc adapters when the concrete type has a richer return signature.
type LearningConsolidator interface {
	ConsolidateLearning(ctx context.Context) error
}

// MemoryOptimizerAdapter wraps separate functions to satisfy MemoryOptimizer.
type MemoryOptimizerAdapter struct {
	UpdateMetricsFn  func(ctx context.Context) error
	ConsolidateFn    func(ctx context.Context) error
}

func (a *MemoryOptimizerAdapter) UpdateGraphMetrics(ctx context.Context) error {
	if a.UpdateMetricsFn == nil {
		return fmt.Errorf("UpdateGraphMetrics not configured")
	}
	return a.UpdateMetricsFn(ctx)
}

func (a *MemoryOptimizerAdapter) ConsolidateMemory(ctx context.Context) error {
	if a.ConsolidateFn == nil {
		return fmt.Errorf("ConsolidateMemory not configured")
	}
	return a.ConsolidateFn(ctx)
}

// LearningConsolidatorAdapter wraps a function to satisfy LearningConsolidator.
type LearningConsolidatorAdapter struct {
	ConsolidateFn func(ctx context.Context) error
}

func (a *LearningConsolidatorAdapter) ConsolidateLearning(ctx context.Context) error {
	if a.ConsolidateFn == nil {
		return fmt.Errorf("ConsolidateLearning not configured")
	}
	return a.ConsolidateFn(ctx)
}

// SecurityScanner is the interface for security scanning operations.
type SecurityScanner interface {
	ScanAll(ctx context.Context) ([]SecurityIssue, error)
}

// SecurityIssue represents a security issue found during scanning.
type SecurityIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Location    string `json:"location,omitempty"`
}

// JobDependencies holds dependencies for extended job types.
type JobDependencies struct {
	Bus              *bus.MessageBus
	MemoryManager    MemoryOptimizer
	LearningPipeline LearningConsolidator
	SecurityEngine   SecurityScanner
}

// OptimizationJob runs memory graph optimization tasks.
type OptimizationJob struct {
	baseJob
	target string
	deps   *JobDependencies
}

// NewOptimizationJob creates a new optimization job.
func NewOptimizationJob(cfg JobConfig, deps *JobDependencies) (*OptimizationJob, error) {
	if cfg.OptimizationConfig == nil {
		return nil, fmt.Errorf("optimization job requires optimization_config")
	}
	target := cfg.OptimizationConfig.Target
	if target == "" {
		target = "all"
	}

	return &OptimizationJob{
		baseJob: baseJob{
			id:       cfg.ID,
			name:     cfg.Name,
			schedule: cfg.Schedule,
			jobType:  JobTypeOptimization,
			config:   cfg,
		},
		target: target,
		deps:   deps,
	}, nil
}

// Execute runs the optimization job.
func (j *OptimizationJob) Execute(ctx context.Context) error {
	if j.deps == nil || j.deps.MemoryManager == nil {
		return fmt.Errorf("memory manager not configured")
	}

	switch j.target {
	case "memory_graph":
		return j.deps.MemoryManager.UpdateGraphMetrics(ctx)
	case "memory_consolidate":
		return j.deps.MemoryManager.ConsolidateMemory(ctx)
	case "all":
		if err := j.deps.MemoryManager.UpdateGraphMetrics(ctx); err != nil {
			return fmt.Errorf("graph metrics failed: %w", err)
		}
		return j.deps.MemoryManager.ConsolidateMemory(ctx)
	default:
		return fmt.Errorf("unknown optimization target: %s", j.target)
	}
}

// SecurityJob runs security scan tasks.
type SecurityJob struct {
	baseJob
	scanTypes []string
	deps      *JobDependencies
}

// NewSecurityJob creates a new security job.
func NewSecurityJob(cfg JobConfig, deps *JobDependencies) (*SecurityJob, error) {
	if cfg.SecurityConfig == nil {
		return nil, fmt.Errorf("security job requires security_config")
	}
	scanTypes := cfg.SecurityConfig.ScanTypes
	if len(scanTypes) == 0 {
		scanTypes = []string{"permissions", "patterns"}
	}

	return &SecurityJob{
		baseJob: baseJob{
			id:       cfg.ID,
			name:     cfg.Name,
			schedule: cfg.Schedule,
			jobType:  JobTypeSecurity,
			config:   cfg,
		},
		scanTypes: scanTypes,
		deps:      deps,
	}, nil
}

// Execute runs the security job.
func (j *SecurityJob) Execute(ctx context.Context) error {
	if j.deps == nil || j.deps.SecurityEngine == nil {
		return fmt.Errorf("security engine not configured")
	}

	issues, err := j.deps.SecurityEngine.ScanAll(ctx)
	if err != nil {
		return fmt.Errorf("security scan failed: %w", err)
	}

	// Publish scan results
	if j.deps.Bus != nil {
		payload := map[string]any{
			SchedulerKeyJobID:      j.id,
			"issues":      issues,
			"issue_count": len(issues),
			"scan_types":  j.scanTypes,
		}
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+j.id, payload)
		j.deps.Bus.Publish("scheduler.security.completed", msg)
	}

	return nil
}

// LearningJob runs learning pipeline tasks.
type LearningJob struct {
	baseJob
	action string
	deps   *JobDependencies
}

// NewLearningJob creates a new learning job.
func NewLearningJob(cfg JobConfig, deps *JobDependencies) (*LearningJob, error) {
	if cfg.LearningConfig == nil {
		return nil, fmt.Errorf("learning job requires learning_config")
	}
	action := cfg.LearningConfig.Action
	if action == "" {
		action = "consolidate"
	}

	return &LearningJob{
		baseJob: baseJob{
			id:       cfg.ID,
			name:     cfg.Name,
			schedule: cfg.Schedule,
			jobType:  JobTypeLearning,
			config:   cfg,
		},
		action: action,
		deps:   deps,
	}, nil
}

// Execute runs the learning job.
func (j *LearningJob) Execute(ctx context.Context) error {
	if j.deps == nil || j.deps.LearningPipeline == nil {
		return fmt.Errorf("learning pipeline not configured")
	}

	switch j.action {
	case "consolidate":
		return j.deps.LearningPipeline.ConsolidateLearning(ctx)
	default:
		return fmt.Errorf("unknown learning action: %s", j.action)
	}
}

// CreateJob creates a job from a JobConfig using only the message bus.
// For extended job types (optimization, security, learning), use CreateJobWithDeps.
func CreateJob(cfg JobConfig, msgBus *bus.MessageBus) (Job, error) {
	switch cfg.Type {
	case JobTypeAgent:
		return NewAgentJob(cfg, msgBus)
	case JobTypeShell:
		return NewShellJob(cfg, msgBus)
	case JobTypeReminder:
		return NewReminderJob(cfg, msgBus)
	case JobTypeOptimization, JobTypeSecurity, JobTypeLearning:
		return nil, fmt.Errorf("job type %s requires dependencies; use CreateJobWithDeps", cfg.Type)
	default:
		return nil, fmt.Errorf("unknown job type: %s", cfg.Type)
	}
}

// CreateJobWithDeps creates a job from a JobConfig with full dependencies.
func CreateJobWithDeps(cfg JobConfig, deps *JobDependencies) (Job, error) {
	switch cfg.Type {
	case JobTypeAgent:
		return NewAgentJob(cfg, deps.Bus)
	case JobTypeShell:
		return NewShellJob(cfg, deps.Bus)
	case JobTypeReminder:
		return NewReminderJob(cfg, deps.Bus)
	case JobTypeOptimization:
		return NewOptimizationJob(cfg, deps)
	case JobTypeSecurity:
		return NewSecurityJob(cfg, deps)
	case JobTypeLearning:
		return NewLearningJob(cfg, deps)
	default:
		return nil, fmt.Errorf("unknown job type: %s", cfg.Type)
	}
}

// ValidateJobConfig validates a job configuration.
func ValidateJobConfig(cfg JobConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("job ID is required")
	}
	if cfg.Name == "" {
		return fmt.Errorf("job name is required")
	}
	if cfg.Schedule == "" {
		return fmt.Errorf("job schedule is required")
	}
	if cfg.Type == "" {
		return fmt.Errorf("job type is required")
	}

	switch cfg.Type {
	case JobTypeAgent:
		if cfg.AgentConfig == nil {
			return fmt.Errorf("agent job requires agent_config")
		}
		if cfg.AgentConfig.Prompt == "" {
			return fmt.Errorf("agent job requires a prompt")
		}
	case JobTypeShell:
		if cfg.ShellConfig == nil {
			return fmt.Errorf("shell job requires shell_config")
		}
		if cfg.ShellConfig.Command == "" {
			return fmt.Errorf("shell job requires a command")
		}
	case JobTypeReminder:
		if cfg.ReminderConfig == nil {
			return fmt.Errorf("reminder job requires reminder_config")
		}
		if cfg.ReminderConfig.Message == "" {
			return fmt.Errorf("reminder job requires a message")
		}
	case JobTypeOptimization:
		if cfg.OptimizationConfig == nil {
			return fmt.Errorf("optimization job requires optimization_config")
		}
	case JobTypeSecurity:
		if cfg.SecurityConfig == nil {
			return fmt.Errorf("security job requires security_config")
		}
	case JobTypeLearning:
		if cfg.LearningConfig == nil {
			return fmt.Errorf("learning job requires learning_config")
		}
	default:
		return fmt.Errorf("unknown job type: %s", cfg.Type)
	}

	return nil
}

// MarshalJSON implements json.Marshaler for JobConfig.
func (c JobConfig) MarshalJSON() ([]byte, error) {
	type Alias JobConfig
	return json.Marshal(struct {
		Alias
		CreatedAt string     `json:"created_at"`
		LastRunAt *string    `json:"last_run_at,omitempty"`
	}{
		Alias:     Alias(c),
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		LastRunAt: func() *string {
			if c.LastRunAt != nil {
				s := c.LastRunAt.Format(time.RFC3339)
				return &s
			}
			return nil
		}(),
	})
}
