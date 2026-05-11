package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// WorkerStage represents the current stage of a monitored worker.
type WorkerStage string

const (
	StageThinking   WorkerStage = "thinking"
	StageExecuting  WorkerStage = "executing"
	StageValidating WorkerStage = "validating"
	StageReviewing  WorkerStage = "reviewing"
)

// WorkerState tracks the state of a single monitored worker/agent.
type WorkerState struct {
	WorkerID      string
	TaskID        string
	StepID        string
	StartTime     time.Time
	LastHeartbeat time.Time
	Iteration     int
	Stage         WorkerStage
	IsStuck       bool
	CancelFunc    context.CancelFunc
}

// WatchdogAlertType identifies the kind of watchdog alert.
type WatchdogAlertType string

const (
	AlertTimeout   WatchdogAlertType = "timeout"
	AlertMaxIter   WatchdogAlertType = "max_iterations"
	AlertStuck     WatchdogAlertType = "stuck"
	AlertHeartbeat WatchdogAlertType = "heartbeat_missed"
)

// WatchdogAlert represents a watchdog alert for a stuck or timed-out worker.
type WatchdogAlert struct {
	Type      WatchdogAlertType
	WorkerID  string
	TaskID    string
	StepID    string
	Message   string
	Duration  time.Duration
	Iteration int
}

// ReportCapture captures partial work state on abort for recovery.
type ReportCapture struct {
	WorkerID      string        `json:"worker_id"`
	TaskID        string        `json:"task_id"`
	StepID        string        `json:"step_id"`
	Iterations    int           `json:"iterations"`
	Duration      time.Duration `json:"duration"`
	Stage         WorkerStage   `json:"stage"`
	CapturedAt    time.Time     `json:"captured_at"`
	PartialResult string        `json:"partial_result,omitempty"`
}

// Watchdog monitors agent workers for stuck conditions and aborts them after timeouts.
type Watchdog struct {
	mu      sync.RWMutex
	workers map[string]*WorkerState // workerID -> state
	config  config.WatchdogConfig
	logger  *slog.Logger

	// Alert channel for consumers to receive alerts
	alertCh chan WatchdogAlert

	// Report channel for capturing partial state on abort
	reportCh chan ReportCapture

	// Background monitoring
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// NewWatchdog creates a new Watchdog monitor with the given configuration.
func NewWatchdog(cfg config.WatchdogConfig, logger *slog.Logger) *Watchdog {
	if logger == nil {
		logger = slog.Default()
	}

	return &Watchdog{
		workers:  make(map[string]*WorkerState),
		config:   cfg,
		logger:   logger.With("component", "watchdog"),
		alertCh:  make(chan WatchdogAlert, 64),
		reportCh: make(chan ReportCapture, 16),
	}
}

// Start begins the background monitoring goroutine.
func (w *Watchdog) Start(ctx context.Context) {
	if !w.config.Enabled {
		w.logger.Debug("Watchdog disabled, not starting background monitor")
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancelFunc = cancel

	heartbeatInterval := time.Duration(w.config.HeartbeatIntervalSec) * time.Second
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 * time.Second
	}

	w.wg.Go(func() {
		w.logger.Info("Watchdog monitor started",
			"timeout_min", w.config.TimeoutMinutes,
			"heartbeat_interval", heartbeatInterval,
			"max_iterations", w.config.MaxIterations,
			"stuck_iteration_count", w.config.StuckIterationCount,
		)

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("Watchdog monitor stopped")
				return
			case <-ticker.C:
				w.checkWorkers()
			}
		}
	})
}

// Stop gracefully stops the watchdog monitor.
func (w *Watchdog) Stop() {
	if w.cancelFunc != nil {
		w.cancelFunc()
	}
	w.wg.Wait()
}

