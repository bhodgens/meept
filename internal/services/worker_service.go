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

// WorkerStatsResponse returns worker pool statistics.
type WorkerStatsResponse struct {
	TotalWorkers  int                     `json:"total_workers"`
	IdleWorkers   int                     `json:"idle_workers"`
	BusyWorkers   int                     `json:"busy_workers"`
	ErrorWorkers  int                     `json:"error_workers"`
	WorkerStats   []worker.WorkerStats    `json:"worker_stats"`
}

// Stats returns worker pool statistics.
func (s *WorkerService) Stats(ctx context.Context) (*WorkerStatsResponse, error) {
	if s.pool == nil {
		return nil, wrapError("worker", "Stats", ErrUnavailable)
	}
	stats := s.pool.GetStats()
	return &WorkerStatsResponse{
		TotalWorkers:  stats.TotalWorkers,
		IdleWorkers:   stats.IdleWorkers,
		BusyWorkers:   stats.BusyWorkers,
		ErrorWorkers:  stats.ErrorWorkers,
		WorkerStats:   stats.WorkerStats,
	}, nil
}
