package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/pkg/models"
)

// Pool manages a collection of workers.
type Pool struct {
	workers   map[string]*Worker
	queue     queue.Queue
	processor JobProcessor
	bus       *bus.MessageBus
	logger    *slog.Logger

	mu     sync.RWMutex
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	defaultCaps []string
	idleTimeout time.Duration
}

// PoolConfig holds pool configuration.
type PoolConfig struct {
	Queue       queue.Queue
	Processor   JobProcessor
	MessageBus  *bus.MessageBus
	Logger      *slog.Logger
	DefaultCaps []string
	IdleTimeout time.Duration
}

// NewPool creates a new worker pool.
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.Queue == nil {
		return nil, fmt.Errorf("queue is required")
	}
	if cfg.Processor == nil {
		return nil, fmt.Errorf("processor is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}

	return &Pool{
		workers:     make(map[string]*Worker),
		queue:       cfg.Queue,
		processor:   cfg.Processor,
		bus:         cfg.MessageBus,
		logger:      cfg.Logger,
		defaultCaps: cfg.DefaultCaps,
		idleTimeout: cfg.IdleTimeout,
	}, nil
}

// Start initializes the worker pool with the specified number of workers.
func (p *Pool) Start(ctx context.Context, workerCount int) error {
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return fmt.Errorf("pool already running")
	}
	ctx, p.cancel = context.WithCancel(ctx)
	p.mu.Unlock()

	// Start workers
	for i := range workerCount {
		if _, err := p.AddWorker(p.defaultCaps); err != nil {
			p.logger.Error("Failed to add worker", "index", i, "error", err)
		}
	}

	// Start the pool context with all workers
	for _, worker := range p.workers {
		p.wg.Add(1)
		go func(w *Worker) {
			defer p.wg.Done()
			if err := w.Start(ctx); err != nil {
				p.logger.Error("Worker failed to start", "id", w.ID, "error", err)
			}
		}(worker)
	}

	// Start monitoring
	go p.monitor(ctx)

	p.logger.Info("Worker pool started", "workers", workerCount)
	p.publishEvent("worker.pool.started", map[string]any{
		"worker_count": workerCount,
	})

	return nil
}

// Stop gracefully stops all workers.
func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.cancel == nil {
		p.mu.Unlock()
		return nil
	}
	p.cancel()
	p.cancel = nil
	p.mu.Unlock()

	// Wait for all workers with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("Worker pool stopped gracefully")
	case <-ctx.Done():
		p.logger.Warn("Worker pool shutdown timed out")
		return ctx.Err()
	}

	p.publishEvent("worker.pool.stopped", nil)
	return nil
}

// AddWorker adds a new worker to the pool.
func (p *Pool) AddWorker(caps []string) (*Worker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	worker, err := NewWorker(Config{
		Capabilities: caps,
		Queue:        p.queue,
		Processor:    p.processor,
		Logger:       p.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create worker: %w", err)
	}

	p.workers[worker.ID] = worker
	p.logger.Info("Worker added to pool", "id", worker.ID)

	p.publishEvent("worker.started", map[string]any{
		"worker_id":    worker.ID,
		"capabilities": caps,
	})

	return worker, nil
}

// RemoveWorker removes a worker from the pool.
func (p *Pool) RemoveWorker(workerID string) error {
	p.mu.Lock()
	worker, exists := p.workers[workerID]
	if !exists {
		p.mu.Unlock()
		return fmt.Errorf("worker not found: %s", workerID)
	}
	delete(p.workers, workerID)
	p.mu.Unlock()

	// Stop the worker
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := worker.Stop(ctx); err != nil {
		p.logger.Error("Error stopping worker", "id", workerID, "error", err)
	}

	p.publishEvent("worker.stopped", map[string]any{
		"worker_id": workerID,
	})

	return nil
}

// GetWorker returns a worker by ID.
func (p *Pool) GetWorker(workerID string) *Worker {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.workers[workerID]
}

// GetWorkers returns all workers.
func (p *Pool) GetWorkers() []*Worker {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := make([]*Worker, 0, len(p.workers))
	for _, w := range p.workers {
		workers = append(workers, w)
	}
	return workers
}

// GetStats returns pool statistics.
func (p *Pool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		TotalWorkers: len(p.workers),
		WorkerStats:  make([]WorkerStats, 0, len(p.workers)),
	}

	for _, w := range p.workers {
		ws := w.GetStats()
		stats.WorkerStats = append(stats.WorkerStats, ws)

		switch ws.State {
		case StateIdle:
			stats.IdleWorkers++
		case StateProcessing:
			stats.BusyWorkers++
		case StateError:
			stats.ErrorWorkers++
		}
	}

	return stats
}

