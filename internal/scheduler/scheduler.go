package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/pkg/models"
	"github.com/robfig/cron/v3"
)

// Scheduler wraps robfig/cron with job management and persistence.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Scheduler struct {
	cron    *cron.Cron
	store   *Store
	bus     *bus.MessageBus
	logger  *slog.Logger
	cfg     config.SchedulerConfig
	dataDir string
	jobDeps *JobDependencies

	mu          sync.RWMutex
	jobs        map[string]Job
	entryIDs    map[string]cron.EntryID // job ID -> cron entry ID
	runningJobs map[string]bool         // job ID -> is running
	running     atomic.Bool
	location    *time.Location

	// RunNow tracking
	runNowCtx    context.Context
	runNowCancel context.CancelFunc
	runNowWg     sync.WaitGroup
}

// Option is a functional option for configuring the Scheduler.
type Option func(*Scheduler) error

// WithDataDir sets the data directory for job persistence.
func WithDataDir(dir string) Option {
	return func(s *Scheduler) error {
		s.dataDir = dir
		return nil
	}
}

// WithLogger sets the logger for the scheduler.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Scheduler) error {
		s.logger = logger
		return nil
	}
}

// WithJobDependencies sets the job dependencies for extended job types
// (optimization, security, learning).
func WithJobDependencies(deps *JobDependencies) Option {
	return func(s *Scheduler) error {
		s.jobDeps = deps
		return nil
	}
}

// NewScheduler creates a new Scheduler instance.
func NewScheduler(cfg config.SchedulerConfig, msgBus *bus.MessageBus, opts ...Option) (*Scheduler, error) {
	// Parse timezone
	loc := time.UTC
	if cfg.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(cfg.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
		}
	}

	s := &Scheduler{
		bus:         msgBus,
		logger:      slog.Default(),
		cfg:         cfg,
		jobs:        make(map[string]Job),
		entryIDs:    make(map[string]cron.EntryID),
		runningJobs: make(map[string]bool),
		location:    loc,
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	// Create store
	store, err := NewStore(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}
	s.store = store

	// Create cron scheduler with options
	cronOpts := []cron.Option{
		cron.WithLocation(loc),
		cron.WithSeconds(), // Enable 6-field cron expressions
		cron.WithParser(cron.NewParser(
			cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		)),
		cron.WithChain(
			cron.Recover(cron.DefaultLogger),
			cron.SkipIfStillRunning(cron.DefaultLogger),
		),
	}
	s.cron = cron.New(cronOpts...)

	return s, nil
}

// Name implements registry.Component.
func (s *Scheduler) Name() string {
	return "scheduler"
}

// Running implements registry.Component.
func (s *Scheduler) Running() bool {
	return s.running.Load()
}

// Start starts the scheduler and loads persisted jobs.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.running.Load() {
		return fmt.Errorf("scheduler already running")
	}

	s.logger.Info("scheduler: starting", "timezone", s.location.String())

	// Create shutdown-aware context for RunNow jobs. Derive from the
	// caller-provided ctx so that RunNow jobs are cancelled when the
	// parent context (e.g. daemon shutdown) is cancelled, rather than
	// running detached (S6-17).
	s.mu.Lock()
	s.runNowCtx, s.runNowCancel = context.WithCancel(ctx)
	s.mu.Unlock()

	// Load persisted jobs
	if err := s.loadPersistedJobs(); err != nil {
		s.logger.Warn("scheduler: failed to load persisted jobs", "error", err)
	}

	// Start cron scheduler
	s.cron.Start()
	s.running.Store(true)

	// Publish startup event
	if s.bus != nil {
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler", map[string]any{
			SchedulerKeyEvent: "started",
			"jobs":            len(s.jobs),
			"timezone":        s.location.String(),
		})
		s.bus.Publish("scheduler.started", msg)
	}

	s.logger.Info("scheduler: started", "jobs", len(s.jobs))
	return nil
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop(ctx context.Context) error {
	if !s.running.Load() {
		return nil
	}

	s.logger.Info("scheduler: stopping")

	// Stop accepting new jobs. Acquire the scheduler lock while flipping
	// the running flag and capturing the WaitGroup/cancel so that a
	// concurrent RunNow cannot observe running=true, then add to the
	// WaitGroup after we've already called Wait() below.
	s.mu.Lock()
	s.running.Store(false)
	runNowCancel := s.runNowCancel
	s.mu.Unlock()

	// Cancel in-flight RunNow jobs and wait for them
	if runNowCancel != nil {
		runNowCancel()
	}
	s.runNowWg.Wait()

	// Stop cron scheduler and wait for running jobs
	cronCtx := s.cron.Stop()

	// Wait for running jobs with timeout (no extra goroutine needed).
	select {
	case <-cronCtx.Done():
		s.logger.Debug("scheduler: all jobs completed")
	case <-ctx.Done():
		s.logger.Warn("scheduler: shutdown timeout, some jobs may not have completed")
	}

	// Publish shutdown event
	if s.bus != nil {
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler", map[string]any{
			SchedulerKeyEvent: "stopped",
		})
		s.bus.Publish("scheduler.stopped", msg)
	}

	s.logger.Info("scheduler: stopped")
	return nil
}

