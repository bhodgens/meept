package scheduler

import (
	"context"
	"errors"
	"os"
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

// timingAwareCountingJob is a countingJob whose Config() carries the
// TimingAt spec: RunNow's one-shot stamp path (S5) consults job.Config()
// from the jobs map, so a plain countingJob (no Timing) would skip it.
type timingAwareCountingJob struct {
	countingJob
	at time.Time
}

func newTimingAwareCountingJob(id string, at time.Time) *timingAwareCountingJob {
	return &timingAwareCountingJob{id: id, runs: make(chan string, 100), at: at}
}

func (j *timingAwareCountingJob) Config() JobConfig {
	return JobConfig{
		ID:      j.id,
		Name:    j.id,
		Type:    JobTypeShell,
		Enabled: true,
		Timing:  &TimingSpec{Kind: TimingAt, At: &j.at},
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

// newStartedScheduler starts a scheduler rooted in the GIVEN data dir (used
// for restart-over-same-dir pins) with a guaranteed Stop in cleanup.
func newStartedScheduler(t *testing.T, cfg config.SchedulerConfig, opts ...Option) *Scheduler {
	t.Helper()
	s, err := NewScheduler(cfg, nil, opts...)
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

// Pin 6 (S2): a FUTURE-due one-shot row present at boot is NOT fired early
// by sweepOneShotsAtBoot — future dues belong to the live cron entry, which
// fires at the due instant. Only past dues are swept.
func TestOneShotFutureDueNotFiredAtBoot(t *testing.T) {
	dataDir := t.TempDir()
	due := time.Now().UTC().Add(2 * time.Second)

	// The observable is a shell job whose command touches a marker file:
	// loadPersistedJobs rebuilds the SAME shell command from the persisted
	// row on restart, so the live cron entry's delivery stays observable
	// across the boot boundary without any test-side job swapping (the
	// swap would be invisible anyway — the reload-registered closure
	// delivers to the job instance it captured).
	marker := filepath.Join(dataDir, "oneshot-future-marker")
	cfg := oneShotJobConfig("oneshot-future", due)
	cfg.ShellConfig = &ShellJobConfig{Command: "/usr/bin/touch", Args: []string{marker}}

	// Persist the row the way a pre-crash daemon would (never fired), then
	// tear the seed down WITHOUT Start so the claims db survives.
	seed, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(dataDir))
	if err != nil {
		t.Fatalf("seed NewScheduler failed: %v", err)
	}
	if err := seed.store.Add(cfg); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}
	_ = seed.claims.close()

	// Boot the "next daemon instance" over the same data dir; its
	// loadPersistedJobs re-registers the future-due cron entry.
	s := newStartedScheduler(t, config.SchedulerConfig{Timezone: "UTC"}, WithDataDir(dataDir))

	// Give the boot sweep ample time to (wrongly) fire the future row.
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("future-due one-shot fired at boot (early): marker exists, want absent")
	}
	// The row must be untouched by the sweep.
	got, ok := s.store.Get("oneshot-future")
	if !ok {
		t.Fatal("future-due one-shot row pruned by boot sweep")
	}
	if got.OneShotFiredAt != nil || got.OneShotFailed {
		t.Error("future-due one-shot row stamped/marked by boot sweep")
	}

	// The live cron entry still fires at the due instant.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("cron entry did not fire the future-due one-shot at due time")
	}
}

// Pin 7 (S1): a cron-fired one-shot must not re-fire when the next daemon
// instance boots over the same data dir — the cron fire goes through the
// shared fireOneShotOnce path (claim + stamp + prune), so the boot sweep
// finds nothing to claim. The job executes EXACTLY once across both runs.
func TestCronFiredOneShotNotRefiredOnBoot(t *testing.T) {
	dataDir := t.TempDir()
	due := time.Now().UTC().Add(2 * time.Second)

	job := newCountingJob("oneshot-cronfire", "")
	seed, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(dataDir))
	if err != nil {
		t.Fatalf("seed NewScheduler failed: %v", err)
	}
	// Wire the one-shot through the real RegisterTimingVia path so the cron
	// closure routes through fireOneShotOnce, and keep the counting job as
	// the captured instance (the closure delivers to the job it captured).
	// The store row is registered AND immediately removed so
	// loadPersistedJobs does not register a SECOND cron entry over the same
	// due instant — the reload's entry would deliver to its own freshly
	// built shell job instead of the counting job, making the observable
	// run count racy. fireOneShotOnce persists the pruning stamp itself, so
	// the pin's store assertions do not need the pre-seeded row.
	cfg := oneShotJobConfig(job.ID(), due)
	seed.mu.Lock()
	entryID, regErr := seed.RegisterTimingVia(cfg, job)
	if regErr == nil {
		seed.jobs[job.ID()] = job
		seed.entryIDs[job.ID()] = entryID
	}
	seed.mu.Unlock()
	if regErr != nil {
		t.Fatalf("RegisterTimingVia failed: %v", regErr)
	}
	_ = seed.store.Add(cfg)
	_ = seed.store.Remove(cfg.ID)
	if err := seed.Start(context.Background()); err != nil {
		t.Fatalf("seed Start failed: %v", err)
	}

	// Wait for the cron fire.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && job.runCount() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if job.runCount() != 1 {
		t.Fatalf("cron fire did not execute exactly once: %d runs", job.runCount())
	}

	// The cron-fired occurrence must have been claimed at the DUE instant.
	claims, err := seed.claims.ListClaimsSince(due.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListClaimsSince failed: %v", err)
	}
	foundDueClaim := false
	for _, c := range claims {
		if c.JobID == job.ID() && c.Tick.Sub(due).Abs() < time.Minute {
			foundDueClaim = true
		}
	}
	if !foundDueClaim {
		t.Fatalf("cron fire did not claim the due tick %s; claims=%v", due, claims)
	}

	// Grace period for the prune goroutine, then stop the first instance.
	time.Sleep(500 * time.Millisecond)
	if err := seed.Stop(context.Background()); err != nil {
		t.Fatalf("seed Stop failed: %v", err)
	}

	// Boot the next daemon instance over the same data dir, again wiring
	// the counting job as the cron-registered instance so a wrongful
	// re-fire is observable. The store row is gone (pruned after the
	// successful cron fire), so no reload re-registration happens here.
	job2 := newCountingJob(job.ID(), "")
	s2 := newStartedScheduler(t, config.SchedulerConfig{Timezone: "UTC"}, WithDataDir(dataDir))

	time.Sleep(1 * time.Second)
	if got := job2.runCount(); got != 0 {
		t.Fatalf("restarted daemon re-fired the cron-fired one-shot: %d runs, want 0", got)
	}
	// The store row is gone (pruned after the successful cron fire).
	if _, ok := s2.store.Get(job.ID()); ok {
		t.Error("fired one-shot row still persisted after restart; want pruned")
	}
}