// Scale adjusts the number of workers in the pool.
func (p *Pool) Scale(ctx context.Context, targetCount int) error {
	p.mu.Lock()
	currentCount := len(p.workers)
	p.mu.Unlock()

	if targetCount == currentCount {
		return nil
	}

	if targetCount > currentCount {
		// Add workers
		for range targetCount - currentCount {
			worker, err := p.AddWorker(p.defaultCaps)
			if err != nil {
				return fmt.Errorf("failed to add worker: %w", err)
			}

			p.wg.Add(1)
			go func(w *Worker) {
				defer p.wg.Done()
				if err := w.Start(ctx); err != nil {
					p.logger.Error("Worker failed to start", "id", w.ID, "error", err)
				}
			}(worker)
		}
	} else {
		// Remove workers (preferring idle ones)
		toRemove := currentCount - targetCount
		removed := 0

		p.mu.RLock()
		var idleWorkers []string
		for id, w := range p.workers {
			if w.GetState() == StateIdle {
				idleWorkers = append(idleWorkers, id)
			}
		}
		p.mu.RUnlock()

		// Remove idle workers first
		for _, id := range idleWorkers {
			if removed >= toRemove {
				break
			}
			if err := p.RemoveWorker(id); err == nil {
				removed++
			}
		}

		// If we still need to remove more, remove any worker
		if removed < toRemove {
			p.mu.RLock()
			var anyWorkers []string
			for id := range p.workers {
				if removed >= toRemove {
					break
				}
				anyWorkers = append(anyWorkers, id)
			}
			p.mu.RUnlock()

			for _, id := range anyWorkers {
				if removed >= toRemove {
					break
				}
				if err := p.RemoveWorker(id); err == nil {
					removed++
				}
			}
		}
	}

	p.logger.Info("Pool scaled", "from", currentCount, "to", targetCount)
	return nil
}

func (p *Pool) monitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := p.GetStats()
			p.publishEvent("worker.status", map[string]any{
				"total":  stats.TotalWorkers,
				"idle":   stats.IdleWorkers,
				"busy":   stats.BusyWorkers,
				"errors": stats.ErrorWorkers,
			})
		}
	}
}

func (p *Pool) publishEvent(topic string, data map[string]any) {
	if p.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "worker-pool", data)
	if err != nil {
		p.logger.Error("Failed to create bus message", "error", err)
		return
	}

	p.bus.Publish(topic, msg)
}

// PoolStats holds pool statistics.
type PoolStats struct {
	TotalWorkers int
	IdleWorkers  int
	BusyWorkers  int
	ErrorWorkers int
	WorkerStats  []WorkerStats
}

// Handler handles worker-related requests on the message bus.
type Handler struct {
	pool   *Pool
	bus    *bus.MessageBus
	logger *slog.Logger
	cancel context.CancelFunc
}

// NewHandler creates a new worker handler.
func NewHandler(pool *Pool, msgBus *bus.MessageBus, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		pool:   pool,
		bus:    msgBus,
		logger: logger,
	}
}

