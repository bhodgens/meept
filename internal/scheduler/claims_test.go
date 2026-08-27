package scheduler

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// countingJob is a minimal Job that counts executions.
type countingJob struct {
	id    string
	sched string
	runs  chan string // receives the tick context on each Execute
}

func newCountingJob(id, sched string) *countingJob {
	return &countingJob{id: id, sched: sched, runs: make(chan string, 100)}
}

func (j *countingJob) ID() string       { return j.id }
func (j *countingJob) Name() string     { return j.id }
func (j *countingJob) Schedule() string { return j.sched }
func (j *countingJob) Type() JobType    { return JobTypeShell }
func (j *countingJob) Config() JobConfig {
	return JobConfig{ID: j.id, Name: j.id, Type: JobTypeShell, Schedule: j.sched, Enabled: true}
}

func (j *countingJob) Execute(_ context.Context) error {
	j.runs <- "run"
	return nil
}

func (j *countingJob) runCount() int { return len(j.runs) }

// newClaimsTestScheduler builds a scheduler rooted in a temp dir.
func newClaimsTestScheduler(t *testing.T, opts ...Option) *Scheduler {
	t.Helper()
	msgBus := bus.New(nil, nil)
	cfg := config.SchedulerConfig{Enabled: true, Timezone: "UTC"}
	allOpts := append([]Option{WithDataDir(t.TempDir())}, opts...)
	s, err := NewScheduler(cfg, msgBus, allOpts...)
	if err != nil {
		t.Fatalf("NewScheduler failed: %v", err)
	}
	return s
}

// dueTicks computes the ticks of a schedule in (lastWake, now], mirroring the
// scheduler's catch-up enumeration.
func dueTicks(t *testing.T, s *Scheduler, spec string, lastWake, now time.Time) []time.Time {
	t.Helper()
	sched, err := s.parseSchedule(spec)
	if err != nil {
		t.Fatalf("parseSchedule(%q) failed: %v", spec, err)
	}
	var ticks []time.Time
	for tick := sched.Next(lastWake); !tick.After(now); tick = sched.Next(tick) {
		ticks = append(ticks, tick)
	}
	return ticks
}

// waitForRuns polls until the job has executed want times or the deadline passes.
func waitForRuns(t *testing.T, j *countingJob, want int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if j.runCount() >= want {
			return j.runCount()
		}
		time.Sleep(10 * time.Millisecond)
	}
	return j.runCount()
}

