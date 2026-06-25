// Package integration contains end-to-end and component-integration tests
// that exercise multiple internal packages together.
//
// This file (skill_evolver_lifecycle_test.go) closes gap #8 of the skill
// evolution closed-loop plan: it provides an integration test exercising the
// full Evolver + EvolverScheduler lifecycle with real implementations backed
// by tempdir storage (SQLite usage tracker, filesystem writer, real learning
// pipeline, real capability index, real verifier). No network or LLM is
// required — the evolver gracefully degrades when the LLM client is nil.
package integration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// callCountUsageTracker wraps a real UsageTrackerImpl and atomically counts
// GetAllStats invocations. Since RunCycle calls usage.GetAllStats in Pass A
// and Pass C, observing the call count is a reliable signal that the scheduler
// triggered a cycle — without needing to inject a mock LLM or observe
// side-effects on disk.
type callCountUsageTracker struct {
	lifecycle.UsageTracker
	getAllStatsCalls atomic.Int64
}

func newCallCountUsageTracker(dbPath string, logger *slog.Logger) (*callCountUsageTracker, error) {
	impl, err := lifecycle.NewUsageTracker(dbPath, logger)
	if err != nil {
		return nil, err
	}
	return &callCountUsageTracker{UsageTracker: impl}, nil
}

func (c *callCountUsageTracker) GetAllStats() (map[string]*lifecycle.UsageStats, error) {
	c.getAllStatsCalls.Add(1)
	return c.UsageTracker.GetAllStats()
}

func (c *callCountUsageTracker) GetAllStatsCallCount() int64 {
	return c.getAllStatsCalls.Load()
}

// evolverFixture bundles all the real objects constructed for a test case.
type evolverFixture struct {
	evolver  *lifecycle.Evolver
	sched    *lifecycle.EvolverScheduler
	usage    *callCountUsageTracker
	writer   *lifecycle.Writer
	skillsDir string
	cleanup  func()
}

// buildEvolverFixture constructs a full Evolver + EvolverScheduler using real
// implementations backed by tempdir storage. No network or LLM required.
//
// The scheduler is constructed via NewEvolverSchedulerWithRunOnStart so that
// the config's RunOnStart field is respected.
func buildEvolverFixture(t *testing.T, cfg config.SkillsEvolverConfig) *evolverFixture {
	t.Helper()

	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills")
	dataDir := filepath.Join(tmpDir, "data")
	dbPath := filepath.Join(tmpDir, "skills.db")

	for _, dir := range []string{skillsDir, dataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Real usage tracker (SQLite at tempdir path).
	usage, err := newCallCountUsageTracker(dbPath, logger)
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}

	// Real writer.
	writer := lifecycle.NewWriter(skillsDir, logger)

	// Real registry with one skill so Pass A has something to iterate.
	registry := skills.NewRegistry()
	skillContent := "---\nname: integration-test-skill\ndescription: test\n---\n\n# integration-test-skill\n\nbody"
	if err := writer.WriteSkill("integration-test-skill", skillContent); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}
	parsed, err := skills.ParseSkillFile(filepath.Join(skillsDir, "integration-test-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	registry.Register(parsed)
	writer.SetRegistry(registry)

	// Real learning pipeline (nil LLM — Retrieve returns empty, Pass B skips).
	lpCfg := selfimprove.DefaultLearningConfig()
	lp := selfimprove.NewLearningPipeline(lpCfg, nil, dataDir, logger)
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize learning pipeline: %v", err)
	}

	// Real capability index (empty).
	capIndex := skills.BuildCapabilityIndex(skills.NewSkillIndex())

	// Real verifier with nil LLM (heuristic fallback rejects everything at
	// the default 0.75 threshold — fine for lifecycle testing).
	verifier := lifecycle.NewVerifier(nil, logger)

	evolver := lifecycle.NewEvolver(
		usage, lp, writer, registry, capIndex,
		verifier, nil, nil, // nil LLM, nil plan manager
		cfg, logger,
	)

	interval := cfg.Interval
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	sched := lifecycle.NewEvolverSchedulerWithRunOnStart(evolver, interval, cfg.RunOnStart, logger)

	return &evolverFixture{
		evolver:   evolver,
		sched:     sched,
		usage:     usage,
		writer:    writer,
		skillsDir: skillsDir,
		cleanup:   func() { _ = usage.Close() },
	}
}

// settleGoroutines waits briefly for goroutines to exit after Stop, then
// returns the current count. Used for goroutine leak detection.
func settleGoroutines() int {
	time.Sleep(100 * time.Millisecond)
	return runtime.NumGoroutine()
}

// ---------------------------------------------------------------------------
// Tests: scheduler lifecycle (Start/Stop)
// ---------------------------------------------------------------------------

// TestSkillEvolverLifecycle_StartStopClean exercises the EvolverScheduler
// lifecycle: Start in a goroutine, wait briefly, then Stop. Asserts no panic,
// no deadlock, and that Stop is idempotent (double-stop is safe).
func TestSkillEvolverLifecycle_StartStopClean(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   100 * time.Millisecond,
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  false,
		RunOnStart:                 true,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	beforeGoroutines := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go f.sched.Start(ctx)

	time.Sleep(200 * time.Millisecond)

	f.sched.Stop()
	f.sched.Stop() // Double-stop must be safe (idempotent).
	cancel()        // Cancel after Stop — should be safe.

	afterGoroutines := settleGoroutines()
	if afterGoroutines > beforeGoroutines {
		t.Errorf("potential goroutine leak: before=%d, after=%d", beforeGoroutines, afterGoroutines)
	}
}