// Start begins listening for worker requests.
func (h *Handler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	topics := []string{
		"worker.add",
		"worker.remove",
		"worker.list",
		"worker.stats",
		"worker.scale",
	}

	for _, topic := range topics {
		sub := h.bus.Subscribe("worker-handler-"+topic, topic)
		go h.handleTopic(ctx, sub, topic)
	}

	h.logger.Info("Worker handler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

func (h *Handler) handleTopic(ctx context.Context, sub *bus.Subscriber, topic string) {
	for {
		select {
		case <-ctx.Done():
			h.bus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			h.handleMessage(ctx, topic, msg)
		}
	}
}

func (h *Handler) handleMessage(ctx context.Context, topic string, msg *models.BusMessage) {
	var response any
	var err error

	switch topic {
	case "worker.add":
		response, err = h.handleAdd(ctx, msg)
	case "worker.remove":
		response, err = h.handleRemove(ctx, msg)
	case "worker.list":
		response, err = h.handleList(ctx, msg)
	case "worker.stats":
		response, err = h.handleStats(ctx, msg)
	case "worker.scale":
		response, err = h.handleScale(ctx, msg)
	default:
		err = fmt.Errorf("unknown topic: %s", topic)
	}

	h.sendResponse(msg.ID, "worker.result", response, err)
}

func (h *Handler) handleAdd(_ context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		Capabilities []string `json:"capabilities,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	worker, err := h.pool.AddWorker(params.Capabilities)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"worker_id":    worker.ID,
		"capabilities": worker.Capabilities,
		"state":        string(worker.GetState()),
	}, nil
}

func (h *Handler) handleRemove(_ context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.pool.RemoveWorker(params.WorkerID); err != nil {
		return nil, err
	}

	return map[string]string{"status": "removed"}, nil
}

func (h *Handler) handleList(_ context.Context, _ *models.BusMessage) (any, error) { //nolint:unparam // interface contract
	workers := h.pool.GetWorkers()

	workerList := make([]map[string]any, 0, len(workers))
	for _, w := range workers {
		ws := w.GetStats()
		workerList = append(workerList, map[string]any{
			"id":             ws.ID,
			"state":          string(ws.State),
			"capabilities":   ws.Capabilities,
			"start_time":     ws.StartTime.Format(time.RFC3339),
			"last_active":    ws.LastActive.Format(time.RFC3339),
			"jobs_complete":  ws.JobsComplete,
			"jobs_failed":    ws.JobsFailed,
			"current_job_id": ws.CurrentJobID,
		})
	}

	stats := h.pool.GetStats()
	return map[string]any{
		"workers": workerList,
		"stats": map[string]any{
			"total_workers": stats.TotalWorkers,
			"idle_workers":  stats.IdleWorkers,
			"busy_workers":  stats.BusyWorkers,
			"error_workers": stats.ErrorWorkers,
		},
	}, nil
}

func (h *Handler) handleStats(_ context.Context, _ *models.BusMessage) (any, error) { //nolint:unparam // interface contract
	stats := h.pool.GetStats()

	workerStats := make([]map[string]any, 0, len(stats.WorkerStats))
	for _, ws := range stats.WorkerStats {
		workerStats = append(workerStats, map[string]any{
			"id":             ws.ID,
			"state":          string(ws.State),
			"capabilities":   ws.Capabilities,
			"start_time":     ws.StartTime.Format(time.RFC3339),
			"last_active":    ws.LastActive.Format(time.RFC3339),
			"jobs_complete":  ws.JobsComplete,
			"jobs_failed":    ws.JobsFailed,
			"current_job_id": ws.CurrentJobID,
		})
	}

	return map[string]any{
		"total_workers": stats.TotalWorkers,
		"idle_workers":  stats.IdleWorkers,
		"busy_workers":  stats.BusyWorkers,
		"error_workers": stats.ErrorWorkers,
		"worker_stats":  workerStats,
	}, nil
}

func (h *Handler) handleScale(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		TargetCount int `json:"target_count"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if params.TargetCount < 0 {
		return nil, fmt.Errorf("target_count must be non-negative")
	}

	if err := h.pool.Scale(ctx, params.TargetCount); err != nil {
		return nil, err
	}

	return map[string]any{
		"status":       "scaled",
		"target_count": params.TargetCount,
	}, nil
}

func (h *Handler) sendResponse(replyTo, topic string, response any, err error) {
	var payload []byte

	if err != nil {
		payload, _ = json.Marshal(map[string]string{"error": err.Error()})
	} else {
		payload, _ = json.Marshal(response)
	}

	respMsg := &models.BusMessage{
		ID:        fmt.Sprintf("worker-resp-%d", time.Now().UnixNano()),
		Type:      models.MessageTypeResponse,
		Topic:     topic,
		Source:    "worker-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish(topic, respMsg)
}
