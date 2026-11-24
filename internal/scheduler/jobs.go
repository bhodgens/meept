// Package scheduler provides cron-based job scheduling for the meept daemon.
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
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
	JobTypeInterval     JobType = "interval"     // Generic interval callback
)

// JobConfig is the serializable configuration for a job.
type JobConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      JobType           `json:"type"`
	Schedule  string            `json:"schedule"`
	Enabled   bool              `json:"enabled"`
	CreatedAt time.Time         `json:"created_at"`
	LastRunAt *time.Time        `json:"last_run_at,omitempty"`
	LastError string            `json:"last_error,omitempty"`
	RunCount  int64             `json:"run_count"`
	Tags      []string          `json:"tags,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`

	// Type-specific configuration
	AgentConfig        *AgentJobConfig        `json:"agent_config,omitempty"`
	ShellConfig        *ShellJobConfig        `json:"shell_config,omitempty"`
	ReminderConfig     *ReminderJobConfig     `json:"reminder_config,omitempty"`
	OptimizationConfig *OptimizationJobConfig `json:"optimization_config,omitempty"`
	SecurityConfig     *SecurityJobConfig     `json:"security_config,omitempty"`
	LearningConfig     *LearningJobConfig     `json:"learning_config,omitempty"`

	// Timing is the semantic schedule spec (leaf 05-schedule/01-timing-kinds).
	// When set, the job may be expressed and re-registered from this spec
	// (Schedule OR Timing validation); the normalized cron string stays in
	// Schedule for the engine.
	Timing *TimingSpec `json:"timing,omitempty"`

	// One-shot (TimingAt) lifecycle bookkeeping. Additive JSON fields:
	// older rows without them load unchanged and behave as today.
	// OneShotFailed marks a one-shot whose due time fell outside the
	// [scheduler] one_shot_window catch-up horizon before the daemon could
	// fire it — the job never ran, the row stops looking schedulable (no
	// cron entry), and it stays re-runnable via RunNow. OneShotFiredAt
	// records when a boot-recovered overdue one-shot fired, so the boot
	// sweep neither re-fires nor re-marks it.
	OneShotFailed  bool       `json:"one_shot_failed,omitempty"`
	OneShotFiredAt *time.Time `json:"one_shot_fired_at,omitempty"`

	// Delivery bookkeeping (leaf 05-schedule/02-delivery-inbox). Both are
	// optional and only populated for jobs that deliver to a session inbox.
	LastDelivery    *ScheduleDeliveryReceipt `json:"last_delivery,omitempty"`
	DeliveryHistory *DeliveryHistory         `json:"delivery_history,omitempty"`
}

// AgentJobConfig holds configuration for agent jobs.
type AgentJobConfig struct {
	Prompt      string            `json:"prompt"`
	Context     map[string]string `json:"context,omitempty"`
	Model       string            `json:"model,omitempty"`
	AgentID     string            `json:"agent_id,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`

	// TargetSessionID pins delivery to a session inbox: when set, the
	// chat.request payload carries session_id (and a stable turn_id derived
	// from the delivery MessageID) and the scheduler tracks a durable
	// delivery receipt. Empty preserves the legacy fire-and-forget behavior.
	TargetSessionID string `json:"target_session_id,omitempty"`
}

