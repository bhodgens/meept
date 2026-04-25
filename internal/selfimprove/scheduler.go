package selfimprove

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Scheduler runs self-improvement cycles on a periodic schedule.
type Scheduler struct {
	controller *Controller
	interval   time.Duration
	logger     *slog.Logger

	mu     sync.Mutex
	stopCh chan struct{}
	running bool
}

// NewScheduler creates a new periodic analysis scheduler.
func NewScheduler(ctrl *Controller, interval time.Duration, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		controller: ctrl,
		interval:   interval,
		logger:     logger,
		stopCh:     make(chan struct{}),
	}
}

// Start begins the periodic cycle loop. It blocks until Stop is called or the
// context is cancelled, so callers should invoke it in a goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("self-improve scheduler started", "interval", s.interval)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("self-improve scheduler stopped (context)")
			return
		case <-s.stopCh:
			s.logger.Info("self-improve scheduler stopped")
			return
		case <-ticker.C:
			s.logger.Info("starting scheduled self-improvement cycle")
			cycle, err := s.controller.RunFullCycle(ctx, false)
			if err != nil {
				s.logger.Error("scheduled self-improve cycle failed", "error", err)
				continue
			}
			s.logger.Info("scheduled self-improve cycle completed",
				"detected", cycle.IssuesDetected,
				"applied", cycle.FixesApplied)
		}
	}
}

// Stop signals the scheduler to stop. It is safe to call multiple times.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
}