// Schedule adds a job to the scheduler.
func (s *Scheduler) Schedule(job Job) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobID := job.ID()

	// Check if job already exists
	if _, exists := s.jobs[jobID]; exists {
		return "", fmt.Errorf("job already exists: %s", jobID)
	}

	// Validate schedule expression
	schedule := job.Schedule()
	if _, err := s.parseSchedule(schedule); err != nil {
		return "", fmt.Errorf("invalid schedule expression: %w", err)
	}

	// Add to cron
	entryID, err := s.cron.AddFunc(schedule, s.wrapJob(job))
	if err != nil {
		return "", fmt.Errorf("failed to add job to cron: %w", err)
	}

	// Store job
	s.jobs[jobID] = job
	s.entryIDs[jobID] = entryID

	// Persist job config
	cfg := job.Config()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = time.Now().UTC()
	}
	if err := s.store.Add(cfg); err != nil {
		s.logger.Warn("scheduler: failed to persist job", "job_id", jobID, "error", err)
	}

	s.logger.Info("scheduler: job scheduled",
		"job_id", jobID,
		"name", job.Name(),
		"schedule", schedule,
	)

	return jobID, nil
}

// ScheduleConfig creates and schedules a job from a JobConfig.
func (s *Scheduler) ScheduleConfig(cfg JobConfig) (string, error) {
	// Validate config
	if err := ValidateJobConfig(cfg); err != nil {
		return "", err
	}

	// Create job using deps if available (needed for extended job types)
	job, err := s.createJob(cfg)
	if err != nil {
		return "", err
	}

	return s.Schedule(job)
}

// Unschedule removes a job from the scheduler.
func (s *Scheduler) Unschedule(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID, ok := s.entryIDs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Remove from cron
	s.cron.Remove(entryID)

	// Remove from maps
	delete(s.jobs, jobID)
	delete(s.entryIDs, jobID)
	delete(s.runningJobs, jobID)

	// Remove from persistence
	if err := s.store.Remove(jobID); err != nil {
		s.logger.Warn("scheduler: failed to remove job from store", "job_id", jobID, "error", err)
	}

	s.logger.Info("scheduler: job unscheduled", "job_id", jobID)
	return nil
}

// RunNow triggers immediate execution of a job.
func (s *Scheduler) RunNow(jobID string) error {
	// Quick check without lock to avoid contention in the common case.
	if !s.running.Load() {
		return fmt.Errorf("scheduler not running")
	}

	s.mu.Lock()
	// Re-check running under the lock to defend against a race where
	// Stop() ran between the atomic check above and acquiring the lock.
	// Without this, runNowWg.Add could happen after Stop's runNowWg.Wait
	// returns, leaking a goroutine past shutdown.
	if !s.running.Load() {
		s.mu.Unlock()
		return fmt.Errorf("scheduler not running")
	}
	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("job not found: %s", jobID)
	}
	if s.runningJobs[jobID] {
		s.mu.Unlock()
		return fmt.Errorf("job already running: %s", jobID)
	}
	s.runningJobs[jobID] = true
	// Add to the WaitGroup under the lock so that a concurrent Stop()
	// cannot return from runNowWg.Wait() before this Add is observed.
	s.runNowWg.Add(1)
	runNowCtx := s.runNowCtx
	s.mu.Unlock()

	// Run job in goroutine with shutdown-aware context
	go func() {
		defer s.runNowWg.Done()
		ctx, cancel := context.WithTimeout(runNowCtx, 30*time.Minute)
		defer cancel()

		s.executeJob(ctx, job)
	}()

	s.logger.Info("scheduler: job triggered manually", "job_id", jobID)
	return nil
}

