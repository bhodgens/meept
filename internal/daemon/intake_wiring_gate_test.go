package daemon

// NOTE: gate tests for the github-intake-factory tree (leaf 00T) — F7.
// Intentionally RED (compile-level): wireIntakePoller does not exist yet;
// it lands in leaf 05 on the proven employeeSchedulerAdapter.RunAtInterval
// path. This file lives in package daemon because the wiring seam IS
// daemon-internal (Components + scheduler); every other daemon test file
// compiles untouched — the only failure is the undefined wireIntakePoller
// symbol referenced below. Written to compile cleanly once leaf 05 lands,
// at which point these tests go GREEN without modification.
//
// Contract: a poller job ("intake_poller") is scheduled ONLY when
// [intake].enabled=true; with no [intake] block (the shipped default) no
// intake job may be registered with the scheduler.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
)

// newWiringScheduler builds a real scheduler on a temp data dir — the same
// construction the daemon's components use, minus the HTTP surface.
func newWiringScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	s, err := scheduler.NewScheduler(
		config.SchedulerConfig{Enabled: true},
		bus.New(bus.DefaultConfig(), slog.New(slog.DiscardHandler)),
		scheduler.WithDataDir(t.TempDir()),
		scheduler.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

// intakeWiringComponents is the minimal Components fixture the wiring
// function reads: config + scheduler + logger.
func intakeWiringComponents(t *testing.T, cfg *config.Config) *Components {
	t.Helper()
	return &Components{
		Config:    cfg,
		Scheduler: newWiringScheduler(t),
		Logger:    slog.New(slog.DiscardHandler),
	}
}

// TestIntakeWiring_Enabled: [intake].enabled=true schedules exactly one
// intake_poller job on the scheduler (F7, leaf 05).
func TestIntakeWiring_Enabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Intake = config.IntakeConfig{
		Enabled:         true,
		DefaultEmployee: "factory-bot",
		PollInterval:    "5m",
		WatchRepo:       "octo/forge",
	}
	c := intakeWiringComponents(t, cfg)

	wireIntakePoller(c)

	jobs := c.Scheduler.ListJobs()
	var found *scheduler.JobInfo
	for i := range jobs {
		if jobs[i].ID == "intake_poller" {
			found = &jobs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no intake_poller job scheduled with [intake] enabled; jobs: %v", jobs)
	}
	// The job must be a real interval job: @every with the configured
	// poll interval.
	if found.Schedule != "@every 5m0s" {
		t.Errorf("intake_poller schedule = %q, want @every 5m0s (the configured poll_interval)", found.Schedule)
	}
}

// TestIntakeWiring_DisabledDefault: the shipped default (no [intake]
// block) schedules NO intake job (F7).
func TestIntakeWiring_DisabledDefault(t *testing.T) {
	cfg := config.DefaultConfig() // Intake zero value: Enabled=false
	if cfg.Intake.Enabled {
		t.Fatal("precondition: [intake] must default to disabled")
	}
	c := intakeWiringComponents(t, cfg)

	wireIntakePoller(c)

	for _, j := range c.Scheduler.ListJobs() {
		if j.ID == "intake_poller" {
			t.Fatalf("intake_poller scheduled with [intake] disabled; want no intake surfaces")
		}
	}

	// An explicit poll interval of 0 (unparseable/empty default) must also
	// never produce a zero-interval job.
	cfg.Intake.Enabled = true
	cfg.Intake.DefaultEmployee = "factory-bot"
	cfg.Intake.WatchRepo = "octo/forge"
	cfg.Intake.PollInterval = "bogus"
	c2 := intakeWiringComponents(t, cfg)
	wireIntakePoller(c2) // must log-and-skip, never panic
	_ = time.Second
}