func TestClaimStoreClaimTick(t *testing.T) {
	tmpDir := t.TempDir()
	cs, err := newClaimStore(filepath.Join(tmpDir, "claims.db"))
	if err != nil {
		t.Fatalf("newClaimStore failed: %v", err)
	}
	defer cs.close()

	tick := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	testCases := []struct {
		name      string
		jobID     string
		tick      time.Time
		preClaim  bool
		want      bool
		wantErr   bool
		wantError string
	}{
		{name: "first claim succeeds", jobID: "job-a", tick: tick, want: true},
		{name: "double claim returns false", jobID: "job-a", tick: tick, preClaim: true, want: false},
		{name: "different tick same job", jobID: "job-a", tick: tick.Add(5 * time.Minute), want: true},
		{name: "same tick different job", jobID: "job-b", tick: tick, want: true},
		{name: "empty job id errors", jobID: "", tick: tick, wantErr: true},
		{name: "zero tick errors", jobID: "job-a", tick: time.Time{}, wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.preClaim {
				if _, err := cs.ClaimTick(tc.jobID, tc.tick); err != nil {
					t.Fatalf("pre-claim failed: %v", err)
				}
			}
			got, err := cs.ClaimTick(tc.jobID, tc.tick)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got claimed=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ClaimTick failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("ClaimTick = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaimStorePruneOldClaims(t *testing.T) {
	tmpDir := t.TempDir()
	cs, err := newClaimStore(filepath.Join(tmpDir, "claims.db"))
	if err != nil {
		t.Fatalf("newClaimStore failed: %v", err)
	}
	defer cs.close()

	now := time.Now().UTC()
	oldTick := now.Add(-8 * 24 * time.Hour)
	recentTick := now.Add(-2 * 24 * time.Hour)

	for _, tick := range []time.Time{oldTick, recentTick} {
		if ok, err := cs.ClaimTick("job-a", tick); err != nil || !ok {
			t.Fatalf("seed claim(%v) = %v, %v; want true, nil", tick, ok, err)
		}
	}

	pruned, err := cs.PruneClaimsOlderThan(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("PruneClaimsOlderThan failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	// Old claim was pruned: re-claim succeeds.
	if ok, err := cs.ClaimTick("job-a", oldTick); err != nil || !ok {
		t.Errorf("re-claim of pruned tick = %v, %v; want true, nil", ok, err)
	}
	// Recent claim survived: re-claim fails.
	if ok, err := cs.ClaimTick("job-a", recentTick); err != nil || ok {
		t.Errorf("re-claim of surviving tick = %v, %v; want false, nil", ok, err)
	}
}

func TestClaimStoreTimeOrdering(t *testing.T) {
	// Guards the fixed-width time format: sub-second ticks must compare
	// lexicographically in chronological order (RFC3339Nano drops trailing
	// zeros, which breaks string comparison).
	tmpDir := t.TempDir()
	cs, err := newClaimStore(filepath.Join(tmpDir, "claims.db"))
	if err != nil {
		t.Fatalf("newClaimStore failed: %v", err)
	}
	defer cs.close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fractional := base.Add(500 * time.Millisecond)

	if ok, err := cs.ClaimTick("job-a", fractional); err != nil || !ok {
		t.Fatalf("claim fractional tick = %v, %v; want true, nil", ok, err)
	}
	pruned, err := cs.PruneClaimsOlderThan(base)
	if err != nil {
		t.Fatalf("PruneClaimsOlderThan failed: %v", err)
	}
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 (fractional tick is AFTER base)", pruned)
	}
	pruned, err = cs.PruneClaimsOlderThan(base.Add(time.Second))
	if err != nil {
		t.Fatalf("PruneClaimsOlderThan failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
}

func TestSchedulerClaimTick(t *testing.T) {
	s := newClaimsTestScheduler(t)
	tick := time.Now().UTC().Add(-time.Minute)

	ok, err := s.ClaimTick("job-a", tick)
	if err != nil || !ok {
		t.Fatalf("first ClaimTick = %v, %v; want true, nil", ok, err)
	}
	ok, err = s.ClaimTick("job-a", tick)
	if err != nil || ok {
		t.Fatalf("second ClaimTick = %v, %v; want false, nil", ok, err)
	}
}

func TestLastWakeRoundtrip(t *testing.T) {
	s := newClaimsTestScheduler(t)

	got, err := s.GetLastWake()
	if err != nil {
		t.Fatalf("GetLastWake failed: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("initial lastWake = %v, want zero", got)
	}

	want := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	if err := s.SetLastWake(want); err != nil {
		t.Fatalf("SetLastWake failed: %v", err)
	}
	got, err = s.GetLastWake()
	if err != nil {
		t.Fatalf("GetLastWake failed: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("lastWake = %v, want %v", got, want)
	}
}

func TestExecuteJobClaimBeforeDispatch(t *testing.T) {
	s := newClaimsTestScheduler(t)
	job := newCountingJob("job-claim", "@every 5m")

	tick := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Pre-claimed tick (e.g. claimed before a crash): delivery must be skipped.
	if ok, err := s.ClaimTick(job.ID(), tick); err != nil || !ok {
		t.Fatalf("ClaimTick = %v, %v; want true, nil", ok, err)
	}
	s.executeJob(context.Background(), job, ExecutionOptions{Tick: tick})
	if got := waitForRuns(t, job, 1, 200*time.Millisecond); got != 0 {
		t.Errorf("job ran %d times for already-claimed tick, want 0", got)
	}

	// Unclaimed tick: delivery proceeds and the tick becomes claimed.
	tick2 := tick.Add(5 * time.Minute)
	s.executeJob(context.Background(), job, ExecutionOptions{Tick: tick2})
	if got := waitForRuns(t, job, 1, 2*time.Second); got != 1 {
		t.Errorf("job ran %d times for unclaimed tick, want 1", got)
	}
	if ok, err := s.ClaimTick(job.ID(), tick2); err != nil || ok {
		t.Errorf("post-delivery ClaimTick = %v, %v; want false, nil", ok, err)
	}
}

func TestProcessMissedTicksCoalesced(t *testing.T) {
	s := newClaimsTestScheduler(t)
	job := newCountingJob("job-coalesce", "@every 5m")
	if _, err := s.Schedule(job); err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	// Subscribe to job-started events to inspect missed_count metadata.
	sub := s.bus.Subscribe("test-missed-count", "scheduler.job.started")

	now := time.Now().UTC()
	lastWake := now.Add(-16 * time.Minute) // due ticks: -11m, -6m, -1m
	if err := s.SetLastWake(lastWake); err != nil {
		t.Fatalf("SetLastWake failed: %v", err)
	}

	runs, err := s.ProcessMissedTicks(now)
	if err != nil {
		t.Fatalf("ProcessMissedTicks failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d enqueued runs, want 1 (coalesced)", len(runs))
	}
	if runs[0].MissedCount != 2 {
		t.Errorf("MissedCount = %d, want 2", runs[0].MissedCount)
	}
	expectedTicks := dueTicks(t, s, job.Schedule(), lastWake, now)
	if len(expectedTicks) != 3 {
		t.Fatalf("test setup: got %d due ticks, want 3", len(expectedTicks))
	}
	if !runs[0].Tick.Equal(expectedTicks[2]) {
		t.Errorf("coalesced tick = %v, want latest %v", runs[0].Tick, expectedTicks[2])
	}

	if got := waitForRuns(t, job, 1, 2*time.Second); got != 1 {
		t.Errorf("job ran %d times, want 1 (coalesced delivery)", got)
	}

	// missed_count metadata must be present on the start event.
	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload failed: %v", err)
		}
		mc, ok := payload[SchedulerKeyMissedCount]
		if !ok {
			t.Fatalf("payload missing %q key: %v", SchedulerKeyMissedCount, payload)
		}
		if mc.(float64) != 2 {
			t.Errorf("missed_count = %v, want 2", mc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for job_started event")
	}

	// A second sweep immediately after finds nothing due (no busy-loop work).
	runs2, err := s.ProcessMissedTicks(now)
	if err != nil {
		t.Fatalf("second ProcessMissedTicks failed: %v", err)
	}
	if len(runs2) != 0 {
		t.Errorf("second sweep enqueued %d runs, want 0", len(runs2))
	}
}

func TestProcessMissedTicksCoalesceDisabled(t *testing.T) {
	s := newClaimsTestScheduler(t, WithCoalesceMissed(false))
	job := newCountingJob("job-accumulate", "@every 5m")
	if _, err := s.Schedule(job); err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	now := time.Now().UTC()
	if err := s.SetLastWake(now.Add(-16 * time.Minute)); err != nil {
		t.Fatalf("SetLastWake failed: %v", err)
	}

	runs, err := s.ProcessMissedTicks(now)
	if err != nil {
		t.Fatalf("ProcessMissedTicks failed: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d enqueued runs, want 3 (accumulate mode)", len(runs))
	}
	for _, r := range runs {
		if r.MissedCount != 0 {
			t.Errorf("MissedCount = %d, want 0 in accumulate mode", r.MissedCount)
		}
	}
	if got := waitForRuns(t, job, 3, 3*time.Second); got != 3 {
		t.Errorf("job ran %d times, want 3 (one per missed tick)", got)
	}
}

func TestProcessMissedTicksNothingDue(t *testing.T) {
	s := newClaimsTestScheduler(t)
	job := newCountingJob("job-nothing", "@every 5m")
	if _, err := s.Schedule(job); err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	started := time.Now()
	runs, err := s.ProcessMissedTicks(time.Now().UTC())
	if err != nil {
		t.Fatalf("ProcessMissedTicks failed: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("got %d runs, want 0", len(runs))
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("sweep with nothing due took %v (busy-loop?)", elapsed)
	}
	if got := waitForRuns(t, job, 1, 200*time.Millisecond); got != 0 {
		t.Errorf("job ran %d times, want 0", got)
	}
}

func TestProcessMissedTicksRequiresRunning(t *testing.T) {
	s := newClaimsTestScheduler(t)
	if _, err := s.ProcessMissedTicks(time.Now().UTC()); err == nil {
		t.Error("expected error when scheduler not running")
	}
}

// Crash simulation: ticks claimed before a crash survive restart and prevent
// re-delivery on catch-up.
func TestCrashRecoveryNoRedelivery(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)
	cfg := config.SchedulerConfig{Enabled: true, Timezone: "UTC"}

	// Phase 1: daemon starts, backdates lastWake, claims all due ticks
	// (claim-before-deliver), then CRASHES (no Stop call).
	s1, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler(s1) failed: %v", err)
	}
	job1 := newCountingJob("job-crash", "@every 5m")
	if _, err := s1.Schedule(job1); err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	if err := s1.Start(context.Background()); err != nil {
		t.Fatalf("Start(s1) failed: %v", err)
	}

	now := time.Now().UTC()
	lastWake := now.Add(-16 * time.Minute)
	if err := s1.SetLastWake(lastWake); err != nil {
		t.Fatalf("SetLastWake failed: %v", err)
	}
	ticks := dueTicks(t, s1, job1.Schedule(), lastWake, now)
	if len(ticks) != 3 {
		t.Fatalf("test setup: got %d due ticks, want 3", len(ticks))
	}
	for _, tick := range ticks {
		if ok, err := s1.ClaimTick(job1.ID(), tick); err != nil || !ok {
			t.Fatalf("pre-crash ClaimTick(%v) = %v, %v; want true, nil", tick, ok, err)
		}
	}
	// Crash: abandon s1 without Stop (its claim store stays open, like a
	// leaked fd in a dying process).

	// Phase 2: restart against the same data dir. Catch-up must find all
	// three due ticks already claimed and deliver nothing.
	s2, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler(s2) failed: %v", err)
	}
	job2 := newCountingJob("job-crash", "@every 5m")
	if _, err := s2.Schedule(job2); err != nil {
		t.Fatalf("Schedule(s2) failed: %v", err)
	}
	if err := s2.Start(context.Background()); err != nil {
		t.Fatalf("Start(s2) failed: %v", err)
	}
	defer func() { _ = s2.Stop(context.Background()) }()

	if got := waitForRuns(t, job2, 1, 300*time.Millisecond); got != 0 {
		t.Errorf("job re-delivered %d times after restart, want 0", got)
	}

	// Explicit sweep over the same window also delivers nothing.
	runs, err := s2.ProcessMissedTicks(now)
	if err != nil {
		t.Fatalf("ProcessMissedTicks(s2) failed: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("post-crash sweep enqueued %d runs, want 0", len(runs))
	}
	if got := waitForRuns(t, job2, 1, 200*time.Millisecond); got != 0 {
		t.Errorf("job ran %d times after explicit sweep, want 0", got)
	}
}

func TestRetentionPruneOnStartup(t *testing.T) {
	tmpDir := t.TempDir()
	msgBus := bus.New(nil, nil)
	cfg := config.SchedulerConfig{Enabled: true, Timezone: "UTC"}

	s1, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler(s1) failed: %v", err)
	}
	now := time.Now().UTC()
	oldTick := now.Add(-48 * time.Hour)
	recentTick := now.Add(-time.Hour)
	for _, tick := range []time.Time{oldTick, recentTick} {
		if ok, err := s1.ClaimTick("job-r", tick); err != nil || !ok {
			t.Fatalf("seed ClaimTick(%v) = %v, %v; want true, nil", tick, ok, err)
		}
	}
	if err := s1.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(s1) failed: %v", err)
	}

	// Restart with a 1-day retention: the 48h-old claim is pruned.
	s2, err := NewScheduler(cfg, msgBus, WithDataDir(tmpDir), WithClaimRetentionDays(1))
	if err != nil {
		t.Fatalf("NewScheduler(s2) failed: %v", err)
	}
	if err := s2.Start(context.Background()); err != nil {
		t.Fatalf("Start(s2) failed: %v", err)
	}
	defer func() { _ = s2.Stop(context.Background()) }()

	if ok, err := s2.ClaimTick("job-r", oldTick); err != nil || !ok {
		t.Errorf("re-claim of pruned tick = %v, %v; want true, nil", ok, err)
	}
	if ok, err := s2.ClaimTick("job-r", recentTick); err != nil || ok {
		t.Errorf("re-claim of retained tick = %v, %v; want false, nil", ok, err)
	}
}

func TestRetentionDaysOptionValidation(t *testing.T) {
	// Non-positive retention falls back to the default.
	s := newClaimsTestScheduler(t, WithClaimRetentionDays(0))
	if s.claimRetentionDays != DefaultClaimRetentionDays {
		t.Errorf("claimRetentionDays = %d, want default %d", s.claimRetentionDays, DefaultClaimRetentionDays)
	}
}

func TestWrapJobClaimsAndDelivers(t *testing.T) {
	s := newClaimsTestScheduler(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	job := newCountingJob("job-wrap", "@every 5m")
	before, err := s.ClaimCount()
	if err != nil {
		t.Fatalf("ClaimCount failed: %v", err)
	}

	s.wrapJob(job)()

	if got := waitForRuns(t, job, 1, 2*time.Second); got != 1 {
		t.Errorf("job ran %d times, want 1", got)
	}
	after, err := s.ClaimCount()
	if err != nil {
		t.Fatalf("ClaimCount failed: %v", err)
	}
	if after != before+1 {
		t.Errorf("claim rows before=%d after=%d, want exactly one new claim", before, after)
	}
}
