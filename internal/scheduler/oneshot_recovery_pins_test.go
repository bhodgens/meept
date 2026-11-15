package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// oneShotJobConfig builds an enabled shell one-shot job config due at `at`.
func oneShotJobConfig(id string, at time.Time) JobConfig {
	return JobConfig{
		ID:      id,
		Name:    id,
		Type:    JobTypeShell,
		Enabled: true,
		Timing:  &TimingSpec{Kind: TimingAt, At: &at},
		ShellConfig: &ShellJobConfig{
			Command: "true",
		},
	}
}

// startTestScheduler builds and starts a scheduler in a fresh temp dir,
// returning it with a guaranteed Stop in cleanup.
func startTestScheduler(t *testing.T, cfg config.SchedulerConfig, opts ...Option) *Scheduler {
	t.Helper()
	s, err := NewScheduler(cfg, nil, append([]Option{WithDataDir(t.TempDir())}, opts...)...)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

// Pin 1: a one-shot due 10 minutes ago whose daemon was down at the due
// instant fires on the next boot ("run at T" = run as soon as possible
// after T): the row is claimed at the due tick and pruned from jobs.json
// after the successful run.
func TestOneShotOverdueWithinWindowFiresOnBoot(t *testing.T) {
	dataDir := t.TempDir()

	// Pre-crash state: the row is persisted with Schedule='' (the defect
	// posture) and the daemon never fired it.
	due := time.Now().UTC().Add(-10 * time.Minute)
	seed, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(dataDir))
	if err != nil {
		t.Fatalf("seed NewScheduler failed: %v", err)
	}
	if err := seed.store.Add(oneShotJobConfig("oneshot-late", due)); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}
	_ = seed.claims.close()

	// Boot the "next daemon instance" over the same data dir.
	s := startTestScheduler(t, config.SchedulerConfig{Timezone: "UTC"}, WithDataDir(dataDir))

	// Wait for the boot sweep's dispatch to complete: a successful run
	// (shell `true`) prunes the row.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := s.store.Get("oneshot-late"); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := s.store.Get("oneshot-late"); ok {
		t.Fatal("overdue one-shot (10min past, 2h window) did not fire on boot: row still persisted")
	}
	// The due instant must have been claimed (the claim-before-deliver
	// boundary ran for the due tick, not some fabricated now-tick).
	claims, err := s.claims.ListClaimsSince(due.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListClaimsSince failed: %v", err)
	}
	foundDueClaim := false
	for _, c := range claims {
		if c.JobID == "oneshot-late" && c.Tick.Sub(due).Abs() < time.Minute {
			foundDueClaim = true
		}
	}
	if !foundDueClaim {
		t.Fatalf("due tick %s not claimed on boot; claims=%v", due, claims)
	}
}

// Pin 2: a one-shot due 5 hours ago (window 2h) is marked failed
// (never-ran, re-runnable) at boot and does NOT fire.
func TestOneShotBeyondWindowMarkedFailedNotFired(t *testing.T) {
	due := time.Now().UTC().Add(-5 * time.Hour)

	// Persist the row the way a pre-crash daemon would, then boot.
	s, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	cfg := oneShotJobConfig("oneshot-too-late", due)
	if err := s.store.Add(cfg); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// Must be marked failed.
	got, ok := s.store.Get("oneshot-too-late")
	if !ok {
		t.Fatal("failed one-shot row was pruned; expected it kept re-runnable")
	}
	if !got.OneShotFailed {
		t.Error("OneShotFailed not set on past-window one-shot")
	}
	if got.LastError == "" {
		t.Error("LastError empty on failed one-shot")
	}
	if got.OneShotFiredAt != nil {
		t.Error("OneShotFiredAt set on a job that never ran")
	}

	// Must NOT have fired: not in the runningJobs map after settle, and the
	// cron engine carries no entry for it.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.RLock()
	_, cronRegistered := s.entryIDs["oneshot-too-late"]
	_, inJobs := s.jobs["oneshot-too-late"]
	s.mu.RUnlock()
	if cronRegistered {
		t.Error("failed one-shot registered on cron; must stop looking schedulable")
	}
	if !inJobs {
		t.Error("failed one-shot missing from jobs map; must stay re-runnable via RunNow")
	}
}