// ShellJobConfig holds configuration for shell jobs.
type ShellJobConfig struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args,omitempty"`
	WorkDir    string            `json:"work_dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Timeout    time.Duration     `json:"timeout,omitempty"`
	CaptureOut bool              `json:"capture_output"`
	// GuardCommand is the command string checked by the execution-time
	// guard. When set, it MUST be the full shell source that command/args
	// will execute (job_start sets it to the user command when command is
	// /bin/sh and args are ["-c", <user command>]). Empty falls back to
	// Command for legacy shell jobs.
	//
	// TRUST BOUNDARY: GuardCommand is legacy persistence data, never
	// trusted over what actually executes. newShellJob derives the guard
	// source from command/args for the /bin/sh -c X wrapper shape and
	// ignores a mismatched field (warn-logged), so a persisted row cannot
	// present "echo harmless" to the guard while "evil" executes.
	GuardCommand string `json:"guard_command,omitempty"`
}

// shellWrapperScript detects the /bin/sh -c X wrapper shape (job_start's
// launch form) and returns the script X that actually executes. Everything
// else reports false: the guard source is the command itself.
func shellWrapperScript(command string, args []string) (string, bool) {
	if command != "/bin/sh" && command != "sh" && command != "/bin/bash" && command != "bash" {
		return "", false
	}
	if len(args) != 2 || args[0] != "-c" {
		return "", false
	}
	return args[1], true
}

// shellGuardSource derives the risk-classification source from command/args
// for EVERY shell-job shape (bughunt non-wrapper hardening): the wrapper
// shape classifies the actual script X; any other shape classifies the
// command+args joined with single spaces, matching the builtin shell tool's
// single-string classification. The persisted GuardCommand is never a
// source — only legacy rows carry it, and it may be stale or poisoned.
func shellGuardSource(command string, args []string) string {
	if script, ok := shellWrapperScript(command, args); ok {
		return script
	}
	if len(args) == 0 {
		return command
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, command)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

// ReminderJobConfig holds configuration for reminder jobs.
type ReminderJobConfig struct {
	Message     string     `json:"message"`
	Channels    []string   `json:"channels,omitempty"` // e.g., ["telegram", "notification"]
	Priority    string     `json:"priority,omitempty"` // low, normal, high, urgent
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
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      JobType    `json:"type"`
	Schedule  string     `json:"schedule"`
	Enabled   bool       `json:"enabled"`
	NextRun   *time.Time `json:"next_run,omitempty"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	RunCount  int64      `json:"run_count"`
	IsRunning bool       `json:"is_running"`
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

// IntervalJob is a persisted placeholder for the transient simpleIntervalJob
// created by employeeSchedulerAdapter. Its Execute is a no-op because the
// actual callback is re-registered at startup by the adapter.
type IntervalJob struct {
	baseJob
}

// Execute is a no-op — the real callback lives in simpleIntervalJob.
func (j *IntervalJob) Execute(_ context.Context) error { return nil }

// NewIntervalJob creates a placeholder job from persisted config.
func NewIntervalJob(cfg JobConfig) (*IntervalJob, error) {
	if cfg.Schedule == "" {
		cfg.Schedule = "@every 1h"
	}
	return &IntervalJob{
		id:       cfg.ID,
		name:     cfg.Name,
		schedule: cfg.Schedule,
		jobType:  cfg.Type,
		config:   cfg,
	}, nil
}

// baseJob provides common job functionality.
type baseJob struct {
	id       string
	name     string
	schedule string
	jobType  JobType
	config   JobConfig
}

func (b *baseJob) ID() string        { return b.id }
func (b *baseJob) Name() string      { return b.name }
func (b *baseJob) Schedule() string  { return b.schedule }
func (b *baseJob) Type() JobType     { return b.jobType }
func (b *baseJob) Config() JobConfig { return b.config }

// AgentJob triggers a chat request with a prompt.
type AgentJob struct {
	baseJob
	prompt  string
	context map[string]string
	model   string
	bus     *bus.MessageBus

	// deliveryScheduler is set by the scheduler when it owns this job so
	// Execute can persist delivery receipts around the publish. Agents
	// created standalone (tests, tooling) leave it nil and skip receipts.
	deliveryScheduler *Scheduler
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
		id:       cfg.ID,
		name:     cfg.Name,
		schedule: cfg.Schedule,
		jobType:  JobTypeAgent,
		config:   cfg,
		prompt:   cfg.AgentConfig.Prompt,
		context:  cfg.AgentConfig.Context,
		model:    cfg.AgentConfig.Model,
		bus:      msgBus,
	}, nil
}

