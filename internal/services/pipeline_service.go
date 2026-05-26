package services

import (
	"context"
	"sync"
	"time"
)

// Pipeline represents a processing pipeline.
type Pipeline struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Steps       []PipelineStep    `json:"steps"`
	Metadata    map[string]string `json:"metadata"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// PipelineStep represents a single step in a pipeline.
type PipelineStep struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// PipelineService handles pipeline operations.
type PipelineService struct {
	mu        sync.RWMutex
	pipelines map[string]*Pipeline
}

// NewPipelineService creates a pipeline service.
func NewPipelineService() *PipelineService {
	return &PipelineService{
		pipelines: make(map[string]*Pipeline),
	}
}

// StatusRequest contains status parameters.
type StatusRequest struct {
	PipelineID string `json:"pipeline_id"`
}

// PipelineStatusResponse contains pipeline status.
type PipelineStatusResponse struct {
	PipelineID string               `json:"pipeline_id"`
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	Steps      []PipelineStepStatus `json:"steps"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

// PipelineStepStatus contains step status information.
type PipelineStepStatus struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// Status returns pipeline status.
func (s *PipelineService) Status(ctx context.Context, req StatusRequest) (*PipelineStatusResponse, error) {
	if req.PipelineID == "" {
		return nil, wrapError("pipeline", "Status", ErrInvalidInput)
	}

	s.mu.RLock()
	pipeline, exists := s.pipelines[req.PipelineID]
	s.mu.RUnlock()

	if !exists {
		return nil, wrapError("pipeline", "Status", ErrNotFound)
	}

	steps := make([]PipelineStepStatus, len(pipeline.Steps))
	for i, step := range pipeline.Steps {
		steps[i] = PipelineStepStatus(step)
	}

	return &PipelineStatusResponse{
		PipelineID: pipeline.ID,
		Name:       pipeline.Name,
		Status:     pipeline.Status,
		Steps:      steps,
		CreatedAt:  pipeline.CreatedAt,
		UpdatedAt:  pipeline.UpdatedAt,
	}, nil
}

// PipelineListRequest contains list parameters.
type PipelineListRequest struct {
	Limit int `json:"limit,omitempty"`
}

// PipelineInfo contains pipeline summary information.
type PipelineInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// List returns all pipelines.
func (s *PipelineService) List(ctx context.Context, req PipelineListRequest) ([]PipelineInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]PipelineInfo, 0, len(s.pipelines))
	for _, p := range s.pipelines {
		result = append(result, PipelineInfo{
			ID:        p.ID,
			Name:      p.Name,
			Status:    p.Status,
			CreatedAt: p.CreatedAt,
		})
	}

	// Apply limit
	if req.Limit > 0 && len(result) > req.Limit {
		result = result[:req.Limit]
	}

	return result, nil
}

// CreateRequest contains create parameters.
type CreatePipelineRequest struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Steps       []PipelineStep    `json:"steps,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Create creates a new pipeline.
func (s *PipelineService) Create(ctx context.Context, req CreatePipelineRequest) (*Pipeline, error) {
	if req.Name == "" {
		return nil, wrapError("pipeline", "Create", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := req.ID
	if id == "" {
		id = generatePipelineID()
	}

	// Check for duplicate ID
	if _, exists := s.pipelines[id]; exists {
		return nil, wrapError("pipeline", "Create", ErrAlreadyExists)
	}

	now := time.Now()
	pipeline := &Pipeline{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		Status:      "pending",
		Steps:       req.Steps,
		Metadata:    req.Metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.pipelines[id] = pipeline
	return pipeline, nil
}

// DeleteRequest contains delete parameters.
type DeletePipelineRequest struct {
	PipelineID string `json:"pipeline_id"`
}

// Delete removes a pipeline.
func (s *PipelineService) Delete(ctx context.Context, req DeletePipelineRequest) error {
	if req.PipelineID == "" {
		return wrapError("pipeline", "Delete", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.pipelines[req.PipelineID]; !exists {
		return wrapError("pipeline", "Delete", ErrNotFound)
	}

	delete(s.pipelines, req.PipelineID)
	return nil
}

// UpdateStatusRequest contains update status parameters.
type UpdateStatusRequest struct {
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"`
}

// UpdateStatus updates a pipeline's status.
func (s *PipelineService) UpdateStatus(ctx context.Context, req UpdateStatusRequest) error {
	if req.PipelineID == "" {
		return wrapError("pipeline", "UpdateStatus", ErrInvalidInput)
	}
	if req.Status == "" {
		return wrapError("pipeline", "UpdateStatus", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pipeline, exists := s.pipelines[req.PipelineID]
	if !exists {
		return wrapError("pipeline", "UpdateStatus", ErrNotFound)
	}

	pipeline.Status = req.Status
	pipeline.UpdatedAt = time.Now()
	return nil
}

// generatePipelineID generates a unique pipeline ID.
func generatePipelineID() string {
	return time.Now().Format("pipeline-20060102-150405-000000000")
}
