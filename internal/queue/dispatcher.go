package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// WorkerInfo represents a registered worker's capabilities.
type WorkerInfo struct {
	ID           string
	Capabilities []string
	Available    bool
	LastSeen     time.Time
}

// Dispatcher routes jobs to workers based on capabilities.
type Dispatcher struct {
	queue   Queue
	workers map[string]*WorkerInfo
	mu      sync.RWMutex
	logger  *slog.Logger

	// Channels for coordination
	jobAvailable chan struct{}
	cancel       context.CancelFunc
	wg           sync.WaitGroup // QUEUE-M2: tracks dispatch/cleanup goroutines so Stop can wait
}

// NewDispatcher creates a new job dispatcher.
func NewDispatcher(queue Queue, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}

	return &Dispatcher{
		queue:        queue,
		workers:      make(map[string]*WorkerInfo),
		logger:       logger,
		jobAvailable: make(chan struct{}, 1),
	}
}

// RegisterWorker registers a worker with its capabilities.
func (d *Dispatcher) RegisterWorker(id string, caps []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.workers[id] = &WorkerInfo{
		ID:           id,
		Capabilities: caps,
		Available:    true,
		LastSeen:     time.Now(),
	}

	d.logger.Info("Worker registered", "id", id, "capabilities", caps)
}

// UnregisterWorker removes a worker from the dispatcher.
func (d *Dispatcher) UnregisterWorker(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.workers, id)
	d.logger.Info("Worker unregistered", "id", id)
}

// SetWorkerAvailable marks a worker as available/unavailable.
func (d *Dispatcher) SetWorkerAvailable(id string, available bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if worker, ok := d.workers[id]; ok {
		worker.Available = available
		worker.LastSeen = time.Now()
	}
}

// UpdateWorkerHeartbeat updates the last seen time for a worker.
func (d *Dispatcher) UpdateWorkerHeartbeat(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if worker, ok := d.workers[id]; ok {
		worker.LastSeen = time.Now()
	}
}

// NotifyJobAvailable signals that new work is available.
func (d *Dispatcher) NotifyJobAvailable() {
	select {
	case d.jobAvailable <- struct{}{}:
	default:
		// Already notified
	}
}

// GetAvailableWorkers returns workers that can handle a job with given caps.
func (d *Dispatcher) GetAvailableWorkers(requiredCaps []string) []*WorkerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var available []*WorkerInfo
	for _, worker := range d.workers {
		if !worker.Available {
			continue
		}

		if d.workerCanHandle(worker, requiredCaps) {
			available = append(available, worker)
		}
	}

	return available
}

// workerCanHandle checks if a worker has all required capabilities.
func (d *Dispatcher) workerCanHandle(worker *WorkerInfo, requiredCaps []string) bool {
	if len(requiredCaps) == 0 {
		return true
	}

	capSet := make(map[string]bool)
	for _, capName := range worker.Capabilities {
		capSet[capName] = true
	}

	for _, required := range requiredCaps {
		if !capSet[required] {
			return false
		}
	}
	return true
}

// Start begins the dispatcher loop that notifies workers of available jobs.
// Calling Start more than once returns an error without leaking resources.
func (d *Dispatcher) Start(ctx context.Context) error {
	if d.cancel != nil {
		return fmt.Errorf("dispatcher already started")
	}
	ctx, d.cancel = context.WithCancel(ctx)

	d.wg.Add(2)
	go func() {
		defer d.wg.Done()
		d.runDispatchLoop(ctx)
	}()
	go func() {
		defer d.wg.Done()
		d.runCleanupLoop(ctx)
	}()

	d.logger.Info("Dispatcher started")
	return nil
}

// Stop stops the dispatcher and waits for the dispatch/cleanup goroutines to exit
// so callers can be certain no goroutine is touching the queue after Stop returns.
func (d *Dispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
}

func (d *Dispatcher) runDispatchLoop(ctx context.Context) {
	// Check for jobs periodically or when notified
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.jobAvailable:
			d.checkAndDispatch(ctx)
		case <-ticker.C:
			d.checkAndDispatch(ctx)
		}
	}
}

func (d *Dispatcher) checkAndDispatch(ctx context.Context) {
	// Get pending jobs
	jobs, err := d.queue.ListByState(ctx, StatePending, 10)
	if err != nil {
		d.logger.Error("Failed to list pending jobs", "error", err)
		return
	}

	if len(jobs) == 0 {
		return
	}

	d.logger.Debug("Checking jobs for dispatch", "pending", len(jobs))

	// For each job, check if any worker can handle it
	for _, job := range jobs {
		workers := d.GetAvailableWorkers(job.RequiredCaps)
		if len(workers) > 0 {
			d.logger.Debug("Job has available workers",
				"job_id", job.ID,
				"workers", len(workers),
			)
			// Workers poll for jobs, so we don't dispatch directly
			// The notification is implicit in the job being pending
		}
	}
}

func (d *Dispatcher) runCleanupLoop(ctx context.Context) {
	// Clean up stale workers periodically
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	staleThreshold := 2 * time.Minute

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.cleanupStaleWorkers(staleThreshold)
		}
	}
}

func (d *Dispatcher) cleanupStaleWorkers(threshold time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for id, worker := range d.workers {
		if now.Sub(worker.LastSeen) > threshold {
			d.logger.Warn("Marking stale worker as unavailable",
				"id", id,
				"last_seen", worker.LastSeen,
			)
			worker.Available = false
		}
	}
}

// GetStats returns dispatcher statistics.
func (d *Dispatcher) GetStats() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()

	total := len(d.workers)
	available := 0
	for _, w := range d.workers {
		if w.Available {
			available++
		}
	}

	return map[string]any{
		"total_workers":     total,
		"available_workers": available,
	}
}

// ListWorkers returns all registered workers.
func (d *Dispatcher) ListWorkers() []*WorkerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	workers := make([]*WorkerInfo, 0, len(d.workers))
	for _, w := range d.workers {
		// Copy to avoid race
		wCopy := *w
		workers = append(workers, &wCopy)
	}
	return workers
}