// Execute sends a chat request to the agent via the message bus.
func (j *AgentJob) Execute(ctx context.Context) error {
	// No target session: legacy fire-and-forget dispatch with a fresh,
	// unpredictable turn id.
	if j.config.AgentConfig.TargetSessionID == "" {
		return j.deliver(ctx, id.Generate("turn-"))
	}

	// Session-pinned delivery: MessageID is minted per occurrence and becomes
	// the chat.request turn_id, so re-delivery (at-least-once recovery) is
	// idempotent. The scheduler records the receipt around this call.
	messageID := id.Generate("turn-")
	// The dispatch boundary (executeJob) passes the atomically-claimed tick
	// through the context. Stamping OccurrenceAt with it keeps hasReceiptForTick
	// (ms-precision claim↔receipt match) exact: a drifting stamp reads as
	// "claim without receipt" and the claimed-tick recovery sweep re-delivers
	// an occurrence that already has receipts, with a fresh turn id.
	occ := time.Now().UTC()
	if claimed, ok := ctx.Value(claimedTickKey{}).(time.Time); ok && !claimed.IsZero() {
		occ = claimed
	}
	if s := j.deliveryScheduler; s != nil {
		receipt := ScheduleDeliveryReceipt{
			ScheduleID:      j.id,
			ScheduledAt:     occ,
			OccurrenceAt:    occ,
			MessageID:       messageID,
			TargetSessionID: j.config.AgentConfig.TargetSessionID,
			Prompt:          j.prompt,
			Status:          DeliveryPending,
		}
		if err := s.recordDelivery(j.id, receipt); err != nil {
			return fmt.Errorf("failed to record pending delivery receipt: %w", err)
		}
		receipt.Status = DeliveryInFlight
		if err := s.recordDelivery(j.id, receipt); err != nil {
			return fmt.Errorf("failed to record in-flight delivery receipt: %w", err)
		}
	}

	if err := j.deliver(ctx, messageID); err != nil {
		// Receipt intentionally stays in_flight so recovery re-delivers it.
		return err
	}

	if s := j.deliveryScheduler; s != nil {
		receipt := ScheduleDeliveryReceipt{
			ScheduleID:      j.id,
			ScheduledAt:     occ,
			OccurrenceAt:    occ,
			DeliveredAt:     time.Now().UTC(),
			MessageID:       messageID,
			TargetSessionID: j.config.AgentConfig.TargetSessionID,
			Prompt:          j.prompt,
			Status:          DeliveryDelivered,
		}
		if err := s.recordDelivery(j.id, receipt); err != nil {
			return fmt.Errorf("failed to record delivered receipt: %w", err)
		}
	}
	return nil
}

