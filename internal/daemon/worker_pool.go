// Package daemon provides the meept daemon components.
package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

// WorkerPool manages a pool of worker goroutines that can execute agent work.
// Workers are lazy: they only run when assigned sessions have pending work.
type WorkerPool struct {
	mu              sync.RWMutex
	workers         []*Worker
	maxWorkers      int
	maxLoopsPerWorker int
	ctx             context.Context
	cancel          context.CancelFunc
}

// Worker represents a goroutine that can process agent loop work.
type Worker struct {
	id            int
	ctx           context.Context
	cancel        context.CancelFunc
	assignedLoops map[*agent.AgentLoop]bool
	workQueue     chan WorkItem
	idleTimer     *time.Timer
	mu            sync.Mutex
	maxLoops      int
}

// WorkItem represents a unit of work for a worker.
type WorkItem struct {
	Loop           *agent.AgentLoop
	Trigger        WorkTrigger
	Message        string
	ConversationID string
	// Parts is optional for multimodal messages
	Parts []interface{} // []llm.ContentPart when used
}

// WorkTrigger indicates what triggered work.
type WorkTrigger int

const (
	TriggerUserMessage WorkTrigger = iota
	TriggerTaskQueued
	TriggerTimer
	TriggerReflection
)

// WorkerPoolConfig holds worker pool configuration.
type WorkerPoolConfig struct {
	MaxWorkers        int           // Default: 100
	MaxLoopsPerWorker int           // Default: 5
	IdleTimeout       time.Duration // Default: 5 minutes
}

// DefaultWorkerPoolConfig returns sensible defaults.
func DefaultWorkerPoolConfig() WorkerPoolConfig {
	return WorkerPoolConfig{
		MaxWorkers:        100,
		MaxLoopsPerWorker: 5,
		IdleTimeout:       5 * time.Minute,
	}
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(cfg WorkerPoolConfig) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	pool := &WorkerPool{
		workers:         make([]*Worker, 0, cfg.MaxWorkers),
		maxWorkers:      cfg.MaxWorkers,
		maxLoopsPerWorker: cfg.MaxLoopsPerWorker,
		ctx:             ctx,
		cancel:          cancel,
	}
	return pool
}

// Start initializes the pool (lazy worker creation).
func (p *WorkerPool) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Workers created on-demand when work arrives
}

// Stop gracefully shuts down all workers.
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cancel()
	for _, w := range p.workers {
		w.cancel()
	}
}

// Submit assigns work to a worker, creating one if needed.
func (p *WorkerPool) Submit(item WorkItem) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find worker with capacity
	worker := p.findWorkerWithCapacity()
	if worker == nil {
		if len(p.workers) >= p.maxWorkers {
			// Pool exhausted, queue or reject
			return fmt.Errorf("worker pool exhausted")
		}
		// Create new worker
		worker = p.createWorker()
		p.workers = append(p.workers, worker)
	}

	// Assign loop to worker if not already assigned
	worker.assignedLoops[item.Loop] = true

	// Send to work queue (non-blocking for now)
	select {
	case worker.workQueue <- item:
		return nil
	case <-time.After(100 * time.Millisecond):
		return fmt.Errorf("worker queue full")
	}
}

func (p *WorkerPool) findWorkerWithCapacity() *Worker {
	for _, w := range p.workers {
		if w.hasCapacity() {
			return w
		}
	}
	return nil
}

func (p *WorkerPool) createWorker() *Worker {
	ctx, cancel := context.WithCancel(p.ctx)
	w := &Worker{
		id:            len(p.workers),
		ctx:           ctx,
		cancel:        cancel,
		assignedLoops: make(map[*agent.AgentLoop]bool),
		workQueue:     make(chan WorkItem, 100),
		maxLoops:      p.maxLoopsPerWorker,
	}
	go w.run()
	return w
}

// hasCapacity checks if worker can accept more loops.
func (w *Worker) hasCapacity() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.assignedLoops) < w.maxLoops
}

// run is the main worker goroutine loop.
func (w *Worker) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case item := <-w.workQueue:
			// Process work item (trigger loop execution)
			w.processWorkItem(item)
			// Reset idle timer on activity
			w.resetIdleTimer()
		}
	}
}

// processWorkItem routes the work item to the appropriate AgentLoop method.
func (w *Worker) processWorkItem(item WorkItem) {
	ctx := w.ctx
	switch item.Trigger {
	case TriggerUserMessage:
		// For now, always use RunOnce - multimodal support is a future enhancement
		item.Loop.RunOnce(ctx, item.Message, item.ConversationID)
	// For other triggers, the loop would have specific handling
	// These are placeholders for future implementation
	case TriggerTaskQueued, TriggerTimer, TriggerReflection:
		// Future: implement specific handling for these triggers
	}
}

func (w *Worker) resetIdleTimer() {
	if w.idleTimer != nil {
		w.idleTimer.Stop()
	}
	// Timer handled by pool-level cleanup
}
