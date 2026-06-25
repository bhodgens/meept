package lifecycle

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EvolverScheduler runs the skill evolver on a periodic schedule, following
// the same pattern as internal/selfimprove/scheduler.go.
type EvolverScheduler struct {
	evolver    *Evolver
	interval   time.Duration
	runOnStart bool
	logger     *slog.Logger

	mu       sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
	running  bool
}

// NewEvolverScheduler creates a new periodic skill evolution scheduler. The
// scheduler does not start until Start is called. By default the scheduler
// runs one cycle immediately on Start (legacy behavior); pass
// WithRunOnStart(false) or construct via NewEvolverSchedulerWithRunOnStart to
// skip the initial cycle (recommended for daemon startup where the immediate
// cycle is noisy).
func NewEvolverScheduler(evolver *Evolver, interval time.Duration, logger *slog.Logger) *EvolverScheduler {
	return newEvolverScheduler(evolver, interval, true, logger)
}

// NewEvolverSchedulerWithRunOnStart creates a scheduler with explicit control
// over whether a cycle runs immediately on Start. When runOnStart is false the
// scheduler enters the tick loop directly, avoiding LLM traffic on daemon
// startup.
func NewEvolverSchedulerWithRunOnStart(evolver *Evolver, interval time.Duration, runOnStart bool, logger *slog.Logger) *EvolverScheduler {
	return newEvolverScheduler(evolver, interval, runOnStart, logger)
}

// WithRunOnStart returns an option (for use with a future option-based
// constructor) that configures whether the scheduler runs a cycle immediately
// on Start. Kept as a standalone option so callers can migrate incrementally.
func WithRunOnStart(runOnStart bool) EvolverSchedulerOption {
	return func(s *EvolverScheduler) {
		s.runOnStart = runOnStart
	}
}

// EvolverSchedulerOption configures an EvolverScheduler.
type EvolverSchedulerOption func(*EvolverScheduler)

func newEvolverScheduler(evolver *Evolver, interval time.Duration, runOnStart bool, logger *slog.Logger) *EvolverScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &EvolverScheduler{
		evolver:    evolver,
		interval:   interval,
		runOnStart: runOnStart,
		logger:     logger.With("component", "skill-evolver-scheduler"),
		stopCh:     make(chan struct{}),
	}
}

// Start begins the periodic evolution cycle loop. It blocks until Stop is
// called or the context is cancelled, so callers should invoke it in a
// goroutine.
func (s *EvolverScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("skill evolver scheduler started", "interval", s.interval, "run_on_start", s.runOnStart)

	// Run one cycle immediately on startup only when explicitly enabled.
	// Default is false — running on daemon startup is noisy (many LLM calls
	// when many skills are registered). Operators can set
	// skills.evolver.run_on_start=true in config or use
	// NewEvolverSchedulerWithRunOnStart(_, _, true, _).
	if s.runOnStart {
		s.runCycle(ctx)
	} else {
		s.logger.Debug("skipping initial skill evolution cycle (run_on_start=false)")
	}

	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			s.logger.Info("skill evolver scheduler stopped (context)")
			return
		case <-s.stopCh:
			s.logger.Info("skill evolver scheduler stopped")
			return
		case <-ticker.C:
			s.runCycle(ctx)
		}
	}
}

// runCycle executes one evolution cycle and logs the result.
func (s *EvolverScheduler) runCycle(ctx context.Context) {
	s.logger.Info("starting scheduled skill evolution cycle")
	report, err := s.evolver.RunCycle(ctx)
	if err != nil {
		s.logger.Error("scheduled skill evolution cycle failed", "error", err)
		return
	}
	s.logger.Info("scheduled skill evolution cycle completed",
		"refined", report.Refined,
		"promoted", report.Promoted,
		"pruned", report.Pruned,
		"skipped", report.Skipped,
		"rejected", report.Rejected,
		"planned", report.Planned,
	)
}

// Stop signals the scheduler to stop. It is safe to call multiple times.
func (s *EvolverScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stopCh) })
}