// deliver publishes the chat.request with the given turn id. When a target
// session is configured the payload is pinned to that session's inbox via
// session_id; otherwise it is legacy fire-and-forget.
func (j *AgentJob) deliver(ctx context.Context, turnID string) error {
	// ChatRequest (internal/agent/handler.go) parses the turn text from
	// "message" — every other chat.request publisher (chat.submit RPC,
	// ChatService, MCP HTTP) uses that key. "prompt" is retained alongside
	// for legacy consumers; the handler ignores it.
	payload := map[string]any{
		"message":         j.prompt,
		"prompt":          j.prompt,
		"source":          "scheduler",
		SchedulerKeyJobID: j.id,
		"job_name":        j.name,
		"turn_id":         turnID,
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
	if target := j.config.AgentConfig.TargetSessionID; target != "" {
		payload["session_id"] = target
	}

	msg, err := models.NewBusMessage(models.MessageTypeRequest, "scheduler."+j.id, payload)
	if err != nil {
		return fmt.Errorf("failed to create bus message: %w", err)
	}

	// Publish to chat.request topic for the ChatHandler to pick up
	if j.bus == nil {
		return fmt.Errorf("message bus not available")
	}
	delivered := j.bus.Publish("chat.request", msg)
	if delivered == 0 {
		return fmt.Errorf("no subscribers for chat.request topic")
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
	// guardCommand is the shell source checked by the execution-time guard.
	// It differs from command only for /bin/sh -c launches (job_start).
	guardCommand string

	// guard is the two-phase execution-time fence. Nil (legacy construction
	// paths, e.g. standalone tests) skips guard checks; the daemon wires it
	// through JobDependencies so every scheduler-fired shell job is fenced.
	guard ShellCommandGuard
	// resultSink receives the settled result. Nil skips the result lane;
	// the daemon wires it through JobDependencies.
	resultSink ResultSink
}

// NewShellJob creates a new shell job. The bus-only form leaves the guard and
// result sink unset; use NewShellJobWithDeps to wire them.
func NewShellJob(cfg JobConfig, msgBus *bus.MessageBus) (*ShellJob, error) {
	return newShellJob(cfg, msgBus, nil, nil)
}

// NewShellJobWithDeps is NewShellJob with the execution-time guard and result
// sink wired from JobDependencies.
func NewShellJobWithDeps(cfg JobConfig, deps *JobDependencies) (*ShellJob, error) {
	if deps == nil {
		return newShellJob(cfg, nil, nil, nil)
	}
	return newShellJob(cfg, deps.Bus, deps.Guard, deps.ResultSink)
}

func newShellJob(cfg JobConfig, msgBus *bus.MessageBus, guard ShellCommandGuard, sink ResultSink) (*ShellJob, error) {
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

	// Guard-source derivation (guard-source-trust fix + non-wrapper
	// hardening, bughunt): the classification source is ALWAYS derived
	// from command/args — NEVER the persisted GuardCommand, which legacy
	// or poisoned rows may have stale, truncated, or attacker-supplied.
	//   - /bin/sh -c X wrapper shape: the actual script X;
	//   - any other shape: command + args joined with single spaces,
	//     matching the builtin shell tool's single-string classification
	//     (a row with command='curl', args=[...] must classify the FULL
	//     invocation, not just the binary name and not a stale
	//     guard_command='echo harmless').
	// A GuardCommand that mismatches the derived source is warn-logged
	// and ignored for EVERY shape (field kept for legacy JSON round-trips).
	guardSource := shellGuardSource(cfg.ShellConfig.Command, cfg.ShellConfig.Args)
	if gc := cfg.ShellConfig.GuardCommand; gc != "" && gc != guardSource {
		slog.Warn("scheduler: ignoring mismatched shell GuardCommand (derived from command/args)",
			"job_id", cfg.ID,
			"guard_command", gc,
			"derived_source_len", len(guardSource))
	}

	return &ShellJob{
		id:           cfg.ID,
		name:         cfg.Name,
		schedule:     cfg.Schedule,
		jobType:      JobTypeShell,
		config:       cfg,
		command:      cfg.ShellConfig.Command,
		guardCommand: guardSource,
		args:         cfg.ShellConfig.Args,
		workDir:      cfg.ShellConfig.WorkDir,
		env:          cfg.ShellConfig.Env,
		timeout:      timeout,
		capture:      cfg.ShellConfig.CaptureOut,
		bus:          msgBus,
		guard:        guard,
		resultSink:   sink,
	}, nil
}

// Execute runs the shell command.
//
// SECURITY NOTE: execution-time guarding is implemented here via the optional
// ShellCommandGuard (both phases run BEFORE the command is spawned). The
// creation-time fence check lives in tool_schedule_create.go; there is a
// TOCTOU window between creation and execution where the working directory or
// referenced files could change. The daemon wires the guard through
// JobDependencies so every scheduler-fired shell job is re-checked at
// execution time. Standalone construction (tests, tooling) leaves it nil and
// skips the check, preserving the legacy behavior.
func (j *ShellJob) Execute(ctx context.Context) error {
	// Phase 1 (risk classification / permission table) runs before any
	// work-dir resolution, preserving the 403-before-400 order.
	// Guard-only command source (the raw shell source behind /bin/sh -c) is
	// used exclusively for risk classification. It MUST NOT replace the
	// executable: keeping it would make exec.CommandContext try to run the
	// whole shell script as a binary name ("exec: printf ...: not found").
	guardSource := j.command
	if j.guardCommand != "" {
		guardSource = j.guardCommand
	}
	if j.guard != nil {
		if err := j.guard.CheckCommand(guardSource); err != nil {
			return j.settleGuardFailure(ctx, err)
		}
	}

	// Create timeout context (bounded by the caller's already-capped ctx).
	execCtx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	// Phase 2 (fence / Tirith / network class) runs after work-dir
	// resolution but before spawn. The guard owns the resolution semantics.
	if j.guard != nil {
		if err := j.guard.CheckCommandInDir(execCtx, guardSource, j.workDir); err != nil {
			return j.settleGuardFailure(ctx, err)
		}
	}

	// Spill output to a file, never an unbounded in-memory buffer. The dir
	// is created here so standalone construction works too; the daemon's
	// MEEPT_HOME/spill/tools/ sibling already exists in production.
	//
	// The file is wrapped in a byte-capped writer: a runaway job can no
	// longer write an unbounded spill file (disk-fill DoS). The cap marks
	// the settled result truncated (TruncatedTo) and drops the excess.
	spill, outputPath, err := prepareSpill(j.id)
	if err != nil {
		// A spill failure is a settled attempt failure, not silent loss.
		j.settle(JobExecutionResult{
			Status:     JobStatusFailed,
			ExitCode:   ExitCodeFailure,
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
			Error:      err.Error(),
		})
		return err
	}
	defer spill.Close()

	//nolint:gosec // validated input by the guard at execution time
	cmd := exec.CommandContext(execCtx, j.command, j.args...)
	if j.workDir != "" {
		cmd.Dir = j.workDir
	}
	if len(j.env) > 0 {
		env := make([]string, 0, len(j.env))
		for k, v := range j.env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = append(cmd.Environ(), env...)
	}
	// Both streams go through the byte-capped writer into the single spill
	// file (stdout then stderr interleaved as the process writes them). The
	// wrapper turns the direct-fd inheritance into a parent-side pipe copy —
	// which also makes WaitDelay authoritative for grandchildren holding the
	// output fd (with a bare *os.File a lingering grandchild is invisible to
	// Wait and the job leaks until its own timeout).
	capped := newCappedSpillWriter(spill, MaxSpillBytes)
	cmd.Stdout = capped
	cmd.Stderr = capped
	// Bound the pipe wait: after a kill, a grandchild that inherited the
	// output file descriptor cannot block Wait indefinitely (mirrors the
	// local runtime backend). Setpgid lets CancelJob kill the whole group.
	cmd.WaitDelay = 2 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Publish the command into the per-execution handle so CancelJob can
	// escalate from ctx cancel to a group kill. The handle is set by
	// executeJob when this is a handle-carrying run; nil is a no-op.
	if h, ok := ctx.Value(executionHandleKey{}).(*executionHandle); ok {
		h.setCmd(cmd)
	}

	startTime := time.Now().UTC()
	runErr := cmd.Run()
	finished := time.Now().UTC()

	exitCode := exitCodeFromRun(runErr)
	status := JobStatusSucceeded
	truncated := int64(0)
	if runErr != nil {
		status = JobStatusFailed
		if errors.Is(execCtx.Err(), context.Canceled) || errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			// A killed run is reported as cancelled with a unix kill code.
			status = JobStatusCancelled
		}
	}

	// ErrWaitDelay rescue: with the pipe-capped spill writer, a grandchild
	// that inherited the output fd can outlive the direct child, so Wait
	// returns exec.ErrWaitDelay ("wait: delayed") even though the child
	// itself already exited. When the execution context was NOT
	// cancelled/timed out, the child's exit state is the truth
	// (ProcessState is populated even when Wait returns ErrWaitDelay): a
	// job that exited 0 is a SUCCESS, not the generic failure that
	// exitCodeFromRun maps a bare ErrWaitDelay to. Mark the spill
	// truncated so readers know the tail may be missing. A cancelled or
	// timed-out run falls through to the mapping below.
	if errors.Is(runErr, exec.ErrWaitDelay) && status != JobStatusCancelled {
		if ps := cmd.ProcessState; ps != nil {
			exitCode = exitCodeFromProcessState(ps)
			if exitCode == ExitCodeOK {
				status = JobStatusSucceeded
			}
			runErr = nil
		}
		truncated = MaxSpillBytes
	}

	// A run killed by our cancel/escalation can surface from cmd.Run as a
	// non-*exec.ExitError: with WaitDelay set and a process-group kill, a
	// lingering grandchild (e.g. `sleep` under `/bin/sh -c`) can outlive the
	// direct child, so Wait returns exec.ErrWaitDelay ("wait: delayed") instead
	// of an *exec.ExitError carrying SIGKILL. exitCodeFromRun maps that generic
	// error to ExitCodeFailure (1), which would misreport a cancelled run as a
	// plain failure. When we KNOW the run was cancelled, map any non-ExitError
	// to the SIGKILL-equivalent code. A real *exec.ExitError is left untouched,
	// so normal exits (N>1) and real signal deaths keep their exact mapping.
	if status == JobStatusCancelled {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			exitCode = ExitCodeSIGKILL
		}
	}

	// Close before stats so the size/hash reflect all writes, then read a
	// bounded preview for the bus event (the event shape is frozen).
	_ = spill.Close()
	var outputBytes int64
	var outputSHA256 string
	outputBytes, outputSHA256, err = spillStats(outputPath)
	if err != nil {
		slog.Warn("scheduler: failed to stat job spill file", "job_id", j.id, "error", err)
	}
	preview := ""
	if j.capture {
		preview = readSpillPreview(outputPath, DefaultJobResultOutputBytes)
	}

	// Publish result event. Shape is unchanged: external subscribers on
	// scheduler.job.completed depend on the JobResult fields.
	result := JobResult{
		JobID:     j.id,
		StartTime: startTime,
		EndTime:   finished,
		Duration:  finished.Sub(startTime),
		Success:   runErr == nil,
		Output:    preview,
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	if j.bus != nil {
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+j.id, result)
		j.bus.PublishExternalOnly("scheduler.job.completed", msg)
	}

	// Write the settled attempt into the result lane (sink owns its own
	// locking; no I/O under a mutex here).
	j.settle(JobExecutionResult{
		Status:       status,
		ExitCode:     exitCode,
		StartedAt:    startTime,
		FinishedAt:   finished,
		OutputPath:   outputPath,
		OutputBytes:  outputBytes,
		OutputSHA256: outputSHA256,
		TruncatedTo:  truncated,
		Error:        result.Error,
	})

	// Terminal error is still returned to executeJob so the existing
	// UpdateLastRun / completion-event / logging paths behave as before.
	return runErr
}

