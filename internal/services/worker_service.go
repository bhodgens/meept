package services

import (
	"context"

	"github.com/caimlas/meept/internal/worker"
)

// WorkerService handles worker operations.
type WorkerService struct {
	pool *worker.Pool
}

// NewWorkerService creates a worker service.
func NewWorkerService(p *worker.Pool) *WorkerService {
	return &WorkerService{pool: p}
}

// WorkerInfo describes a single worker for API responses.
type WorkerInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	State        string   `json:"state"`
	JobsComplete int      `json:"jobs_complete"`
	JobsFailed   int      `json:"jobs_failed"`
}

// List returns all workers in the pool.
func (s *WorkerService) List(ctx context.Context) ([]WorkerInfo, error) {
	if s.pool == nil {
		return nil, wrapError("worker", "List", ErrUnavailable)
	}
	stats := s.pool.GetStats()
	result := make([]WorkerInfo, 0, len(stats.WorkerStats))
	for _, ws := range stats.WorkerStats {
		result = append(result, WorkerInfo{
			ID:           ws.ID,
			Capabilities: ws.Capabilities,
			State:        string(ws.State),
			JobsComplete: ws.JobsComplete,
			JobsFailed:   ws.JobsFailed,
		})
	}
	return result, nil
}

// WorkerStatsResponse returns worker pool statistics.
type WorkerStatsResponse struct {
	TotalWorkers int                  `json:"total_workers"`
	IdleWorkers  int                  `json:"idle_workers"`
	BusyWorkers  int                  `json:"busy_workers"`
	ErrorWorkers int                  `json:"error_workers"`
	WorkerStats  []worker.WorkerStats `json:"worker_stats"`
}

// Stats returns worker pool statistics.
func (s *WorkerService) Stats(ctx context.Context) (*WorkerStatsResponse, error) {
	if s.pool == nil {
		return nil, wrapError("worker", "Stats", ErrUnavailable)
	}
	stats := s.pool.GetStats()
	return &WorkerStatsResponse{
		TotalWorkers: stats.TotalWorkers,
		IdleWorkers:  stats.IdleWorkers,
		BusyWorkers:  stats.BusyWorkers,
		ErrorWorkers: stats.ErrorWorkers,
		WorkerStats:  stats.WorkerStats,
	}, nil
}

// AddWorkerRequest contains add worker parameters.
type AddWorkerRequest struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	AgentID      string   `json:"agent_id,omitempty"`
}

// Add adds a worker to the pool.
func (s *WorkerService) Add(ctx context.Context, req AddWorkerRequest) (*worker.Worker, error) {
	if req.ID == "" {
		return nil, wrapError("worker", "Add", ErrInvalidInput)
	}
	if s.pool == nil {
		return nil, wrapError("worker", "Add", ErrUnavailable)
	}
	w, err := s.pool.AddWorker(req.Capabilities, req.AgentID)
	if err != nil {
		return nil, wrapError("worker", "Add", err)
	}
	return w, nil
}

// RemoveWorkerRequest contains remove worker parameters.
type RemoveWorkerRequest struct {
	ID string `json:"id"`
}

// Remove removes a worker from the pool.
func (s *WorkerService) Remove(ctx context.Context, req RemoveWorkerRequest) error {
	if req.ID == "" {
		return wrapError("worker", "Remove", ErrInvalidInput)
	}
	if s.pool == nil {
		return wrapError("worker", "Remove", ErrUnavailable)
	}
	return s.pool.RemoveWorker(req.ID)
}

// ScaleWorkersRequest contains scale parameters.
type ScaleWorkersRequest struct {
	DesiredCount int `json:"desired_count"`
}

// Scale adjusts the worker pool size.
func (s *WorkerService) Scale(ctx context.Context, req ScaleWorkersRequest) error {
	if req.DesiredCount < 0 {
		return wrapError("worker", "Scale", ErrInvalidInput)
	}
	if s.pool == nil {
		return wrapError("worker", "Scale", ErrUnavailable)
	}
	return s.pool.Scale(ctx, req.DesiredCount)
}