// RegisterWorker registers a worker for monitoring.
// The cancel function is used to abort the worker if it becomes stuck.
func (w *Watchdog) RegisterWorker(workerID, taskID, stepID string, cancelFunc context.CancelFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	w.workers[workerID] = &WorkerState{
		WorkerID:      workerID,
		TaskID:        taskID,
		StepID:        stepID,
		StartTime:     now,
		LastHeartbeat: now,
		Iteration:     0,
		Stage:         StageThinking,
		IsStuck:       false,
		CancelFunc:    cancelFunc,
	}

	w.logger.Debug("Worker registered",
		"worker_id", workerID,
		"task_id", taskID,
		"step_id", stepID,
	)
}

// UpdateHeartbeat updates the heartbeat for a worker, indicating it is still alive.
// Also updates the iteration count and stage.
func (w *Watchdog) UpdateHeartbeat(workerID string, iteration int, stage WorkerStage) {
	w.mu.Lock()
	defer w.mu.Unlock()

	state, ok := w.workers[workerID]
	if !ok {
		return
	}

	state.LastHeartbeat = time.Now()
	state.Iteration = iteration
	state.Stage = stage
	state.IsStuck = false
}

// UnregisterWorker removes a worker from monitoring.
// Should be called when a worker completes or is cleaned up.
func (w *Watchdog) UnregisterWorker(workerID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.workers, workerID)
	w.logger.Debug("Worker unregistered", "worker_id", workerID)
}

// Alerts returns a read-only channel for receiving watchdog alerts.
func (w *Watchdog) Alerts() <-chan WatchdogAlert {
	return w.alertCh
}

// Reports returns a read-only channel for receiving report captures.
func (w *Watchdog) Reports() <-chan ReportCapture {
	return w.reportCh
}

// GetWorkerState returns the current state of a worker (for inspection).
func (w *Watchdog) GetWorkerState(workerID string) (*WorkerState, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	state, ok := w.workers[workerID]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *state
	return &copy, true
}

// ActiveWorkerCount returns the number of currently monitored workers.
func (w *Watchdog) ActiveWorkerCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.workers)
}

// CaptureReport captures the partial state of a worker (used on abort).
// This method is safe to call while holding the write lock (does not re-acquire it).
// When called from checkWorkers (which holds w.mu), use captureReportUnsafe instead.
func (w *Watchdog) CaptureReport(workerID string, partialResult string) *ReportCapture {
	w.mu.Lock()
	state, ok := w.workers[workerID]
	if !ok {
		w.mu.Unlock()
		return nil
	}

	report := ReportCapture{
		WorkerID:      workerID,
		TaskID:        state.TaskID,
		StepID:        state.StepID,
		Iterations:    state.Iteration,
		Duration:      time.Since(state.StartTime),
		Stage:         state.Stage,
		CapturedAt:    time.Now(),
		PartialResult: partialResult,
	}
	w.mu.Unlock()

	// Non-blocking send to report channel
	select {
	case w.reportCh <- report:
	default:
		w.logger.Warn("Report channel full, dropping capture",
			"worker_id", workerID,
		)
	}

	return &report
}

// captureReportUnsafe captures a report without acquiring the lock.
// Must only be called when w.mu is already held (e.g., from checkWorkers).
func (w *Watchdog) captureReportUnsafe(state *WorkerState, partialResult string) *ReportCapture {
	report := ReportCapture{
		WorkerID:      state.WorkerID,
		TaskID:        state.TaskID,
		StepID:        state.StepID,
		Iterations:    state.Iteration,
		Duration:      time.Since(state.StartTime),
		Stage:         state.Stage,
		CapturedAt:    time.Now(),
		PartialResult: partialResult,
	}

	// Non-blocking send to report channel
	select {
	case w.reportCh <- report:
	default:
		w.logger.Warn("Report channel full, dropping capture",
			"worker_id", state.WorkerID,
		)
	}

	return &report
}

