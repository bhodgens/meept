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
	JobTypeAgent    JobType = "agent"
	JobTypeShell    JobType = "shell"
	JobTypeReminder JobType = "reminder"
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
	AgentConfig    *AgentJobConfig    `json:"agent_config,omitempty"`
	ShellConfig    *ShellJobConfig    `json:"shell_config,omitempty"`
	ReminderConfig *ReminderJobConfig `json:"reminder_config,omitempty"`
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
		"job_id":     j.id,
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
		"job_id":    j.id,
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

// CreateJob creates a job from a JobConfig.
func CreateJob(cfg JobConfig, msgBus *bus.MessageBus) (Job, error) {
	switch cfg.Type {
	case JobTypeAgent:
		return NewAgentJob(cfg, msgBus)
	case JobTypeShell:
		return NewShellJob(cfg, msgBus)
	case JobTypeReminder:
		return NewReminderJob(cfg, msgBus)
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