// Pin 8 (S5): RunNow on a TimingAt one-shot stamps OneShotFiredAt so a
// restart's boot sweep skips the row — the manual delivery must not become
// an automatic re-fire at boot.
func TestRunNowStampsOneShotFiredAtBootSweepSkips(t *testing.T) {
	dataDir := t.TempDir()
	due := time.Now().UTC().Add(-10 * time.Minute)

	job := newTimingAwareCountingJob("oneshot-runnnow", due)
	seed, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, nil, WithDataDir(dataDir))
	if err != nil {
		t.Fatalf("seed NewScheduler failed: %v", err)
	}
	// Persist the row in the failed (never-ran) posture BEFORE Start: a
	// failed row is not cron-registered and the boot sweep leaves it alone,
	// so RunNow is the only delivery path (deterministic — no sweep race).
	if err := seed.store.Add(func() JobConfig {
		c := oneShotJobConfig(job.ID(), due)
		c.OneShotFailed = true
		c.LastError = "one-shot never ran"
		return c
	}()); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}
	seed.mu.Lock()
	seed.jobs[job.ID()] = job
	seed.mu.Unlock()
	if err := seed.Start(context.Background()); err != nil {
		t.Fatalf("seed Start failed: %v", err)
	}
	// loadPersistedJobs put its own shell job in the map; swap the counting
	// job back in so RunNow's dispatch is observable (RunNow re-fetches the
	// job from the map at dispatch time, unlike the cron closure).
	seed.mu.Lock()
	seed.jobs[job.ID()] = job
	seed.mu.Unlock()

	// RunNow on a failed one-shot is the documented manual re-run path.
	if err := seed.RunNow(job.ID()); err != nil {
		t.Fatalf("RunNow failed: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && job.runCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if job.runCount() == 0 {
		t.Fatal("RunNow did not execute the one-shot")
	}

	// The stamp must be persisted.
	got, ok := seed.store.Get(job.ID())
	if !ok {
		t.Fatal("row pruned by RunNow; expected it kept for the boot-sweep check")
	}
	if got.OneShotFiredAt == nil {
		t.Fatal("RunNow did not stamp OneShotFiredAt on the one-shot row")
	}
	if got.OneShotFailed {
		t.Error("OneShotFailed still set after RunNow")
	}
	// Baseline claim snapshot AFTER RunNow settled: RunNow claims a
	// nanosecond-unique start tick, which is legitimate and must be
	// excluded from the boot-sweep re-fire assertion below.
	claimsBaseline, err := seed.claims.ListClaimsSince(due.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListClaimsSince failed: %v", err)
	}
	if err := seed.Stop(context.Background()); err != nil {
		t.Fatalf("seed Stop failed: %v", err)
	}

	// Boot the next instance: the sweep must NOT re-fire the stamped row.
	s2 := newStartedScheduler(t, config.SchedulerConfig{Timezone: "UTC"}, WithDataDir(dataDir))
	time.Sleep(1 * time.Second)
	// Not-refired proof: no NEW claim rows appeared at boot (RunNow itself
	// claims a nanosecond-unique start tick, so the baseline is non-zero —
	// the assertion is equality, not emptiness), and the stamped row is
	// kept for inspection.
	claimsAfter, err := s2.claims.ListClaimsSince(due.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListClaimsSince failed: %v", err)
	}
	if len(claimsAfter) != len(claimsBaseline) {
		t.Fatalf("boot sweep re-fired the RunNow-delivered one-shot: claims %d -> %d, want no new claims",
			len(claimsBaseline), len(claimsAfter))
	}
	if _, ok := s2.store.Get(job.ID()); !ok {
		t.Error("RunNow-delivered one-shot row pruned by boot sweep; want kept")
	}
}