// settleGuardFailure writes a settled failed result for a guard refusal
// (403 identity preserved by the guard error) and returns that error.
func (j *ShellJob) settleGuardFailure(ctx context.Context, guardErr error) error {
	now := time.Now().UTC()
	j.settle(JobExecutionResult{
		Status:     JobStatusFailed,
		ExitCode:   ExitCodeFailure,
		StartedAt:  now,
		FinishedAt: now,
		Error:      guardErr.Error(),
	})
	return guardErr
}

// settle writes a result to the sink when one is wired.
func (j *ShellJob) settle(result JobExecutionResult) {
	result.JobID = j.id
	if j.resultSink != nil {
		j.resultSink.Put(result)
	}
}

// exitCodeFromRun maps a cmd.Run error to a unix-consistent exit code.
// Non-zero process exits (N>1) are preserved verbatim; kill codes are
// normalized; an unset/signal death with no mapping is -1.
func exitCodeFromRun(runErr error) int {
	if runErr == nil {
		return ExitCodeOK
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				switch ws.Signal() {
				case syscall.SIGKILL:
					return ExitCodeSIGKILL
				case syscall.SIGTERM:
					return ExitCodeSIGTERM
				default:
					return ExitCodeUnknown
				}
			}
			if code := ws.ExitStatus(); code >= 0 {
				return code // preserves N>1 verbatim
			}
		}
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
		return ExitCodeUnknown
	}
	// Context kill/cancel from CommandContext surfaces as a signal death
	// (SIGKILL) on the ExitError path above; other errors (spawn failure,
	// etc.) are generic failures.
	return ExitCodeFailure
}