// ListJobs returns information about all scheduled jobs.
func (s *Scheduler) ListJobs() []JobInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]JobInfo, 0, len(s.jobs))
	for jobID, job := range s.jobs {
		info := JobInfo{
			ID:        jobID,
			Name:      job.Name(),
			Type:      job.Type(),
			Schedule:  job.Schedule(),
			Enabled:   true,
			IsRunning: s.runningJobs[jobID],
		}

		// Get next run time from cron
		if entryID, ok := s.entryIDs[jobID]; ok {
			entry := s.cron.Entry(entryID)
			if !entry.Next.IsZero() {
				next := entry.Next
				info.NextRun = &next
			}
		}

		// Get last run info from store
		if cfg, ok := s.store.Get(jobID); ok {
			info.LastRun = cfg.LastRunAt
			info.LastError = cfg.LastError
			info.RunCount = cfg.RunCount
			info.Enabled = cfg.Enabled
		}

		infos = append(infos, info)
	}

	return infos
}

// GetJob returns information about a specific job.
func (s *Scheduler) GetJob(jobID string) (JobInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return JobInfo{}, false
	}

	info := JobInfo{
		ID:        jobID,
		Name:      job.Name(),
		Type:      job.Type(),
		Schedule:  job.Schedule(),
		Enabled:   true,
		IsRunning: s.runningJobs[jobID],
	}

	if entryID, ok := s.entryIDs[jobID]; ok {
		entry := s.cron.Entry(entryID)
		if !entry.Next.IsZero() {
			next := entry.Next
			info.NextRun = &next
		}
	}

	if cfg, ok := s.store.Get(jobID); ok {
		info.LastRun = cfg.LastRunAt
		info.LastError = cfg.LastError
		info.RunCount = cfg.RunCount
		info.Enabled = cfg.Enabled
	}

	return info, true
}

// PauseJob pauses a job (removes from cron but keeps in store).
func (s *Scheduler) PauseJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID, ok := s.entryIDs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Remove from cron
	s.cron.Remove(entryID)
	delete(s.entryIDs, jobID)

	// Update store
	if err := s.store.SetEnabled(jobID, false); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	s.logger.Info("scheduler: job paused", "job_id", jobID)
	return nil
}

// ResumeJob resumes a paused job.
func (s *Scheduler) ResumeJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	// Check if already scheduled
	if _, scheduled := s.entryIDs[jobID]; scheduled {
		return fmt.Errorf("job already running: %s", jobID)
	}

	// Add back to cron
	entryID, err := s.cron.AddFunc(job.Schedule(), s.wrapJob(job))
	if err != nil {
		return fmt.Errorf("failed to add job to cron: %w", err)
	}

	s.entryIDs[jobID] = entryID

	// Update store
	if err := s.store.SetEnabled(jobID, true); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	s.logger.Info("scheduler: job resumed", "job_id", jobID)
	return nil
}

// JobCount returns the number of scheduled jobs.
func (s *Scheduler) JobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// wrapJob creates a function wrapper for the job that handles execution tracking.
func (s *Scheduler) wrapJob(job Job) func() {
	return func() {
		// Derive from runNowCtx so shutdown can signal cancellation,
		// rather than using detached context.Background().
		baseCtx := s.runNowCtx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(baseCtx, 30*time.Minute)
		defer cancel()

		s.executeJob(ctx, job)
	}
}