// TestSkillEvolverLifecycle_StopWithoutStart verifies that calling Stop without
// Start is safe (no panic, no deadlock).
func TestSkillEvolverLifecycle_StopWithoutStart(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:  true,
		Interval: 1 * time.Second,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	f.sched.Stop()
	f.sched.Stop()
}

// TestSkillEvolverLifecycle_NoGoroutineLeak verifies that after Start + Stop,
// no scheduler goroutine remains running. Uses a WaitGroup to ensure the Start
// goroutine has fully exited before measuring.
func TestSkillEvolverLifecycle_NoGoroutineLeak(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:    true,
		Interval:   50 * time.Millisecond,
		RunOnStart: true,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f.sched.Start(ctx)
	}()

	time.Sleep(250 * time.Millisecond)

	f.sched.Stop()
	cancel()
	wg.Wait()

	after := settleGoroutines()
	if after > before {
		t.Errorf("goroutine leak: before=%d, after=%d (delta=%d)", before, after, after-before)
	}
}

// ---------------------------------------------------------------------------
// Tests: RunCycle is triggered by the scheduler
// ---------------------------------------------------------------------------

// TestSkillEvolverLifecycle_RunCycleTriggered verifies that starting the
// scheduler with RunOnStart=true triggers at least one RunCycle (detected via
// GetAllStats call count). Even with a long interval, the immediate-on-start
// cycle fires.
func TestSkillEvolverLifecycle_RunCycleTriggered(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   10 * time.Second, // Long interval — tests the immediate cycle.
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  false,
		RunOnStart:                 true,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go f.sched.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	var calls int64
	for time.Now().Before(deadline) {
		calls = f.usage.GetAllStatsCallCount()
		if calls >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	f.sched.Stop()
	cancel()

	if calls < 1 {
		t.Fatalf("expected at least 1 GetAllStats call from RunCycle, got %d", calls)
	}
}

// TestSkillEvolverLifecycle_RunCycleMultipleTicks verifies that with a short
// interval and RunOnStart=true, the scheduler triggers multiple RunCycles
// (initial + at least 2 ticks).
func TestSkillEvolverLifecycle_RunCycleMultipleTicks(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   50 * time.Millisecond,
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  false,
		RunOnStart:                 true,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go f.sched.Start(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if f.usage.GetAllStatsCallCount() >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	f.sched.Stop()
	cancel()

	calls := f.usage.GetAllStatsCallCount()
	if calls < 3 {
		t.Errorf("expected at least 3 GetAllStats calls (initial + 2 ticks), got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// Tests: RunOnStart=false suppresses the immediate cycle
// ---------------------------------------------------------------------------

// TestSkillEvolverLifecycle_RunOnStartFalse verifies that with RunOnStart=false,
// starting the scheduler does NOT trigger an immediate RunCycle (within a
// short window well before the first tick would fire).
func TestSkillEvolverLifecycle_RunOnStartFalse(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   10 * time.Second, // Long — no tick within the test window.
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  false,
		RunOnStart:                 false,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go f.sched.Start(ctx)

	// Wait 300ms — much less than the 10s interval. No cycle should fire.
	time.Sleep(300 * time.Millisecond)

	f.sched.Stop()
	cancel()

	calls := f.usage.GetAllStatsCallCount()
	if calls != 0 {
		t.Errorf("expected 0 GetAllStats calls with RunOnStart=false, got %d", calls)
	}
}

// TestSkillEvolverLifecycle_RunOnStartTrue verifies that with RunOnStart=true,
// starting the scheduler DOES trigger at least one RunCycle (detected within
// a 2-second polling window).
func TestSkillEvolverLifecycle_RunOnStartTrue(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   10 * time.Second, // Long — tests only the immediate cycle.
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  false,
		RunOnStart:                 true,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go f.sched.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	var calls int64
	for time.Now().Before(deadline) {
		calls = f.usage.GetAllStatsCallCount()
		if calls >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	f.sched.Stop()
	cancel()

	if calls < 1 {
		t.Errorf("expected at least 1 GetAllStats call with RunOnStart=true, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// Tests: direct RunCycle (component integration without scheduler)
// ---------------------------------------------------------------------------

// TestSkillEvolverLifecycle_RunCycleDirect verifies that calling RunCycle
// directly on the Evolver works with all real components (no scheduler).
// This is the "component integration" level: Evolver + UsageTracker + Writer +
// LearningPipeline + Registry + Verifier, all real implementations backed by
// tempdir storage and nil LLM.
func TestSkillEvolverLifecycle_RunCycleDirect(t *testing.T) {
	cfg := config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   1 * time.Hour,
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  false,
	}

	f := buildEvolverFixture(t, cfg)
	defer f.cleanup()

	// Record enough injections to clear the MinInjections threshold.
	for range 6 {
		_ = f.usage.RecordInjection("integration-test-skill")
	}
	_ = f.usage.RecordOutcome("integration-test-skill", lifecycle.OutcomeNegative, "session-1")

	report, err := f.evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if report == nil {
		t.Fatal("RunCycle returned nil report")
	}

	// The skill has inject_count >= MinInjections (6 >= 5), so Pass A should
	// evaluate it. With nil LLM, callLLMJSON returns (nil, nil) → the skill
	// is skipped. So Skipped should be >= 1.
	if report.Skipped < 1 {
		t.Errorf("expected Skipped >= 1 (skill present with enough injections, nil LLM), got %d (report: %+v)", report.Skipped, report)
	}

	// Skill file should still exist (nothing was applied).
	skillPath := filepath.Join(f.skillsDir, "integration-test-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill file should still exist after RunCycle: %v", err)
	}

	// GetAllStats should have been called at least once (by Pass A or Pass C).
	if f.usage.GetAllStatsCallCount() < 1 {
		t.Error("expected at least 1 GetAllStats call during RunCycle")
	}
}