// Pin 3: RunNow on a failed one-shot executes it and clears the failed
// state.
func TestRunNowClearsOneShotFailedState(t *testing.T) {
	due := time.Now().UTC().Add(-5 * time.Hour)
	s, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	job := newCountingJob("oneshot-failed-rerun", "")
	cfg := oneShotJobConfig(job.ID(), due)
	cfg.OneShotFailed = true
	cfg.LastError = "one-shot never ran"
	if err := s.store.Add(cfg); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}

	s.mu.Lock()
	s.jobs[job.ID()] = job // simulate the counting job AFTER Start: loadPersistedJobs
	s.mu.Unlock()          // rebuilds the jobs map from the store during Start
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	// loadPersistedJobs put its own shell job in the map; swap in the
	// counting job so the execution is observable.
	s.mu.Lock()
	s.jobs[job.ID()] = job
	s.mu.Unlock()

	if err := s.RunNow(job.ID()); err != nil {
		t.Fatalf("RunNow on failed one-shot: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && job.runCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if job.runCount() == 0 {
		t.Fatal("RunNow on failed one-shot did not execute it")
	}

	got, ok := s.store.Get(job.ID())
	if !ok {
		t.Fatal("row pruned by RunNow; expected it kept")
	}
	if got.OneShotFailed {
		t.Error("OneShotFailed still set after RunNow re-run")
	}
}

// Pin 4: scheduling a one-shot more than 2h in the future is rejected at
// schedule time with an error naming the window.
func TestTimingAtBeyondFutureHorizonRejected(t *testing.T) {
	s, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	far := time.Now().UTC().Add(3 * time.Hour)
	_, err = s.ScheduleConfig(oneShotJobConfig("oneshot-far-future", far))
	if err == nil {
		t.Fatal("TimingAt 3h in future accepted; want rejection")
	}
	if !errors.Is(err, ErrOneShotBeyondHorizon) {
		t.Fatalf("error %v does not wrap ErrOneShotBeyondHorizon", err)
	}
	if !containsWindow(err.Error(), "2h") {
		t.Errorf("error %q does not name the 2h window", err.Error())
	}
	// The rejected row must not persist.
	if _, ok := s.store.Get("oneshot-far-future"); ok {
		t.Error("rejected one-shot row persisted")
	}
	// Within-horizon scheduling still works.
	near := time.Now().UTC().Add(30 * time.Minute)
	if _, err := s.ScheduleConfig(oneShotJobConfig("oneshot-near-future", near)); err != nil {
		t.Fatalf("TimingAt 30min in future rejected: %v", err)
	}
}

// Pin 5: the knob override (30min window) is honored in both directions —
// a 45min-past one-shot is failed (outside the smaller window) and a 3h
// future one-shot is rejected with the 30min window named.
func TestOneShotWindowKnobOverride(t *testing.T) {
	cfg := config.SchedulerConfig{Timezone: "UTC", OneShotWindow: "30m"}
	if got := cfg.EffectiveOneShotWindow(); got != 30*time.Minute {
		t.Fatalf("EffectiveOneShotWindow() = %v, want 30m", got)
	}

	// Past direction: 45min overdue is OUTSIDE a 30m window -> failed.
	s, err := NewScheduler(cfg, nil, WithDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	due := time.Now().UTC().Add(-45 * time.Minute)
	if err := s.store.Add(oneShotJobConfig("oneshot-45min-past", due)); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	got, ok := s.store.Get("oneshot-45min-past")
	if !ok || !got.OneShotFailed {
		t.Fatalf("45min-past one-shot not marked failed under 30m window (row ok=%v)", ok)
	}

	// Future direction: 40min ahead is beyond a 30m window -> rejected,
	// naming the 30m window.
	future := time.Now().UTC().Add(40 * time.Minute)
	_, err = s.ScheduleConfig(oneShotJobConfig("oneshot-40min-future", future))
	if !errors.Is(err, ErrOneShotBeyondHorizon) {
		t.Fatalf("40min-future one-shot not rejected under 30m window: %v", err)
	}
	if !containsWindow(err.Error(), "30m") {
		t.Errorf("error %q does not name the 30m window", err.Error())
	}
}

// TestOneShotWindowZeroValueDefault pins the knob contract: the zero-value
// SchedulerConfig resolves to the 2h default.
func TestOneShotWindowZeroValueDefault(t *testing.T) {
	if got := (config.SchedulerConfig{}).EffectiveOneShotWindow(); got != config.DefaultOneShotWindow {
		t.Fatalf("zero-value window = %v, want %v", got, config.DefaultOneShotWindow)
	}
	if got := (config.SchedulerConfig{OneShotWindow: "bogus"}).EffectiveOneShotWindow(); got != config.DefaultOneShotWindow {
		t.Fatalf("unparseable window = %v, want %v", got, config.DefaultOneShotWindow)
	}
}

func containsWindow(msg, want string) bool {
	return len(msg) >= len(want) && (func() bool {
		for i := 0; i+len(want) <= len(msg); i++ {
			if msg[i:i+len(want)] == want {
				return true
			}
		}
		return false
	})()
}

var _ = filepath.Join // keep filepath import if test bodies change