// executeJob runs the job and tracks execution state.
func (s *Scheduler) executeJob(ctx context.Context, job Job) {
	jobID := job.ID()

	// Mark as running
	s.mu.Lock()
	s.runningJobs[jobID] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.runningJobs, jobID)
		s.mu.Unlock()
	}()

	startTime := time.Now()

	// Publish job start event
	if s.bus != nil {
		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+jobID, map[string]any{
			SchedulerKeyEvent: "job_started",
			SchedulerKeyJobID: jobID,
			"name":            job.Name(),
			"type":            job.Type(),
		})
		s.bus.Publish("scheduler.job.started", msg)
	}

	// Execute job
	err := job.Execute(ctx)
	duration := time.Since(startTime)

	// Update store with run result
	if storeErr := s.store.UpdateLastRun(jobID, startTime, err); storeErr != nil {
		s.logger.Warn("scheduler: failed to update job run status",
			"job_id", jobID,
			"error", storeErr,
		)
	}

	// Publish completion event
	if s.bus != nil {
		result := map[string]any{
			SchedulerKeyEvent:   "job_completed",
			SchedulerKeyJobID:   jobID,
			"name":              job.Name(),
			"type":              job.Type(),
			"duration":          duration.String(),
			SchedulerKeySuccess: err == nil,
		}
		if err != nil {
			result["error"] = err.Error()
		}

		msg, _ := models.NewBusMessage(models.MessageTypeEvent, "scheduler."+jobID, result)
		s.bus.Publish("scheduler.job.completed", msg)
	}

	if err != nil {
		s.logger.Error("scheduler: job failed",
			"job_id", jobID,
			"name", job.Name(),
			"duration", duration,
			"error", err,
		)
	} else {
		s.logger.Debug("scheduler: job completed",
			"job_id", jobID,
			"name", job.Name(),
			"duration", duration,
		)
	}
}

// loadPersistedJobs loads jobs from the persistent store.
func (s *Scheduler) loadPersistedJobs() error {
	jobs, err := s.store.Load()
	if err != nil {
		return err
	}

	if len(jobs) == 0 {
		return nil
	}

	s.logger.Debug("scheduler: loading persisted jobs", "count", len(jobs))

	for _, cfg := range jobs {
		if !cfg.Enabled {
			s.logger.Debug("scheduler: skipping disabled job", "job_id", cfg.ID)
			continue
		}

		job, err := s.createJob(cfg)
		if err != nil {
			s.logger.Warn("scheduler: failed to create job from config",
				"job_id", cfg.ID,
				"error", err,
			)
			continue
		}

		// Add to cron
		entryID, err := s.cron.AddFunc(cfg.Schedule, s.wrapJob(job))
		if err != nil {
			s.logger.Warn("scheduler: failed to schedule job",
				"job_id", cfg.ID,
				"schedule", cfg.Schedule,
				"error", err,
			)
			continue
		}

		s.jobs[cfg.ID] = job
		s.entryIDs[cfg.ID] = entryID

		s.logger.Debug("scheduler: loaded job",
			"job_id", cfg.ID,
			"name", cfg.Name,
			"schedule", cfg.Schedule,
		)
	}

	return nil
}

// createJob creates a job from config, using JobDependencies if available.
func (s *Scheduler) createJob(cfg JobConfig) (Job, error) {
	if s.jobDeps != nil {
		return CreateJobWithDeps(cfg, s.jobDeps)
	}
	return CreateJob(cfg, s.bus)
}

// parseSchedule validates and parses a schedule expression.
func (s *Scheduler) parseSchedule(spec string) (cron.Schedule, error) {
	parser := cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	return parser.Parse(spec)
}

// ValidateSchedule checks if a schedule expression is valid.
func (s *Scheduler) ValidateSchedule(spec string) error {
	_, err := s.parseSchedule(spec)
	return err
}

// NextRun returns the next scheduled run time for a job.
func (s *Scheduler) NextRun(jobID string) (time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entryID, ok := s.entryIDs[jobID]
	if !ok {
		return time.Time{}, fmt.Errorf("job not found: %s", jobID)
	}

	entry := s.cron.Entry(entryID)
	return entry.Next, nil
}

// Location returns the scheduler's timezone location.
func (s *Scheduler) Location() *time.Location {
	return s.location
}

// Store returns the underlying persistence store.
func (s *Scheduler) Store() *Store {
	return s.store
}

// Bus returns the message bus.
func (s *Scheduler) Bus() *bus.MessageBus {
	return s.bus
}