// exitCodeFromProcessState maps an os.ProcessState (the direct child's
// settled exit, available even when Wait returned exec.ErrWaitDelay) to a
// unix-consistent exit code using the same normalization as exitCodeFromRun.
func exitCodeFromProcessState(ps *os.ProcessState) int {
	if ps == nil {
		return ExitCodeUnknown
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			switch ws.Signal() {
			case syscall.SIGKILL:
				return ExitCodeSIGKILL
			case syscall.SIGTERM:
				return ExitCodeSIGTERM
			default:
				return ExitCodeUnknown
			}
		}
		if code := ws.ExitStatus(); code >= 0 {
			return code // preserves N>1 verbatim
		}
	}
	return ps.ExitCode()
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
		id:       cfg.ID,
		name:     cfg.Name,
		schedule: cfg.Schedule,
		jobType:  JobTypeReminder,
		config:   cfg,
		message:  cfg.ReminderConfig.Message,
		channels: channels,
		priority: priority,
		bus:      msgBus,
	}, nil
}

// Execute sends the reminder to configured channels.
func (j *ReminderJob) Execute(ctx context.Context) error {
	payload := map[string]any{
		"message":         j.message,
		SchedulerKeyJobID: j.id,
		"job_name":        j.name,
		"priority":        j.priority,
		"channels":        j.channels,
		"timestamp":       time.Now().UTC(),
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+j.id, payload)
	if err != nil {
		return fmt.Errorf("failed to create bus message: %w", err)
	}

	// Publish to each configured channel
	if j.bus == nil {
		return fmt.Errorf("message bus not available")
	}
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
	j.bus.PublishExternalOnly("scheduler.reminder", msg)

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
	UpdateMetricsFn func(ctx context.Context) error
	ConsolidateFn   func(ctx context.Context) error
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

	// Guard is the two-phase execution-time fence for shell jobs. Nil skips
	// guard checks (legacy construction paths and standalone tests).
	Guard ShellCommandGuard
	// ResultSink receives settled shell-job execution results (the result
	// lane). Nil skips the lane.
	ResultSink ResultSink
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
		id:       cfg.ID,
		name:     cfg.Name,
		schedule: cfg.Schedule,
		jobType:  JobTypeOptimization,
		config:   cfg,
		target:   target,
		deps:     deps,
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
		id:        cfg.ID,
		name:      cfg.Name,
		schedule:  cfg.Schedule,
		jobType:   JobTypeSecurity,
		config:    cfg,
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
			SchedulerKeyJobID: j.id,
			"issues":          issues,
			"issue_count":     len(issues),
			"scan_types":      j.scanTypes,
		}
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+j.id, payload)
		j.deps.Bus.PublishExternalOnly("scheduler.security.completed", msg)
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
		id:       cfg.ID,
		name:     cfg.Name,
		schedule: cfg.Schedule,
		jobType:  JobTypeLearning,
		config:   cfg,
		action:   action,
		deps:     deps,
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
	case JobTypeInterval:
		return NewIntervalJob(cfg)
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
		return NewShellJobWithDeps(cfg, deps)
	case JobTypeReminder:
		return NewReminderJob(cfg, deps.Bus)
	case JobTypeOptimization:
		return NewOptimizationJob(cfg, deps)
	case JobTypeSecurity:
		return NewSecurityJob(cfg, deps)
	case JobTypeLearning:
		return NewLearningJob(cfg, deps)
	case JobTypeInterval:
		return NewIntervalJob(cfg)
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
	// Schedule OR Timing (leaf 05-schedule/01-timing-kinds): a job may be
	// expressed either as a raw cron expression or as a semantic timing spec.
	if cfg.Schedule == "" && cfg.Timing == nil {
		return fmt.Errorf("job schedule or timing is required")
	}
	if cfg.Timing != nil {
		if err := cfg.Timing.Validate(); err != nil {
			return fmt.Errorf("invalid timing spec: %w", err)
		}
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
		CreatedAt string  `json:"created_at"`
		LastRunAt *string `json:"last_run_at,omitempty"`
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
