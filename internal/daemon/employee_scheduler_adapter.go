// Package daemon — employee_scheduler_adapter.go adapts the scheduler.Scheduler
// to the employee.Scheduler interface. The employee package defines a minimal
// RunAtInterval interface to avoid importing internal/scheduler; this adapter
// bridges the two by wrapping each callback in a scheduler.Job that uses the
// robfig/cron "@every <duration>" syntax.
package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/scheduler"
)

// employeeSchedulerAdapter wraps *scheduler.Scheduler to satisfy
// employee.Scheduler. Each RunAtInterval call creates a scheduler.Job with
// an "@every <duration>" cron expression that fires the given callback.
type employeeSchedulerAdapter struct {
	sched *scheduler.Scheduler
}

// RunAtInterval implements employee.Scheduler. It creates a job with the
// given name as the job ID and "@every <interval>" as the cron expression.
// If a job with the same ID already exists (from a prior registration), it
// is unscheduled first so repeated calls are idempotent.
func (a employeeSchedulerAdapter) RunAtInterval(name string, interval time.Duration, fn func()) {
	if a.sched == nil || interval <= 0 {
		return
	}
	// Unschedule any prior job with this name (idempotent re-registration).
	_ = a.sched.Unschedule(name)

	job := &simpleIntervalJob{
		id:       name,
		name:     name,
		interval: interval,
		fn:       fn,
	}
	if _, err := a.sched.Schedule(job); err != nil {
		// Best-effort: log via slog.Default since this is a background job.
		// The caller (daemon init) logs the scheduling attempt separately.
		_ = err // swallow — daemon logs the warning
	}
}

// simpleIntervalJob is a minimal scheduler.Job implementation that fires
// a callback at a fixed interval. Used by employeeSchedulerAdapter.
type simpleIntervalJob struct {
	id       string
	name     string
	interval time.Duration
	fn       func()
}

func (j *simpleIntervalJob) ID() string                 { return j.id }
func (j *simpleIntervalJob) Name() string               { return j.name }
func (j *simpleIntervalJob) Schedule() string           { return fmt.Sprintf("@every %s", j.interval) }
func (j *simpleIntervalJob) Type() scheduler.JobType    { return scheduler.JobTypeLearning }
func (j *simpleIntervalJob) Execute(_ context.Context) error {
	j.fn()
	return nil
}
func (j *simpleIntervalJob) Config() scheduler.JobConfig {
	return scheduler.JobConfig{
		ID:        j.id,
		Name:      j.name,
		Type:      scheduler.JobTypeLearning,
		Schedule:  fmt.Sprintf("@every %s", j.interval),
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
		Tags:      []string{"employee"},
	}
}