// checkWorkers is the main monitoring loop body that checks all registered workers.
func (w *Watchdog) checkWorkers() {
	w.mu.Lock()
	defer w.mu.Unlock()

	timeout := time.Duration(w.config.TimeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	maxIter := w.config.MaxIterations
	if maxIter <= 0 {
		maxIter = 50
	}

	stuckCount := w.config.StuckIterationCount
	if stuckCount <= 0 {
		stuckCount = 5
	}

	now := time.Now()

	for workerID, state := range w.workers {
		// Check timeout
		elapsed := now.Sub(state.StartTime)
		if elapsed > timeout {
			w.logger.Warn("Worker timed out, aborting",
				"worker_id", workerID,
				"task_id", state.TaskID,
				"elapsed", elapsed,
				"timeout", timeout,
			)

			w.fireAlert(WatchdogAlert{
				Type:      AlertTimeout,
				WorkerID:  workerID,
				TaskID:    state.TaskID,
				StepID:    state.StepID,
				Message:   fmt.Sprintf("worker timed out after %v", elapsed.Round(time.Second)),
				Duration:  elapsed,
				Iteration: state.Iteration,
			})

			// Capture partial state before abort
			w.captureReportUnsafe(state, "")

			// Cancel the worker
			if state.CancelFunc != nil {
				state.CancelFunc()
			}
			delete(w.workers, workerID)
			continue
		}

		// Check max iterations
		if state.Iteration >= maxIter {
			w.logger.Warn("Worker exceeded max iterations, aborting",
				"worker_id", workerID,
				"task_id", state.TaskID,
				"iterations", state.Iteration,
				"max", maxIter,
			)

			w.fireAlert(WatchdogAlert{
				Type:      AlertMaxIter,
				WorkerID:  workerID,
				TaskID:    state.TaskID,
				StepID:    state.StepID,
				Message:   fmt.Sprintf("worker exceeded max iterations (%d)", maxIter),
				Duration:  elapsed,
				Iteration: state.Iteration,
			})

			w.captureReportUnsafe(state, "")
			if state.CancelFunc != nil {
				state.CancelFunc()
			}
			delete(w.workers, workerID)
			continue
		}

		// Check heartbeat staleness (stuck detection)
		heartbeatAge := now.Sub(state.LastHeartbeat)
		if heartbeatAge > time.Duration(w.config.HeartbeatIntervalSec*2)*time.Second {
			w.logger.Warn("Worker heartbeat missed",
				"worker_id", workerID,
				"task_id", state.TaskID,
				"heartbeat_age", heartbeatAge.Round(time.Second),
			)

			w.fireAlert(WatchdogAlert{
				Type:      AlertHeartbeat,
				WorkerID:  workerID,
				TaskID:    state.TaskID,
				StepID:    state.StepID,
				Message:   fmt.Sprintf("heartbeat missed for %v", heartbeatAge.Round(time.Second)),
				Duration:  elapsed,
				Iteration: state.Iteration,
			})
		}

		// Check for stuck state (same stage for too many iterations)
		if state.Iteration >= stuckCount && state.LastHeartbeat.Sub(state.StartTime) < time.Second {
			state.IsStuck = true
			w.logger.Warn("Worker appears stuck",
				"worker_id", workerID,
				"task_id", state.TaskID,
				"iterations", state.Iteration,
				"stage", state.Stage,
			)

			w.fireAlert(WatchdogAlert{
				Type:      AlertStuck,
				WorkerID:  workerID,
				TaskID:    state.TaskID,
				StepID:    state.StepID,
				Message:   fmt.Sprintf("worker stuck at stage %s for %d iterations", state.Stage, state.Iteration),
				Duration:  elapsed,
				Iteration: state.Iteration,
			})
		}
	}
}

// fireAlert sends an alert to the alert channel (non-blocking).
func (w *Watchdog) fireAlert(alert WatchdogAlert) {
	select {
	case w.alertCh <- alert:
	default:
		w.logger.Warn("Alert channel full, dropping alert",
			"alert_type", alert.Type,
			"worker_id", alert.WorkerID,
		)
	}
}
